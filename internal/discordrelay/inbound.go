package discordrelay

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"go.uber.org/zap"
)

// runInbound turns a batch of tags (one, or those that waited together) into one turn, and
// waits for its reply to be posted (or the turn to fail), keeping the typing indicator up
// meanwhile. Each tag passes the loop guard first; the reply answers the newest.
func (s *Service) runInbound(ctx context.Context, batch []inboundWork) {
	s.defaults()
	newest := batch[len(batch)-1]
	log := s.Logger.With(zap.String("binding_id", newest.target.Binding.ID.String()), zap.String("discord_message_id", newest.msg.ID),
		zap.Int("batch", len(batch)))

	// The tags may have waited behind a running turn for minutes; re-check against the
	// binding and thread as they are now, and act on that state from here on. A binding
	// repointed to another thread meanwhile drops them: they were accepted for this one.
	fresh, err := s.Store.GetDiscordBindingTarget(ctx, newest.target.Binding.ID)
	if err != nil || fresh == nil || fresh.Binding.ChatID != newest.target.Binding.ChatID ||
		!fresh.Binding.InboundEnabled || fresh.Binding.Status != models.DiscordBindingActive ||
		!s.threadOpen(ctx, *fresh) {
		log.Info("discord relay: binding or thread no longer open; dropping queued tags")
		return
	}
	b, bot := fresh.Binding, fresh.Bot

	msgs := make([]InboundMessage, 0, len(batch))
	for _, w := range batch {
		if (w.msg.AuthorIsBot && !b.AnswerAgents) || !Allowed(w.msg.AuthorID, b.AllowUserIDs, b.DenyUserIDs) {
			continue
		}
		msgs = append(msgs, w.msg)
	}
	depths, turnDepth, ok := s.guardBatch(ctx, b.ID, msgs, log)
	if !ok {
		return
	}

	type recorded struct {
		msg  InboundMessage
		link *models.DiscordMessageLink
	}
	var tags []recorded
	for i, msg := range msgs {
		if depths[i] < 0 {
			continue // past the loop guard's cap
		}
		link, created, err := s.Store.RecordInboundDiscordMessage(ctx, models.DiscordInbound{
			BindingID:        b.ID,
			DiscordMessageID: msg.ID,
			DiscordChannelID: msg.ChannelID,
			AuthorID:         msg.AuthorID,
			AuthorName:       msg.AuthorName,
			AgentDepth:       depths[i],
		})
		if err != nil {
			log.Error("discord relay: record inbound", zap.Error(err))
			continue
		}
		if !created {
			continue // already answered (a redelivered event)
		}
		tags = append(tags, recorded{msg: msg, link: link})
	}
	if len(tags) == 0 {
		return
	}
	last := tags[len(tags)-1].msg
	// Who is told when the turn fails: the newest person in the batch. Never a bot, which would
	// answer the notice; and only when there is no person in the batch is nobody told.
	var asker *InboundMessage
	for i := len(tags) - 1; i >= 0; i-- {
		if !tags[i].msg.AuthorIsBot {
			asker = &tags[i].msg
			break
		}
	}

	var (
		lines       []string
		attachments []*models.FileAttachment
	)
	for _, tag := range tags {
		atts, notes := s.ingestInbound(ctx, bot.OwnerID, b.ChatID, tag.msg, MaxInboundAttachments-len(attachments))
		attachments = append(attachments, atts...)
		line := PromptText(tag.msg, bot.BotUserID, b.ChannelName)
		if len(notes) > 0 {
			line += "\n" + strings.Join(notes, "\n")
		}
		lines = append(lines, line)
	}
	prompt := relaykit.BatchPrompt(lines)
	if hint := s.guard.Hint(turnDepth); hint != "" {
		prompt += "\n" + hint
	}
	done := make(chan struct{})
	resp, err := s.Turns.StartUserTurn(ctx, plugins.UserTurn{
		// No timezone: the turn's timestamps use the server default, so the owner's
		// local time zone is not shown to whoever tagged the bot.
		UserID: bot.OwnerID,
		// Metered as a Discord turn (metering.TurnSource), apart from the owner's own chats.
		Source: TurnSource,
		Message: models.ChatMessage{
			ChatID:      b.ChatID,
			Message:     prompt,
			Attachments: attachments,
		},
	})
	if err != nil {
		log.Warn("discord relay: start turn", zap.Error(err))
		for _, tag := range tags {
			_ = s.Store.DeleteDiscordLink(ctx, tag.link.ID)
		}
		if asker != nil {
			s.notice(ctx, bot.Token, asker.ChannelID, asker.ID, NoticeFailed)
		}
		return
	}
	s.setWaiter(resp.ID, done)
	defer s.clearWaiter(resp.ID)
	// Newest first: the reply hook posts to the newest attached link, so a reply that finishes
	// while these are attached must already find the newest, not the oldest, tag.
	for i := len(tags) - 1; i >= 0; i-- {
		if err := s.Store.AttachDiscordLinkMessage(ctx, tags[i].link.ID, resp.ID); err != nil {
			log.Error("discord relay: attach inbound message", zap.Error(err))
		}
	}
	_ = s.Store.TouchDiscordBinding(ctx, b.ID)
	// A reply that finished before the links were attached found none; post it now.
	if ev, ok := s.takeOrphan(resp.ID); ok {
		s.OnReply(ctx, ev)
	}

	jobID, _ := uuid.Parse(resp.JobID)
	s.waitForReply(ctx, bot, last, asker, jobID, done)
}

// lastInbound is the binding's previous message for the loop guard (nil: none within its
// Since). A failed read counts as the chain at its cap (LoopGuard.Saturated): bots are dropped,
// people still answered, rather than a failed read restarting an exchange.
func (s *Service) lastInbound(ctx context.Context, bindingID uuid.UUID) *relaykit.LastInbound {
	last, err := s.Store.LastInboundDiscordLink(ctx, bindingID, s.guard.Since())
	if err != nil {
		s.Logger.Warn("discord relay: read the loop guard's previous message", zap.String("binding_id", bindingID.String()), zap.Error(err))
		return s.guard.Saturated()
	}
	if last == nil {
		return nil
	}
	return &relaykit.LastInbound{Depth: last.AgentDepth, At: last.CreatedAt}
}

// guardBatch applies the loop guard to a batch's tags (relaykit.LoopGuard.Batch), from the
// binding's newest inbound link: each tag's agent depth, or -1 when it is dropped past the cap.
func (s *Service) guardBatch(ctx context.Context, bindingID uuid.UUID, msgs []InboundMessage, log *zap.Logger) (depths []int, turnDepth int, ok bool) {
	prev := s.lastInbound(ctx, bindingID)
	fromBot := make([]bool, len(msgs))
	for i, m := range msgs {
		fromBot[i] = m.AuthorIsBot
	}
	depths, turnDepth, ok = s.guard.Batch(bindingID, fromBot, prev)
	for i, d := range depths {
		if d < 0 {
			log.Info("discord relay: loop guard: too many bot turns in a row; dropping tag", zap.String("dropped_message_id", msgs[i].ID))
		}
	}
	if !ok && slices.ContainsFunc(depths, func(d int) bool { return d > 0 }) {
		log.Info("discord relay: loop guard: hourly budget of bot-started turns spent; dropping tags")
	}
	return depths, turnDepth, ok
}

func (s *Service) waitForReply(ctx context.Context, bot models.DiscordBotCredentials, msg InboundMessage, asker *InboundMessage, jobID uuid.UUID, done <-chan struct{}) {
	_ = s.Discord.Typing(ctx, bot.Token, msg.ChannelID)
	typing := time.NewTicker(s.TypingEvery)
	defer typing.Stop()
	poll := time.NewTicker(s.PollEvery)
	defer poll.Stop()
	deadline := time.NewTimer(s.TurnTimeout)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-deadline.C:
			s.Logger.Warn("discord relay: turn timed out", zap.String("job_id", jobID.String()))
			return
		case <-typing.C:
			_ = s.Discord.Typing(ctx, bot.Token, msg.ChannelID)
		case <-poll.C:
			if jobID == uuid.Nil {
				continue
			}
			job, err := s.Store.GetJob(ctx, bot.OwnerID, jobID)
			if err != nil || job == nil {
				continue
			}
			if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
				if asker != nil {
					s.notice(ctx, bot.Token, asker.ChannelID, asker.ID, NoticeFailed)
				}
				return
			}
			if job.Status == models.JobStatusComplete {
				// The reply hook posts it; the thread's next tag need not wait for that.
				return
			}
		}
	}
}
