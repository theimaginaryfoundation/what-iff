package slackrelay

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

// runInbound turns a batch of mentions (one, or those that waited together) into one turn,
// and waits for its reply to be posted (or the turn to fail), with the working reaction on the
// newest mention meanwhile. Each mention passes the loop guard first; the reply answers the
// newest.
func (s *Service) runInbound(ctx context.Context, batch []inboundWork) {
	s.defaults()
	newest := batch[len(batch)-1]
	log := s.Logger.With(zap.String("binding_id", newest.target.Binding.ID.String()), zap.String("ts", newest.msg.TS),
		zap.Int("batch", len(batch)))

	// The mentions may have waited behind a running turn; re-check against the binding and
	// thread as they are now. A binding repointed to another thread meanwhile drops them.
	fresh, err := s.Store.GetSlackBindingTarget(ctx, newest.target.Binding.ID)
	if err != nil || fresh == nil || fresh.Binding.ChatID != newest.target.Binding.ChatID || !s.threadOpen(ctx, *fresh) {
		log.Info("slack relay: binding or thread no longer open; dropping queued mentions")
		return
	}
	t := *fresh
	b, app := t.Binding, t.App

	mentions, turnDepth := s.recordBatch(ctx, t, batch, log)
	if len(mentions) == 0 {
		return
	}
	last := mentions[len(mentions)-1].w
	prompt, attachments := s.buildTurn(ctx, t, mentions, turnDepth)

	done := make(chan struct{})
	resp, err := s.Turns.StartUserTurn(ctx, plugins.UserTurn{
		// No timezone: the turn's timestamps use the server default, so the owner's local time
		// zone is not shown to whoever mentioned the persona.
		UserID: app.OwnerID,
		Source: TurnSource,
		Message: models.ChatMessage{
			ChatID:      b.ChatID,
			Message:     prompt,
			Attachments: attachments,
		},
	})
	if err != nil {
		log.Warn("slack relay: start turn", zap.Error(err))
		for _, m := range mentions {
			_ = s.Store.DeleteSlackLink(ctx, m.link.ID)
		}
		if !last.agent {
			s.notice(ctx, t, last.msg, NoticeFailed)
		}
		return
	}
	s.replies.Wait(resp.ID, done)
	defer s.replies.Forget(resp.ID)
	// Newest first: the reply hook posts to the newest attached link, so a reply that finishes
	// while these are attached must already find the newest, not the oldest, mention.
	for i := len(mentions) - 1; i >= 0; i-- {
		if err := s.Store.AttachSlackLinkMessage(ctx, mentions[i].link.ID, resp.ID); err != nil {
			log.Error("slack relay: attach inbound message", zap.Error(err))
		}
	}
	_ = s.Store.TouchSlackBinding(ctx, b.ID)
	// A reply that finished before the links were attached found none; post it now.
	if orphan, ok := s.replies.TakeOrphan(resp.ID); ok {
		s.OnReply(ctx, orphan)
	}

	jobID, _ := uuid.Parse(resp.JobID)
	s.react(ctx, app, b.ChannelID, last.msg.TS, true)
	defer s.react(context.WithoutCancel(ctx), app, b.ChannelID, last.msg.TS, false)
	s.waitForReply(ctx, t, last, jobID, done)
}

// recorded is a mention of a batch that passed the loop guard and was recorded.
type recorded struct {
	w      inboundWork
	link   *models.SlackMessageLink
	author string
}

// recordBatch re-applies the trigger rules and the loop guard to a batch against the binding as
// it is now, and records each mention that passes (the dedupe key): the mentions to answer,
// oldest first, and the turn's agent depth.
func (s *Service) recordBatch(ctx context.Context, t models.SlackBindingTarget, batch []inboundWork, log *zap.Logger) ([]recorded, int) {
	b, app := t.Binding, t.App
	var admitted []inboundWork
	for _, w := range batch {
		agent, ok := s.admits(ctx, t, w.msg)
		if !ok {
			continue
		}
		w.agent = agent
		admitted = append(admitted, w)
	}
	depths, turnDepth, ok := s.guardBatch(ctx, b.ID, admitted, log)
	if !ok {
		return nil, 0
	}
	var mentions []recorded
	for i, w := range admitted {
		if depths[i] < 0 {
			continue // past the loop guard's cap
		}
		author := s.authorName(ctx, app, w.msg)
		link, created, err := s.Store.RecordInboundSlackMessage(ctx, models.SlackInbound{
			BindingID: b.ID, MessageTS: w.msg.TS, ThreadTS: w.msg.ThreadRoot(),
			AuthorUserID: w.msg.User, AuthorName: author, AgentDepth: depths[i],
		})
		if err != nil {
			log.Error("slack relay: record inbound", zap.Error(err))
			continue
		}
		if !created {
			continue // already answered (a redelivered event)
		}
		mentions = append(mentions, recorded{w: w, link: link, author: author})
	}
	return mentions, turnDepth
}

// buildTurn is the turn's user message for a batch of mentions: each relayed line (with the
// thread's first message quoted once, and notes for files left out) under the batch note and
// the loop guard's hint, and the files the mentions brought, within one turn's budget.
func (s *Service) buildTurn(ctx context.Context, t models.SlackBindingTarget, mentions []recorded, turnDepth int) (string, []*models.FileAttachment) {
	b, app := t.Binding, t.App
	names := func(id string) string { return s.names.get(ctx, s.Slack, app, id).name }
	var (
		lines       []string
		attachments []*models.FileAttachment
		quotedRoots = map[string]bool{}
	)
	for _, m := range mentions {
		quoteAuthor, quote := "", ""
		if root := m.w.msg.ThreadTS; m.w.msg.InThread() && !quotedRoots[root] {
			quotedRoots[root] = true
			quoteAuthor, quote = s.quoted(ctx, app, m.w.msg, names)
		}
		atts, notes := s.ingestInbound(ctx, app, b.ChatID, m.w.msg, MaxInboundAttachments-len(attachments))
		attachments = append(attachments, atts...)
		line := PromptText(PlainText(m.w.msg.Text, app.BotUserID, names), m.author, b.ChannelName, quoteAuthor, quote)
		if len(notes) > 0 {
			line += "\n" + strings.Join(notes, "\n")
		}
		lines = append(lines, line)
	}
	prompt := relaykit.BatchPrompt(lines)
	if hint := s.guard.Hint(turnDepth); hint != "" {
		prompt += "\n" + hint
	}
	return prompt, attachments
}

// lastInbound is the binding's previous message for the loop guard (nil: none within its
// Since). A failed read counts as the chain at its cap (LoopGuard.Saturated): agents are
// dropped, people still answered, rather than a failed read restarting an exchange.
func (s *Service) lastInbound(ctx context.Context, bindingID uuid.UUID) *relaykit.LastInbound {
	last, err := s.Store.LastInboundSlackLink(ctx, bindingID, s.guard.Since())
	if err != nil {
		s.Logger.Warn("slack relay: read the loop guard's previous message", zap.String("binding_id", bindingID.String()), zap.Error(err))
		return s.guard.Saturated()
	}
	if last == nil {
		return nil
	}
	return &relaykit.LastInbound{Depth: last.AgentDepth, At: last.CreatedAt}
}

// guardBatch applies the loop guard to a batch's mentions (relaykit.LoopGuard.Batch), from the
// binding's newest inbound link: each mention's agent depth, or -1 when it is dropped past the
// cap.
func (s *Service) guardBatch(ctx context.Context, bindingID uuid.UUID, batch []inboundWork, log *zap.Logger) (depths []int, turnDepth int, ok bool) {
	prev := s.lastInbound(ctx, bindingID)
	fromAgent := make([]bool, len(batch))
	for i, w := range batch {
		fromAgent[i] = w.agent
	}
	depths, turnDepth, ok = s.guard.Batch(bindingID, fromAgent, prev)
	for i, d := range depths {
		if d < 0 {
			log.Info("slack relay: loop guard: too many agent turns in a row; dropping mention", zap.String("dropped_ts", batch[i].msg.TS))
		}
	}
	if !ok && slices.ContainsFunc(depths, func(d int) bool { return d > 0 }) {
		log.Info("slack relay: loop guard: hourly budget of agent-started turns spent; dropping mentions")
	}
	return depths, turnDepth, ok
}

// authorName is what the relay calls a message's author: a bot by its app's name, a person by
// their display name.
func (s *Service) authorName(ctx context.Context, app models.SlackAppCredentials, m Message) string {
	if m.BotProfile != nil && strings.TrimSpace(m.BotProfile.Name) != "" {
		return m.BotProfile.Name
	}
	if n := s.names.get(ctx, s.Slack, app, m.User).name; n != "" {
		return n
	}
	return "someone"
}

// quoted is the first message of the thread m was posted in, when that is someone else's (the
// persona's own is already in the relay thread): its author's name and text. Lookup failures
// quote nothing.
func (s *Service) quoted(ctx context.Context, app models.SlackAppCredentials, m Message, names func(string) string) (string, string) {
	parent, err := s.Slack.ThreadParent(ctx, app.BotToken, m.Channel, m.ThreadTS)
	if err != nil || parent == nil || parent.User == app.BotUserID || (app.BotID != "" && parent.BotID == app.BotID) {
		return "", ""
	}
	return s.authorName(ctx, app, *parent), PlainText(parent.Text, app.BotUserID, names)
}

// react puts the working reaction on (or takes it off) the message being answered. Failures
// (a missing reactions:write scope, say) only cost the indicator.
func (s *Service) react(ctx context.Context, app models.SlackAppCredentials, channel, ts string, on bool) {
	var err error
	if on {
		err = s.Slack.AddReaction(ctx, app.BotToken, channel, ts, WorkingReaction)
	} else {
		err = s.Slack.RemoveReaction(ctx, app.BotToken, channel, ts, WorkingReaction)
	}
	if err != nil {
		s.Logger.Debug("slack relay: working reaction", zap.Bool("on", on), zap.Error(err))
	}
}

func (s *Service) waitForReply(ctx context.Context, t models.SlackBindingTarget, w inboundWork, jobID uuid.UUID, done <-chan struct{}) {
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
			s.Logger.Warn("slack relay: turn timed out", zap.String("job_id", jobID.String()))
			return
		case <-poll.C:
			if jobID == uuid.Nil {
				continue
			}
			job, err := s.Store.GetJob(ctx, t.App.OwnerID, jobID)
			if err != nil || job == nil {
				continue
			}
			if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
				if !w.agent {
					s.notice(ctx, t, w.msg, NoticeFailed)
				}
				return
			}
			if job.Status == models.JobStatusComplete {
				return // the reply hook posts it; the thread's next mention need not wait
			}
		}
	}
}
