package buzzrelay

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"go.uber.org/zap"
)

// runInbound turns a batch of mentions (one, or those that waited together) into one turn,
// and waits for its reply to be posted (or the turn to fail), keeping the typing indicator up
// meanwhile. Each mention passes the loop guard first; the reply answers the newest.
func (s *Service) runInbound(ctx context.Context, batch []inboundWork) {
	s.defaults()
	newest := batch[len(batch)-1]
	log := s.Logger.With(zap.String("binding_id", newest.target.Binding.ID.String()), zap.String("event_id", newest.ev.ID),
		zap.Int("batch", len(batch)))

	// The mentions may have waited behind a running turn; re-check against the binding and
	// thread as they are now. A binding repointed to another thread meanwhile drops them.
	fresh, err := s.Store.GetBuzzBindingTarget(ctx, newest.target.Binding.ID)
	if err != nil || fresh == nil || fresh.Binding.ChatID != newest.target.Binding.ChatID || !s.threadOpen(ctx, *fresh) {
		log.Info("buzz relay: binding or thread no longer open; dropping queued mentions")
		return
	}
	t := *fresh
	b := t.Binding
	ep, err := EndpointFor(t.Community)
	if err != nil {
		log.Error("buzz relay: stored relay URL unusable", zap.Error(err))
		return
	}
	signer, err := SignerFor(t.Agent, t.AuthTag)
	if err != nil {
		log.Error("buzz relay: persona key unusable", zap.Error(err))
		return
	}
	if name := s.channelName(ctx, t, ep, signer); name != "" && name != b.ChannelName {
		if err := s.Store.RenameBuzzChannel(ctx, b.ID, name); err != nil {
			log.Warn("buzz relay: record channel rename", zap.Error(err))
		} else {
			b.ChannelName = name
		}
	}

	var admitted []inboundWork
	for _, w := range batch {
		agent, ok := s.admits(ctx, t, w.ev)
		if !ok {
			continue
		}
		w.agent = agent
		admitted = append(admitted, w)
	}
	depths, turnDepth, ok := s.guardBatch(ctx, b.ID, admitted, log)
	if !ok {
		return
	}

	type recorded struct {
		w      inboundWork
		link   *models.BuzzMessageLink
		author string
	}
	var mentions []recorded
	for i, w := range admitted {
		if depths[i] < 0 {
			continue // past the loop guard's cap
		}
		root := ThreadOf(w.ev).Root
		if root == "" {
			root = w.ev.ID
		}
		author := s.names.name(ctx, s.Relay, ep, signer, w.ev.PubKey)
		link, created, err := s.Store.RecordInboundBuzzMessage(ctx, models.BuzzInbound{
			BindingID: b.ID, EventID: w.ev.ID, ThreadRoot: root, AuthorPubKey: w.ev.PubKey, AuthorName: author, AgentDepth: depths[i],
		})
		if err != nil {
			log.Error("buzz relay: record inbound", zap.Error(err))
			continue
		}
		if !created {
			continue // already answered (a redelivered event)
		}
		mentions = append(mentions, recorded{w: w, link: link, author: author})
	}
	if len(mentions) == 0 {
		return
	}
	last := mentions[len(mentions)-1].w

	personaName := ""
	if p, err := s.Store.GetPersonality(ctx, t.Agent.OwnerID, t.Agent.PersonalityID); err == nil && p != nil {
		personaName = p.Name
	}
	npub, _ := nostr.EncodeNPub(t.Agent.PubKey)
	var (
		lines       []string
		attachments []*models.FileAttachment
	)
	for _, m := range mentions {
		quoteAuthor, quote := s.quoted(ctx, ep, signer, t.Agent.PubKey, m.w.ev, ThreadOf(m.w.ev))
		atts, notes := s.ingestInbound(ctx, ep, signer, t.Agent.OwnerID, b.ChatID, m.w.ev, MaxInboundAttachments-len(attachments))
		attachments = append(attachments, atts...)
		line := PromptText(m.w.ev.Content, m.author, b.ChannelName, personaName, npub, quoteAuthor, quote)
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
		// No timezone: the turn's timestamps use the server default, so the owner's local time
		// zone is not shown to whoever mentioned the persona.
		UserID: t.Agent.OwnerID,
		Source: TurnSource,
		Message: models.ChatMessage{
			ChatID:      b.ChatID,
			Message:     prompt,
			Attachments: attachments,
		},
	})
	if err != nil {
		log.Warn("buzz relay: start turn", zap.Error(err))
		for _, m := range mentions {
			_ = s.Store.DeleteBuzzLink(ctx, m.link.ID)
		}
		if !last.agent {
			s.notice(ctx, t, last.ev, NoticeFailed)
		}
		return
	}
	s.replies.Wait(resp.ID, done)
	defer s.replies.Forget(resp.ID)
	for _, m := range mentions {
		if err := s.Store.AttachBuzzLinkMessage(ctx, m.link.ID, resp.ID); err != nil {
			log.Error("buzz relay: attach inbound message", zap.Error(err))
		}
	}
	_ = s.Store.TouchBuzzBinding(ctx, b.ID)
	// A reply that finished before the links were attached found none; post it now.
	if orphan, ok := s.replies.TakeOrphan(resp.ID); ok {
		s.OnReply(ctx, orphan)
	}

	jobID, _ := uuid.Parse(resp.JobID)
	s.waitForReply(ctx, t, ep, signer, last, jobID, done)
}

// guardBatch applies the loop guard to a batch's mentions (relaykit.LoopGuard.Batch), from the
// binding's newest inbound link: each mention's agent depth, or -1 when it is dropped past the
// cap.
func (s *Service) guardBatch(ctx context.Context, bindingID uuid.UUID, batch []inboundWork, log *zap.Logger) (depths []int, turnDepth int, ok bool) {
	var prev *relaykit.LastInbound
	if last, err := s.Store.LastInboundBuzzLink(ctx, bindingID, s.guard.Since()); err != nil {
		log.Warn("buzz relay: read the loop guard's previous message", zap.Error(err))
	} else if last != nil {
		prev = &relaykit.LastInbound{Depth: last.AgentDepth, At: last.CreatedAt}
	}
	fromAgent := make([]bool, len(batch))
	for i, w := range batch {
		fromAgent[i] = w.agent
	}
	depths, turnDepth, ok = s.guard.Batch(bindingID, fromAgent, prev)
	for i, d := range depths {
		if d < 0 {
			log.Info("buzz relay: loop guard: too many agent turns in a row; dropping mention", zap.String("dropped_event_id", batch[i].ev.ID))
		}
	}
	if !ok && slices.ContainsFunc(depths, func(d int) bool { return d > 0 }) {
		log.Info("buzz relay: loop guard: hourly budget of agent-started turns spent; dropping mentions")
	}
	return depths, turnDepth, ok
}

// quoted is the message ev replies to, when that is someone else's (the persona's own is
// already the previous reply in the thread): its author's name and text. Lookup failures
// quote nothing.
func (s *Service) quoted(ctx context.Context, ep Endpoint, signer Signer, agentPub string, ev nostr.Event, thread ThreadRef) (string, string) {
	if thread.Parent == "" || thread.Parent == ev.ID {
		return "", ""
	}
	events, err := s.Relay.Query(ctx, ep, signer, Filter{"ids": []string{thread.Parent}, "limit": 1})
	if err != nil || len(events) == 0 {
		return "", ""
	}
	parent := events[0]
	if parent.Verify() != nil || parent.PubKey == agentPub || parent.Kind != KindStreamMessage {
		return "", ""
	}
	return s.names.name(ctx, s.Relay, ep, signer, parent.PubKey), parent.Content
}

func (s *Service) waitForReply(ctx context.Context, t models.BuzzBindingTarget, ep Endpoint, signer Signer, w inboundWork, jobID uuid.UUID, done <-chan struct{}) {
	ev := w.ev
	reply := ReplyThread(ev)
	s.typing(ctx, ep, signer, t.Binding.ChannelID, &reply)
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
			s.Logger.Warn("buzz relay: turn timed out", zap.String("job_id", jobID.String()))
			return
		case <-typing.C:
			s.typing(ctx, ep, signer, t.Binding.ChannelID, &reply)
		case <-poll.C:
			if jobID == uuid.Nil {
				continue
			}
			job, err := s.Store.GetJob(ctx, t.Agent.OwnerID, jobID)
			if err != nil || job == nil {
				continue
			}
			if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
				if !w.agent {
					s.notice(ctx, t, ev, NoticeFailed)
				}
				return
			}
			if job.Status == models.JobStatusComplete {
				return // the reply hook posts it; the thread's next mention need not wait
			}
		}
	}
}

func (s *Service) typing(ctx context.Context, ep Endpoint, signer Signer, channelID string, thread *ThreadRef) {
	ev, err := TypingEvent(signer, channelID, thread)
	if err != nil {
		return
	}
	if err := s.Relay.Publish(ctx, ep, signer, ev); err != nil {
		s.Logger.Debug("buzz relay: typing pulse failed", zap.Error(err))
	}
}
