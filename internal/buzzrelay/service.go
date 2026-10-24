package buzzrelay

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

// Store is the storage the relay needs (the datastore satisfies it).
type Store interface {
	GetBuzzBindingTarget(ctx context.Context, bindingID uuid.UUID) (*models.BuzzBindingTarget, error)
	RecordInboundBuzzMessage(ctx context.Context, in models.BuzzInbound) (*models.BuzzMessageLink, bool, error)
	AttachBuzzLinkMessage(ctx context.Context, linkID, chatMessageID uuid.UUID) error
	DeleteBuzzLink(ctx context.Context, linkID uuid.UUID) error
	FindInboundBuzzLink(ctx context.Context, chatMessageID uuid.UUID) (*models.BuzzMessageLink, error)
	BuzzThreadContinues(ctx context.Context, bindingID uuid.UUID, root, parent, author string) (bool, error)
	StartOutboundBuzzLink(ctx context.Context, bindingID, chatMessageID uuid.UUID, threadRoot string, replyTo *uuid.UUID) (*models.BuzzMessageLink, bool, error)
	FinishOutboundBuzzLink(ctx context.Context, linkID uuid.UUID, postedIDs []string, postErr error) error
	ConsumeBuzzPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error)
	SetBuzzBindingStatus(ctx context.Context, id uuid.UUID, status models.BuzzBindingStatus, lastError string) error
	WithdrawBuzzBindingAcknowledgement(ctx context.Context, id uuid.UUID) error
	TouchBuzzBinding(ctx context.Context, id uuid.UUID) error
	// LastInboundBuzzLink is the binding's newest inbound message since a time, for the loop
	// guard; RenameBuzzChannel records a channel's current name on a binding.
	LastInboundBuzzLink(ctx context.Context, bindingID uuid.UUID, since time.Time) (*models.BuzzMessageLink, error)
	RenameBuzzChannel(ctx context.Context, bindingID uuid.UUID, name string) error
	SetBuzzCommunityState(ctx context.Context, id uuid.UUID, status models.BuzzCommunityStatus, lastError string) error
	GetChatMessage(ctx context.Context, userID, id uuid.UUID) (*models.ChatMessage, error)
	GetJob(ctx context.Context, userID, id uuid.UUID) (*models.Job, error)
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
}

// Relay is how the service reaches a community (*Client satisfies it).
type Relay interface {
	Publish(ctx context.Context, ep Endpoint, s Signer, ev *nostr.Event) error
	Query(ctx context.Context, ep Endpoint, s Signer, filters ...Filter) ([]nostr.Event, error)
	Upload(ctx context.Context, ep Endpoint, s Signer, name, contentType string, data []byte) (*Blob, error)
	FetchMedia(ctx context.Context, ep Endpoint, s Signer, rawURL string, max int64) ([]byte, error)
}

// Notices posted to Buzz when a mention cannot be answered. They are neutral on purpose:
// nothing about the owner's account (credits, plan) is revealed.
const (
	NoticeFailed = "Sorry, I couldn't reply to that just now."
	NoticeBusy   = "I'm getting a lot of messages at once, so I skipped some. Mention me again if I missed yours."
)

// TurnSource is how the relay's turns are metered (metering.TurnSource).
const TurnSource = "buzz"

// UnrestrictedWarning and ThreadDeletedError are recorded on a binding (shown in the app,
// never posted to Buzz) when mentions are dropped because of the bound thread.
const (
	UnrestrictedWarning = "Paused: this thread is no longer sandboxed, so Buzz mentions are not answered. " +
		"Sandbox the thread again, or acknowledge that anyone allowed to mention the persona may use everything the thread can read."
	ThreadDeletedError = "The relay thread was deleted. Connect this channel to another thread."
)

// busyNoticeEvery bounds how often one binding posts NoticeBusy.
const busyNoticeEvery = time.Minute

// bindingSkew tolerates clock differences when dropping events older than their binding.
const bindingSkew = 5 * time.Second

// Service is the relay: it turns mentions in bound channels into turns in their relay threads
// and publishes replies back.
type Service struct {
	Store  Store
	Relay  Relay
	Turns  plugins.TurnStarter
	Logger *zap.Logger
	// Files reads the files saved with a reply, to upload them to the community. Nil posts
	// text only.
	Files relaykit.FileReader
	// Ingest saves the files posted with a mention to the relay thread. Nil leaves them out,
	// with a note to the persona.
	Ingest plugins.AttachmentIngester

	// TypingEvery refreshes the typing indicator while a turn runs.
	TypingEvery time.Duration
	// PollEvery is how often a waiting turn checks its job for failure.
	PollEvery time.Duration
	// TurnTimeout bounds how long one inbound turn holds its thread's queue.
	TurnTimeout time.Duration
	// Policy paces turns: how many mentions may wait as the next batch, and the agent-to-agent
	// loop guard (relaykit.Policy; zero fields take the defaults).
	Policy relaykit.Policy
	// PostRetries is how many times a failed publish is retried (transient errors only).
	PostRetries int
	// RetryBackoff is the wait before the first retry; it doubles each time.
	RetryBackoff time.Duration

	once    sync.Once
	turns   relaykit.TurnQueue[inboundWork]
	guard   *relaykit.LoopGuard
	replies relaykit.Replies
	busy    relaykit.Throttle
	names   nameCache
	// channels remembers when each binding's channel name was last checked.
	channels channelCheck
}

type inboundWork struct {
	target models.BuzzBindingTarget
	ev     nostr.Event
	// agent: the author is an agent (an owner attestation, or a bot profile).
	agent bool
}

func (s *Service) defaults() { s.once.Do(s.setDefaults) }

func (s *Service) setDefaults() {
	if s.TypingEvery <= 0 {
		s.TypingEvery = 8 * time.Second
	}
	if s.PollEvery <= 0 {
		s.PollEvery = 3 * time.Second
	}
	if s.TurnTimeout <= 0 {
		s.TurnTimeout = 10 * time.Minute
	}
	if s.PostRetries < 0 {
		s.PostRetries = 0
	} else if s.PostRetries == 0 {
		s.PostRetries = 2
	}
	if s.RetryBackoff <= 0 {
		s.RetryBackoff = 2 * time.Second
	}
	if s.Logger == nil {
		s.Logger = zap.NewNop()
	}
	s.turns.Max = s.Policy.BatchMax
	// A thread's mentions batch together, and so do the channel's top-level ones (root "").
	s.turns.Place = func(w inboundWork) string { return w.target.Binding.ID.String() + "/" + ThreadOf(w.ev).Root }
	s.turns.Run = s.runInbound
	s.guard = s.Policy.Guard()
	s.replies.TTL = s.TurnTimeout
	s.busy.Every = busyNoticeEvery
	s.names.ttl = 10 * time.Minute
	s.channels.ttl = 10 * time.Minute
}

// SignerFor is who the relay acts as for a persona: its key, with its owner's attestation when
// it has one. An attestation that no longer parses is left off (the relay then judges the
// persona on its own membership).
func SignerFor(agent models.BuzzAgentCredentials, authTag string) (Signer, error) {
	keys, err := nostr.KeysFromHex(agent.SecretKey)
	if err != nil {
		return Signer{}, err
	}
	s := Signer{Keys: keys}
	if authTag != "" {
		if att, err := nostr.ParseAttestation(authTag); err == nil {
			s.Attestation = att
		}
	}
	return s, nil
}

// EndpointFor is a stored community's endpoint (stored URLs were validated when added).
func EndpointFor(c models.BuzzCommunity) (Endpoint, error) {
	ep, _, err := ParseEndpoint(c.RelayURL, true)
	return ep, err
}

// HandleEvent is called for every event a binding's subscription delivers. It returns at once;
// an accepted mention starts a turn, or, while one runs in the relay thread, waits with the
// other mentions of its thread to be answered together (relaykit.TurnQueue).
func (s *Service) HandleEvent(ctx context.Context, bindingID uuid.UUID, ev nostr.Event) {
	s.defaults()
	target, err := s.Store.GetBuzzBindingTarget(ctx, bindingID)
	if err != nil || target == nil {
		return
	}
	agent, ok := s.admits(ctx, *target, ev)
	if !ok || !s.threadOpen(ctx, *target) {
		return
	}
	if s.turns.Enqueue(ctx, target.Binding.ChatID, inboundWork{target: *target, ev: ev, agent: agent}) {
		return
	}
	s.Logger.Warn("buzz relay: queue full; dropping mention",
		zap.String("binding_id", bindingID.String()), zap.String("event_id", ev.ID))
	// Never to an agent: a notice it answers would feed the flood.
	if !agent && s.busy.Due(bindingID) {
		go s.notice(context.WithoutCancel(ctx), *target, ev, NoticeBusy)
	}
}

// admits applies the trigger rules to an event on a binding: a message in the bound channel,
// newer than the binding, not the persona's own, from someone the lists allow, that mentions
// the persona or continues a conversation with it, and not from an agent unless the binding
// answers agents. It reports whether the author is an agent. The bound thread is checked
// apart (threadOpen).
func (s *Service) admits(ctx context.Context, t models.BuzzBindingTarget, ev nostr.Event) (agent, ok bool) {
	b := t.Binding
	if !IsChannelMessage(ev, b.ChannelID) || ev.PubKey == t.Agent.PubKey {
		return false, false
	}
	if !b.InboundEnabled || b.Status != models.BuzzBindingActive {
		return false, false
	}
	if time.Unix(ev.CreatedAt, 0).Before(b.CreatedAt.Add(-bindingSkew)) {
		return false, false // said before the channel was bound
	}
	if !relaykit.Allowed(ev.PubKey, b.AllowPubKeys, b.DenyPubKeys) {
		return false, false
	}
	if !Mentions(ev, t.Agent.PubKey) {
		thread := ThreadOf(ev)
		if thread.Root == "" {
			return false, false
		}
		ok, err := s.Store.BuzzThreadContinues(ctx, b.ID, thread.Root, thread.Parent, ev.PubKey)
		if err != nil || !ok {
			return false, false
		}
	}
	agent = s.isAgent(ctx, t, ev)
	if agent && !b.AnswerAgents {
		return true, false
	}
	return agent, true
}

// isAgent reports whether ev's author is an agent: the event carries an owner attestation
// (FromAgent), or the author's profile says bot (NIP-24; What Iff personas publish it, and
// so does a persona joined by invite, which has no attestation).
func (s *Service) isAgent(ctx context.Context, t models.BuzzBindingTarget, ev nostr.Event) bool {
	if FromAgent(ev) {
		return true
	}
	ep, err := EndpointFor(t.Community)
	if err != nil {
		return false
	}
	signer, err := SignerFor(t.Agent, t.AuthTag)
	if err != nil {
		return false
	}
	return s.names.profile(ctx, s.Relay, ep, signer, ev.PubKey).bot
}

// threadOpen is the fail-closed check on the bound thread (relaykit.ThreadOpen).
func (s *Service) threadOpen(ctx context.Context, t models.BuzzBindingTarget) bool {
	b := t.Binding
	chat, err := s.Store.GetChat(ctx, t.Agent.OwnerID, b.ChatID)
	open := relaykit.ThreadOpen(relaykit.Gate{
		Chat: chat, ChatErr: err,
		Acknowledged: b.AllowUnrestricted, Broken: b.Status == models.BuzzBindingBroken, LastError: b.LastError,
		Warning: UnrestrictedWarning, DeletedError: ThreadDeletedError,
		MarkBroken: func(reason string) {
			_ = s.Store.SetBuzzBindingStatus(ctx, b.ID, models.BuzzBindingBroken, reason)
		},
		SetLastError: func(msg string) { _ = s.Store.SetBuzzBindingStatus(ctx, b.ID, b.Status, msg) },
		Withdraw: func() {
			if err := s.Store.WithdrawBuzzBindingAcknowledgement(ctx, b.ID); err != nil {
				s.Logger.Warn("buzz relay: could not withdraw a stale acknowledgement",
					zap.String("binding_id", b.ID.String()), zap.Error(err))
			}
		},
	})
	if !open {
		s.Logger.Warn("buzz relay: bound thread is missing, or not sandboxed and not acknowledged; dropping mention",
			zap.String("binding_id", b.ID.String()), zap.String("chat_id", b.ChatID.String()), zap.Error(err))
	}
	return open
}

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

// OnReply is the reply hook: it publishes a finished reply when the turn came from Buzz (back
// into the thread it came from) or when a post was requested for the chat (the composer
// toggle or the post_to_buzz tool).
func (s *Service) OnReply(ctx context.Context, ev replyhook.Event) {
	s.defaults()
	postedTo := map[uuid.UUID]bool{}

	if ev.TriggerMessageID != nil {
		inbound, err := s.Store.FindInboundBuzzLink(ctx, *ev.TriggerMessageID)
		if err != nil {
			s.Logger.Warn("buzz relay: find inbound link", zap.Error(err))
		}
		if inbound != nil {
			s.postInboundReply(ctx, inbound, ev, postedTo)
		} else if err == nil && s.turns.Active(ev.ChatID) {
			s.replies.KeepOrphan(ev)
			// runInbound attaches the link right after the turn starts, then takes any orphan.
			// If it attached between the lookup above and KeepOrphan, look once more.
			if again, err := s.Store.FindInboundBuzzLink(ctx, *ev.TriggerMessageID); err == nil && again != nil {
				if _, mine := s.replies.TakeOrphan(*ev.TriggerMessageID); mine {
					s.postInboundReply(ctx, again, ev, postedTo)
				}
			}
		}
	}

	bindings, err := s.Store.ConsumeBuzzPendingPosts(ctx, ev.UserID, ev.ChatID)
	if err != nil {
		s.Logger.Warn("buzz relay: pending posts", zap.Error(err))
		return
	}
	for _, id := range bindings {
		if postedTo[id] {
			continue
		}
		target, err := s.Store.GetBuzzBindingTarget(ctx, id)
		if err != nil || target.Agent.OwnerID != ev.UserID {
			continue
		}
		s.post(ctx, *target, ev.MessageID, nil, "", nil)
	}
}

// postInboundReply publishes a finished reply into the thread its inbound link came from,
// mentioning the person who asked.
func (s *Service) postInboundReply(ctx context.Context, inbound *models.BuzzMessageLink, ev replyhook.Event, postedTo map[uuid.UUID]bool) {
	if target, err := s.Store.GetBuzzBindingTarget(ctx, inbound.BindingID); err == nil {
		var thread *ThreadRef
		if inbound.EventID != nil {
			root := inbound.ThreadRoot
			if root == "" {
				root = *inbound.EventID
			}
			thread = &ThreadRef{Root: root, Parent: *inbound.EventID}
		}
		linkID := inbound.ID
		s.post(ctx, *target, ev.MessageID, thread, inbound.AuthorPubKey, &linkID)
		postedTo[target.Binding.ID] = true
	}
	s.replies.Release(*ev.TriggerMessageID)
}

// post publishes one saved reply to a binding's channel, at most once (the outbound link is
// the record), retrying transient failures and recording on the binding or community when the
// relay says the persona may no longer post.
func (s *Service) post(ctx context.Context, t models.BuzzBindingTarget, messageID uuid.UUID, thread *ThreadRef, mention string, replyToLink *uuid.UUID) {
	s.defaults()
	b := t.Binding
	log := s.Logger.With(zap.String("binding_id", b.ID.String()), zap.String("message_id", messageID.String()))
	root := ""
	if thread != nil {
		root = thread.Root
	}
	link, created, err := s.Store.StartOutboundBuzzLink(ctx, b.ID, messageID, root, replyToLink)
	if err != nil {
		log.Error("buzz relay: start outbound", zap.Error(err))
		return
	}
	if !created && link.Status == models.BuzzLinkSent {
		return
	}
	ep, err := EndpointFor(t.Community)
	if err != nil {
		_ = s.Store.FinishOutboundBuzzLink(ctx, link.ID, nil, err)
		return
	}
	signer, err := SignerFor(t.Agent, t.AuthTag)
	if err != nil {
		_ = s.Store.FinishOutboundBuzzLink(ctx, link.ID, nil, err)
		return
	}
	reply, err := s.Store.GetChatMessage(ctx, t.Agent.OwnerID, messageID)
	if err != nil {
		log.Error("buzz relay: read reply", zap.Error(err))
		_ = s.Store.FinishOutboundBuzzLink(ctx, link.ID, nil, err)
		return
	}

	media, skipped := s.uploadReplyFiles(ctx, ep, signer, reply)
	text := reply.Message
	if len(skipped) > 0 {
		text = strings.TrimRight(text, "\n") + "\n\n_" + skippedNote(skipped) + "_"
	}
	parts := SplitMessage(text)
	if len(parts) == 0 && len(media) == 0 {
		_ = s.Store.FinishOutboundBuzzLink(ctx, link.ID, nil, nil)
		return
	}
	if len(parts) == 0 {
		parts = []string{""}
	}

	var mentions []string
	if mention != "" {
		mentions = []string{mention}
	}
	var ids []string
	for i, part := range parts {
		var partMedia [][]string
		partMentions := mentions
		if i == 0 {
			partMedia = media
		} else {
			partMentions = nil // notify once
		}
		var id string
		id, err = s.publishWithRetry(ctx, ep, signer, func() (*nostr.Event, error) {
			return MessageEvent(signer, b.ChannelID, part, thread, partMentions, partMedia)
		})
		if err != nil {
			break
		}
		ids = append(ids, id)
	}
	if ferr := s.Store.FinishOutboundBuzzLink(ctx, link.ID, ids, err); ferr != nil {
		log.Error("buzz relay: finish outbound", zap.Error(ferr))
	}
	s.recordPublishError(ctx, t, err)
	if err == nil {
		_ = s.Store.TouchBuzzBinding(ctx, b.ID)
	} else {
		log.Warn("buzz relay: publish failed", zap.Error(err))
	}
}

// publishWithRetry signs (fresh each attempt, so created_at stays current) and publishes an
// event, retrying only transient failures. It returns the published event's id.
func (s *Service) publishWithRetry(ctx context.Context, ep Endpoint, signer Signer, build func() (*nostr.Event, error)) (string, error) {
	backoff := s.RetryBackoff
	for attempt := 0; ; attempt++ {
		ev, err := build()
		if err != nil {
			return "", err
		}
		err = s.Relay.Publish(ctx, ep, signer, ev)
		if err == nil {
			return ev.ID, nil
		}
		var rejected *RejectedError
		if attempt >= s.PostRetries || errors.As(err, &rejected) || errors.Is(err, ErrNoAccess) {
			return "", err
		}
		if !relaykit.SleepCtx(ctx, backoff) {
			return "", err
		}
		backoff *= 2
	}
}

// recordPublishError marks the community or binding when the relay refused the persona, so
// the app can say why replies stop arriving. Transient failures leave both alone.
func (s *Service) recordPublishError(ctx context.Context, t models.BuzzBindingTarget, err error) {
	var rejected *RejectedError
	switch {
	case err == nil:
	case errors.Is(err, ErrNoAccess):
		_ = s.Store.SetBuzzCommunityState(ctx, t.Community.ID, models.BuzzCommunityNeedsAccess,
			"The community no longer lets this persona post. Ask an admin to add it again.")
	case errors.As(err, &rejected) && IsAccessReason(rejected.Reason):
		_ = s.Store.SetBuzzBindingStatus(ctx, t.Binding.ID, models.BuzzBindingBroken,
			"Buzz refused the persona's message: "+rejected.Reason)
	}
}

// notice publishes a short reply to the triggering message; failures are ignored.
func (s *Service) notice(ctx context.Context, t models.BuzzBindingTarget, ev nostr.Event, text string) {
	ep, err := EndpointFor(t.Community)
	if err != nil {
		return
	}
	signer, err := SignerFor(t.Agent, t.AuthTag)
	if err != nil {
		return
	}
	thread := ReplyThread(ev)
	msg, err := MessageEvent(signer, t.Binding.ChannelID, text, &thread, nil, nil)
	if err != nil {
		return
	}
	if err := s.Relay.Publish(ctx, ep, signer, msg); err != nil {
		s.Logger.Debug("buzz relay: notice failed", zap.Error(err))
	}
}

// nameCache remembers authors' display names (kind 0) for a while, so a busy channel does not
// look every author up on every mention.
type nameCache struct {
	ttl time.Duration

	mu    sync.Mutex
	names map[string]cachedName
}

type cachedName struct {
	name string
	// bot: the profile marks an automated account.
	bot bool
	at  time.Time
}

// name is pubkey's display name in the community, or a short npub when it has no profile.
func (c *nameCache) name(ctx context.Context, relay Relay, ep Endpoint, signer Signer, pubkey string) string {
	return c.profile(ctx, relay, ep, signer, pubkey).name
}

// profile is what pubkey's profile (kind 0) says, looked up at most once per ttl.
func (c *nameCache) profile(ctx context.Context, relay Relay, ep Endpoint, signer Signer, pubkey string) cachedName {
	key := ep.Host() + "|" + pubkey
	c.mu.Lock()
	if n, ok := c.names[key]; ok && time.Since(n.at) < c.ttl {
		c.mu.Unlock()
		return n
	}
	c.mu.Unlock()

	var p Profile
	events, err := relay.Query(ctx, ep, signer, Filter{"kinds": []int{KindProfile}, "authors": []string{pubkey}, "limit": 1})
	if err == nil && len(events) > 0 && events[0].Verify() == nil && events[0].PubKey == pubkey {
		p = ParseProfile(events[0])
	}
	n := cachedName{name: p.Label(), bot: p.Bot, at: time.Now()}
	if strings.TrimSpace(n.name) == "" {
		n.name = ShortNPub(pubkey)
	}
	c.mu.Lock()
	if c.names == nil {
		c.names = map[string]cachedName{}
	}
	if len(c.names) > 5000 {
		c.names = map[string]cachedName{}
	}
	c.names[key] = n
	c.mu.Unlock()
	return n
}

// channelCheck remembers when each binding's channel name was last read from the relay, so a
// busy channel reads it at most once per ttl.
type channelCheck struct {
	ttl time.Duration

	mu      sync.Mutex
	checked map[uuid.UUID]time.Time
}

// due reports whether the binding's channel name should be read again, and records it.
func (c *channelCheck) due(bindingID uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.checked[bindingID]; ok && time.Since(at) < c.ttl {
		return false
	}
	if c.checked == nil || len(c.checked) > 5000 {
		c.checked = map[uuid.UUID]time.Time{}
	}
	c.checked[bindingID] = time.Now()
	return true
}

// channelName is the bound channel's current name from its relay-signed metadata (kind
// 39000), read when a mention is answered and at most once per ttl; "" when not due or unknown.
// A channel renamed in Buzz is then renamed on the binding, as the Discord relay does.
func (s *Service) channelName(ctx context.Context, t models.BuzzBindingTarget, ep Endpoint, signer Signer) string {
	if !s.channels.due(t.Binding.ID) {
		return ""
	}
	events, err := s.Relay.Query(ctx, ep, signer, Filter{"kinds": []int{KindGroupMetadata}, "#d": []string{t.Binding.ChannelID}, "limit": 1})
	if err != nil {
		return ""
	}
	var newest *nostr.Event
	for i, ev := range events {
		if ev.Verify() != nil || ev.Tags.Value("d") != t.Binding.ChannelID {
			continue
		}
		if t.Community.RelayPubKey != "" && ev.PubKey != t.Community.RelayPubKey {
			continue // channel state is relay-signed
		}
		if newest == nil || ev.CreatedAt > newest.CreatedAt {
			newest = &events[i]
		}
	}
	if newest == nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(newest.Tags.Value("name")), "#")
}
