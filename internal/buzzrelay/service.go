package buzzrelay

import (
	"context"
	"errors"
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
