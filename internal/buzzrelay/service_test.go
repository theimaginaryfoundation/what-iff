package buzzrelay

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

// fakeStore is an in-memory Store for one owner and one binding.
type fakeStore struct {
	mu         sync.Mutex
	target     models.BuzzBindingTarget
	links      []*models.BuzzMessageLink
	messages   map[uuid.UUID]*models.ChatMessage
	chat       *models.Chat
	chatErr    error
	pending    []uuid.UUID
	bindStatus models.BuzzBindingStatus
	bindError  string
	community  models.BuzzCommunityStatus
	continues  bool
	renamed    string
}

func (f *fakeStore) GetBuzzBindingTarget(_ context.Context, id uuid.UUID) (*models.BuzzBindingTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.target.Binding.ID {
		return nil, datastore.ErrBuzzBindingNotFound
	}
	t := f.target
	return &t, nil
}

func (f *fakeStore) RecordInboundBuzzMessage(_ context.Context, in models.BuzzInbound) (*models.BuzzMessageLink, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.Direction == "inbound" && l.EventID != nil && *l.EventID == in.EventID {
			return nil, false, nil
		}
	}
	id := in.EventID
	l := &models.BuzzMessageLink{ID: uuid.New(), BindingID: in.BindingID, Direction: "inbound", EventID: &id,
		ThreadRoot: in.ThreadRoot, AuthorPubKey: in.AuthorPubKey, AuthorName: in.AuthorName, Status: models.BuzzLinkReceived,
		AgentDepth: in.AgentDepth, CreatedAt: time.Now()}
	f.links = append(f.links, l)
	return l, true, nil
}

func (f *fakeStore) AttachBuzzLinkMessage(_ context.Context, linkID, msgID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.ID == linkID {
			l.ChatMessageID = &msgID
		}
	}
	return nil
}

func (f *fakeStore) DeleteBuzzLink(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links = slices.DeleteFunc(f.links, func(l *models.BuzzMessageLink) bool { return l.ID == id })
	return nil
}

func (f *fakeStore) FindInboundBuzzLink(_ context.Context, msgID uuid.UUID) (*models.BuzzMessageLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range slices.Backward(f.links) {
		if l.Direction == "inbound" && l.ChatMessageID != nil && *l.ChatMessageID == msgID {
			c := *l
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) LastInboundBuzzLink(_ context.Context, _ uuid.UUID, since time.Time) (*models.BuzzMessageLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range slices.Backward(f.links) {
		if l.Direction == "inbound" && l.CreatedAt.After(since) {
			c := *l
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) RenameBuzzChannel(_ context.Context, _ uuid.UUID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renamed = name
	f.target.Binding.ChannelName = name
	return nil
}

// inboundAttached counts inbound links attached to a saved message.
func (f *fakeStore) inboundAttached() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.links {
		if l.Direction == "inbound" && l.ChatMessageID != nil {
			n++
		}
	}
	return n
}

func (f *fakeStore) BuzzThreadContinues(context.Context, uuid.UUID, string, string, string) (bool, error) {
	return f.continues, nil
}

func (f *fakeStore) StartOutboundBuzzLink(_ context.Context, bindingID, msgID uuid.UUID, root string, replyTo *uuid.UUID) (*models.BuzzMessageLink, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.Direction == "outbound" && *l.ChatMessageID == msgID {
			c := *l
			return &c, false, nil
		}
	}
	l := &models.BuzzMessageLink{ID: uuid.New(), BindingID: bindingID, Direction: "outbound", ChatMessageID: &msgID, ThreadRoot: root, ReplyToLinkID: replyTo, Status: models.BuzzLinkPending}
	f.links = append(f.links, l)
	return l, true, nil
}

func (f *fakeStore) FinishOutboundBuzzLink(_ context.Context, id uuid.UUID, ids []string, err error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.ID == id {
			l.PostedEventIDs = ids
			l.Status = models.BuzzLinkSent
			if err != nil {
				l.Status = models.BuzzLinkFailed
			}
		}
	}
	return nil
}

func (f *fakeStore) ConsumeBuzzPendingPosts(context.Context, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.pending
	f.pending = nil
	return out, nil
}

func (f *fakeStore) SetBuzzBindingStatus(_ context.Context, _ uuid.UUID, st models.BuzzBindingStatus, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindStatus, f.bindError = st, msg
	f.target.Binding.Status = st
	if msg == "" {
		f.target.Binding.LastError = nil
	} else {
		f.target.Binding.LastError = &msg
	}
	return nil
}

func (f *fakeStore) WithdrawBuzzBindingAcknowledgement(context.Context, uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target.Binding.AllowUnrestricted = false
	return nil
}

func (f *fakeStore) TouchBuzzBinding(context.Context, uuid.UUID) error { return nil }

func (f *fakeStore) SetBuzzCommunityState(_ context.Context, _ uuid.UUID, st models.BuzzCommunityStatus, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.community = st
	return nil
}

func (f *fakeStore) GetChatMessage(_ context.Context, _, id uuid.UUID) (*models.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.messages[id]; ok {
		return m, nil
	}
	return nil, errors.New("no message")
}

func (f *fakeStore) GetJob(context.Context, uuid.UUID, uuid.UUID) (*models.Job, error) {
	return &models.Job{Status: models.JobStatusProcessing}, nil
}

func (f *fakeStore) GetChat(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chat, f.chatErr
}

func (f *fakeStore) GetPersonality(context.Context, uuid.UUID, uuid.UUID) (*models.Personality, error) {
	return &models.Personality{Name: "Vix"}, nil
}

// fakeRelay records what is published and answers profile and id queries.
type fakeRelay struct {
	mu        sync.Mutex
	published []nostr.Event
	profiles  map[string]Profile
	events    map[string]nostr.Event
	keys      map[string]*nostr.Keys
	// metadata answers channel metadata (kind 39000) queries.
	metadata []nostr.Event
	err      error
}

func (r *fakeRelay) Publish(_ context.Context, _ Endpoint, _ Signer, ev *nostr.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.published = append(r.published, *ev)
	return nil
}

func (r *fakeRelay) messages() []nostr.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []nostr.Event
	for _, ev := range r.published {
		if ev.Kind == KindStreamMessage {
			out = append(out, ev)
		}
	}
	return out
}

func (r *fakeRelay) Query(_ context.Context, _ Endpoint, _ Signer, filters ...Filter) ([]nostr.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := filters[0]
	if ids, ok := f["ids"].([]string); ok {
		if ev, ok := r.events[ids[0]]; ok {
			return []nostr.Event{ev}, nil
		}
		return nil, nil
	}
	if kinds, ok := f["kinds"].([]int); ok && slices.Contains(kinds, KindGroupMetadata) {
		return append([]nostr.Event(nil), r.metadata...), nil
	}
	if authors, ok := f["authors"].([]string); ok {
		p, ok := r.profiles[authors[0]]
		k := r.keys[authors[0]]
		if !ok || k == nil {
			return nil, nil
		}
		ev, _ := ProfileEvent(Signer{Keys: k}, p)
		return []nostr.Event{*ev}, nil
	}
	return nil, nil
}

func (r *fakeRelay) Upload(_ context.Context, _ Endpoint, _ Signer, name, _ string, _ []byte) (*Blob, error) {
	return &Blob{URL: "https://acme.example/media/abc.png", SHA256: "abc", Size: 3, Type: "image/png", Name: name}, nil
}

func (r *fakeRelay) FetchMedia(context.Context, Endpoint, Signer, string, int64) ([]byte, error) {
	return []byte("png"), nil
}

// fakeTurns records turns and saves their user messages.
type fakeTurns struct {
	mu    sync.Mutex
	turns []plugins.UserTurn
	ids   []uuid.UUID
	err   error
}

func (f *fakeTurns) StartUserTurn(_ context.Context, t plugins.UserTurn) (*models.ChatMessageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	id := uuid.New()
	f.turns = append(f.turns, t)
	f.ids = append(f.ids, id)
	return &models.ChatMessageResponse{ID: id, JobID: uuid.NewString()}, nil
}

func (f *fakeTurns) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.turns)
}

type harness struct {
	ctx    context.Context
	store  *fakeStore
	relay  *fakeRelay
	turns  *fakeTurns
	svc    *Service
	agent  *nostr.Keys
	alice  *nostr.Keys
	target models.BuzzBindingTarget
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	agent, _ := nostr.GenerateKeys()
	alice, _ := nostr.GenerateKeys()
	target := models.BuzzBindingTarget{
		Binding: models.BuzzBinding{ID: uuid.New(), ChatID: uuid.New(), ChannelID: "chan-1", ChannelName: "general",
			InboundEnabled: true, Status: models.BuzzBindingActive, CreatedAt: time.Now().Add(-time.Hour)},
		Community: models.BuzzCommunity{ID: uuid.New(), RelayURL: "wss://acme.example", Name: "Acme"},
		Agent:     models.BuzzAgentCredentials{ID: uuid.New(), OwnerID: uuid.New(), PersonalityID: uuid.New(), PubKey: agent.PublicKey(), SecretKey: agent.SecretHex()},
	}
	store := &fakeStore{target: target, messages: map[uuid.UUID]*models.ChatMessage{},
		chat: &models.Chat{ID: target.Binding.ChatID, ContextScope: models.ContextScopeSandbox}}
	relay := &fakeRelay{profiles: map[string]Profile{alice.PublicKey(): {DisplayName: "alice"}},
		keys: map[string]*nostr.Keys{alice.PublicKey(): alice}, events: map[string]nostr.Event{}}
	turns := &fakeTurns{}
	svc := &Service{Store: store, Relay: relay, Turns: turns, Logger: zap.NewNop(),
		TypingEvery: time.Hour, PollEvery: time.Hour, TurnTimeout: 200 * time.Millisecond, RetryBackoff: time.Millisecond}
	return &harness{ctx: ctx, store: store, relay: relay, turns: turns, svc: svc, agent: agent, alice: alice, target: target}
}

// say publishes a kind 9 message from k in the bound channel.
func (h *harness) say(t *testing.T, k *nostr.Keys, content string, thread *ThreadRef, mentions ...string) nostr.Event {
	t.Helper()
	ev, err := MessageEvent(Signer{Keys: k}, "chan-1", content, thread, mentions, nil)
	require.NoError(t, err)
	h.relay.mu.Lock()
	h.relay.events[ev.ID] = *ev
	h.relay.mu.Unlock()
	return *ev
}

// replyTo simulates the agent finishing its reply to the turn'th turn.
func (h *harness) replyTo(t *testing.T, turn int, text string) uuid.UUID {
	t.Helper()
	h.turns.mu.Lock()
	userMsg := h.turns.ids[turn]
	h.turns.mu.Unlock()
	replyID := uuid.New()
	h.store.mu.Lock()
	h.store.messages[replyID] = &models.ChatMessage{ID: replyID, Message: text}
	h.store.mu.Unlock()
	h.svc.OnReply(h.ctx, replyhook.Event{UserID: h.target.Agent.OwnerID, ChatID: h.target.Binding.ChatID, MessageID: replyID, TriggerMessageID: &userMsg})
	return replyID
}

func TestAMentionStartsATurnAndTheReplyThreadsBack(t *testing.T) {
	h := newHarness(t)
	ask := h.say(t, h.alice, "@Vix what's the plan?", nil, h.agent.PublicKey())
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ask)
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)

	turn := h.turns.turns[0]
	assert.Equal(t, "alice (Buzz, #general): what's the plan?", turn.Message.Message)
	assert.Equal(t, TurnSource, turn.Source)
	assert.Equal(t, h.target.Agent.OwnerID, turn.UserID)
	assert.Empty(t, turn.Timezone, "the owner's time zone is not exposed")
	require.Eventually(t, func() bool {
		h.store.mu.Lock()
		defer h.store.mu.Unlock()
		return h.store.links[0].ChatMessageID != nil
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, "alice", h.store.links[0].AuthorName)

	h.replyTo(t, 0, "**Ship it** on Friday.")
	msgs := h.relay.messages()
	require.Len(t, msgs, 1)
	reply := msgs[0]
	assert.NoError(t, reply.Verify())
	assert.Equal(t, h.agent.PublicKey(), reply.PubKey)
	assert.Equal(t, "**Ship it** on Friday.", reply.Content)
	assert.Equal(t, ThreadRef{Root: ask.ID, Parent: ask.ID}, ThreadOf(reply))
	assert.Equal(t, []string{h.alice.PublicKey()}, reply.Tags.Values("p"), "only the asker is notified")

	// A retried hook does not post twice.
	h.store.mu.Lock()
	userMsg := h.turns.ids[0]
	h.store.mu.Unlock()
	var replyID uuid.UUID
	for _, l := range h.store.links {
		if l.Direction == "outbound" {
			replyID = *l.ChatMessageID
		}
	}
	h.svc.OnReply(h.ctx, replyhook.Event{UserID: h.target.Agent.OwnerID, ChatID: h.target.Binding.ChatID, MessageID: replyID, TriggerMessageID: &userMsg})
	assert.Len(t, h.relay.messages(), 1)
}

func TestWhatIsNeverAnswered(t *testing.T) {
	h := newHarness(t)
	mention := []string{h.agent.PublicKey()}
	other, _ := nostr.GenerateKeys()
	owner, _ := nostr.GenerateKeys()
	att, _ := nostr.Attest(owner, other.PublicKey(), "")

	own := h.say(t, h.agent, "talking to myself", nil, mention...)
	notMentioned := h.say(t, h.alice, "just chatting", nil)
	otherChannel, _ := MessageEvent(Signer{Keys: h.alice}, "chan-2", "hi", nil, mention, nil)
	fromAgent, _ := MessageEvent(Signer{Keys: other, Attestation: att}, "chan-1", "beep", nil, mention, nil)
	old := &nostr.Event{Kind: KindStreamMessage, Tags: nostr.Tags{{"h", "chan-1"}, {"p", h.agent.PublicKey()}}, Content: "ancient",
		CreatedAt: time.Now().Add(-2 * time.Hour).Unix()}
	require.NoError(t, old.Sign(h.alice))

	for name, ev := range map[string]nostr.Event{
		"own": own, "not mentioned": notMentioned, "other channel": *otherChannel, "from an agent": *fromAgent, "before binding": *old,
	} {
		h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ev)
		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, 0, h.turns.count(), name)
	}

	h.store.target.Binding.DenyPubKeys = []string{h.alice.PublicKey()}
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, h.say(t, h.alice, "denied", nil, mention...))
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h.turns.count(), "deny wins")

	h.store.target.Binding.DenyPubKeys = nil
	h.store.target.Binding.AnswerAgents = true
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, *fromAgent)
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond, "answer_agents lets agents in")
}

func TestARedeliveredEventIsAnsweredOnce(t *testing.T) {
	h := newHarness(t)
	ask := h.say(t, h.alice, "hello", nil, h.agent.PublicKey())
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ask)
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(250 * time.Millisecond) // let the first turn time out of the queue
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ask)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, h.turns.count())
}

func TestAThreadContinuationNeedsNoMention(t *testing.T) {
	h := newHarness(t)
	follow := h.say(t, h.alice, "and then?", &ThreadRef{Root: "root", Parent: "persona-msg"})
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, follow)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h.turns.count())

	h.store.continues = true
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, follow)
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)
}

func TestAReplyToSomeoneElseQuotesThem(t *testing.T) {
	h := newHarness(t)
	bob, _ := nostr.GenerateKeys()
	h.relay.profiles[bob.PublicKey()] = Profile{Name: "bob"}
	h.relay.keys[bob.PublicKey()] = bob
	root := h.say(t, bob, "we should rewrite it in Rust", nil)
	ask := h.say(t, h.alice, "@vix thoughts?", &ThreadRef{Root: root.ID, Parent: root.ID}, h.agent.PublicKey())
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ask)
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "(replying to bob: \"we should rewrite it in Rust\")\nalice (Buzz, #general): thoughts?", h.turns.turns[0].Message.Message)

	h.replyTo(t, 0, "Maybe.")
	assert.Equal(t, ThreadRef{Root: root.ID, Parent: ask.ID}, ThreadOf(h.relay.messages()[0]), "answered in place in the existing thread")
}

func TestAnUnsandboxedThreadIsClosedUntilAcknowledged(t *testing.T) {
	h := newHarness(t)
	h.store.chat = &models.Chat{ID: h.target.Binding.ChatID, ContextScope: models.ContextScopeAccount}
	ask := h.say(t, h.alice, "hi", nil, h.agent.PublicKey())
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ask)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h.turns.count())
	assert.Equal(t, UnrestrictedWarning, h.store.bindError)

	h.store.target.Binding.AllowUnrestricted = true
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, ask)
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)

	h2 := newHarness(t)
	h2.store.chat, h2.store.chatErr = nil, datastore.ErrChatNotFound
	h2.svc.HandleEvent(h2.ctx, h2.target.Binding.ID, h2.say(t, h2.alice, "hi", nil, h2.agent.PublicKey()))
	assert.Equal(t, models.BuzzBindingBroken, h2.store.bindStatus)
	assert.Equal(t, ThreadDeletedError, h2.store.bindError)
}

func TestAFailedTurnPostsANeutralNotice(t *testing.T) {
	h := newHarness(t)
	h.turns.err = errors.New("out of credits for account 1234")
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, h.say(t, h.alice, "hi", nil, h.agent.PublicKey()))
	require.Eventually(t, func() bool { return len(h.relay.messages()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, NoticeFailed, h.relay.messages()[0].Content)
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	assert.Empty(t, h.store.links, "the inbound link is removed so a redelivery may try again")
}

func TestPublishRefusalsAreRecorded(t *testing.T) {
	h := newHarness(t)
	replyID := uuid.New()
	h.store.messages[replyID] = &models.ChatMessage{ID: replyID, Message: "x"}
	h.relay.err = &RejectedError{Reason: "restricted: not a channel member"}
	h.svc.post(h.ctx, h.target, replyID, nil, "", nil)
	assert.Equal(t, models.BuzzBindingBroken, h.store.bindStatus)
	assert.Contains(t, h.store.bindError, "not a channel member")

	other := uuid.New()
	h.store.messages[other] = &models.ChatMessage{ID: other, Message: "y"}
	h.relay.err = ErrNoAccess
	h.svc.post(h.ctx, h.target, other, nil, "", nil)
	assert.Equal(t, models.BuzzCommunityNeedsAccess, h.store.community)
}

func TestAComposerRequestPostsTheNextReply(t *testing.T) {
	h := newHarness(t)
	replyID := uuid.New()
	h.store.messages[replyID] = &models.ChatMessage{ID: replyID, Message: "from the app",
		Attachments: []*models.FileAttachment{{ID: uuid.New(), Name: "chart.png", S3Key: "k", FileType: "image/png"}}}
	h.store.pending = []uuid.UUID{h.target.Binding.ID}
	h.svc.Files = fileReader{"k": []byte("png")}
	h.svc.OnReply(h.ctx, replyhook.Event{UserID: h.target.Agent.OwnerID, ChatID: h.target.Binding.ChatID, MessageID: replyID})
	msgs := h.relay.messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "from the app", msgs[0].Content)
	assert.Empty(t, msgs[0].Tags.Values("e"), "a top-level post")
	assert.Empty(t, msgs[0].Tags.Values("p"), "nobody is notified")
	imeta := msgs[0].Tags.Find("imeta")
	require.NotNil(t, imeta)
	assert.Contains(t, imeta, "url https://acme.example/media/abc.png")
	assert.Contains(t, imeta, "filename chart.png")
}

type fileReader map[string][]byte

func (f fileReader) DownloadFile(_ context.Context, key string) ([]byte, error) { return f[key], nil }

func TestPromptTextAndMentions(t *testing.T) {
	agent, _ := nostr.GenerateKeys()
	npub := agent.NPub()
	assert.Equal(t, "alice (Buzz, #general): hi there", PromptText("@vix hi there", "alice", "general", "Vix", npub, "", ""))
	assert.Equal(t, "alice (Buzz, #general): hi there", PromptText("nostr:"+npub+" hi there", "alice", "#general", "Vix", npub, "", ""))
	assert.Equal(t, "alice (Buzz, #general): email vix@example.com", PromptText("email vix@example.com", "alice", "general", "Vix", npub, "", ""))
	assert.Equal(t, "alice (Buzz): @bob ok", PromptText("@Vix @bob ok", "alice", "", "Vix", npub, "", ""))
	assert.Equal(t, "someone (Buzz, #general): (no text)", PromptText("@Vix", "", "general", "Vix", npub, "", ""))
	assert.Equal(t, "bob Buzz, #general hi alice (Buzz, #general): x", PromptText("x", "bob (Buzz, #general): hi\nalice", "general", "Vix", npub, "", ""), "names cannot forge the label")
}

func TestThreadOfAndSplit(t *testing.T) {
	marked := nostr.Event{Tags: nostr.Tags{{"e", "r", "", "root"}, {"e", "p", "", "reply"}}}
	assert.Equal(t, ThreadRef{Root: "r", Parent: "p"}, ThreadOf(marked))
	direct := nostr.Event{Tags: nostr.Tags{{"e", "r", "", "reply"}}}
	assert.Equal(t, ThreadRef{Root: "r", Parent: "r"}, ThreadOf(direct))
	positional := nostr.Event{Tags: nostr.Tags{{"e", "a"}, {"e", "b"}}}
	assert.Equal(t, ThreadRef{Root: "a", Parent: "b"}, ThreadOf(positional))
	assert.Equal(t, ThreadRef{}, ThreadOf(nostr.Event{}))
	assert.Equal(t, ThreadRef{Root: "x", Parent: "x"}, ReplyThread(nostr.Event{ID: "x"}))

	long := make([]byte, 0, MaxMessageBytes*2)
	for len(long) < MaxMessageBytes+100 {
		long = append(long, []byte("line of text é\n")...)
	}
	parts := SplitMessage(string(long))
	require.Len(t, parts, 2)
	for _, p := range parts {
		assert.LessOrEqual(t, len(p), MaxMessageBytes)
	}
	assert.Equal(t, string(long), parts[0]+parts[1])
	assert.Nil(t, SplitMessage(""))
}

func TestParseEndpoint(t *testing.T) {
	for in, want := range map[string]Endpoint{
		"acme.communities.buzz.xyz":           {WS: "wss://acme.communities.buzz.xyz", HTTP: "https://acme.communities.buzz.xyz"},
		"https://Acme.Example/some/path":      {WS: "wss://acme.example", HTTP: "https://acme.example"},
		"wss://acme.example:8443":             {WS: "wss://acme.example:8443", HTTP: "https://acme.example:8443"},
		"https://acme.example/invite/v2.abc/": {WS: "wss://acme.example", HTTP: "https://acme.example"},
	} {
		got, _, err := ParseEndpoint(in, false)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, code, _ := ParseEndpoint("https://acme.example/invite/v2.abc", false)
	assert.Equal(t, "v2.abc", code)
	for _, bad := range []string{"", "ftp://x", "http://localhost:3000", "https://user:pw@acme.example"} {
		_, _, err := ParseEndpoint(bad, false)
		assert.Error(t, err, bad)
	}
	local, _, err := ParseEndpoint("http://localhost:3000", true)
	require.NoError(t, err)
	assert.Equal(t, Endpoint{WS: "ws://localhost:3000", HTTP: "http://localhost:3000"}, local)
}

// person adds a community member with a profile.
func (h *harness) person(name string, bot bool) *nostr.Keys {
	k, _ := nostr.GenerateKeys()
	h.relay.mu.Lock()
	h.relay.profiles[k.PublicKey()] = Profile{DisplayName: name, Bot: bot}
	h.relay.keys[k.PublicKey()] = k
	h.relay.mu.Unlock()
	return k
}

func TestMentionsThatWaitAreAnsweredInOneBatch(t *testing.T) {
	h := newHarness(t)
	h.svc.turns.Gather = -1
	h.svc.TurnTimeout = time.Hour // each turn holds the thread until its reply
	bob, carol := h.person("bob", false), h.person("carol", false)
	mention := h.agent.PublicKey()

	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, h.say(t, h.alice, "first", nil, mention))
	require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, h.say(t, bob, "second", nil, mention))
	last := h.say(t, carol, "third", nil, mention)
	h.svc.HandleEvent(h.ctx, h.target.Binding.ID, last)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 1, h.turns.count(), "the burst waits")

	require.Eventually(t, func() bool { return h.store.inboundAttached() == 1 }, time.Second, 5*time.Millisecond)
	h.replyTo(t, 0, "one")
	require.Eventually(t, func() bool { return h.turns.count() == 2 }, time.Second, 5*time.Millisecond)
	h.turns.mu.Lock()
	prompt := h.turns.turns[1].Message.Message
	h.turns.mu.Unlock()
	assert.Contains(t, prompt, "(Relay note: 2 messages arrived")
	assert.Contains(t, prompt, "bob (Buzz, #general): second\ncarol (Buzz, #general): third")

	require.Eventually(t, func() bool { return h.store.inboundAttached() == 3 }, time.Second, 5*time.Millisecond)
	h.replyTo(t, 1, "both of you")
	msgs := h.relay.messages()
	require.Len(t, msgs, 2, "one reply for the batch")
	assert.Equal(t, ThreadRef{Root: last.ID, Parent: last.ID}, ThreadOf(msgs[1]), "in the newest mention's thread")
	assert.Equal(t, []string{carol.PublicKey()}, msgs[1].Tags.Values("p"), "only the newest asker is notified")
}

func TestAgentsAndTheLoopGuard(t *testing.T) {
	tests := []struct {
		name         string
		answerAgents bool
		prev         int // the binding's newest recorded depth; -1 for none
		wantTurn     bool
		wantDepth    int
		wantHint     string
	}{
		{name: "a bot profile counts as an agent", prev: -1},
		{name: "answered when the binding allows it", answerAgents: true, prev: -1, wantTurn: true, wantDepth: 1},
		{name: "wrap-up hint near the cap", answerAgents: true, prev: 1, wantTurn: true, wantDepth: 2, wantHint: "Start wrapping up"},
		{name: "past the cap", answerAgents: true, prev: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.store.target.Binding.AnswerAgents = tt.answerAgents
			nova := h.person("Nova", true) // a persona joined by invite: no attestation, a bot profile
			if tt.prev >= 0 {
				_, _, err := h.store.RecordInboundBuzzMessage(h.ctx, models.BuzzInbound{BindingID: h.target.Binding.ID, EventID: "before", AgentDepth: tt.prev})
				require.NoError(t, err)
			}
			h.svc.HandleEvent(h.ctx, h.target.Binding.ID, h.say(t, nova, "hey Vix", nil, h.agent.PublicKey()))
			if !tt.wantTurn {
				time.Sleep(30 * time.Millisecond)
				assert.Equal(t, 0, h.turns.count())
				assert.Empty(t, h.relay.messages(), "nothing is posted to an agent")
				return
			}
			require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)
			last, err := h.store.LastInboundBuzzLink(h.ctx, h.target.Binding.ID, time.Now().Add(-time.Hour))
			require.NoError(t, err)
			assert.Equal(t, tt.wantDepth, last.AgentDepth)
			h.turns.mu.Lock()
			prompt := h.turns.turns[0].Message.Message
			h.turns.mu.Unlock()
			if tt.wantHint == "" {
				assert.NotContains(t, prompt, "Relay note")
			} else {
				assert.Contains(t, prompt, tt.wantHint)
			}
		})
	}
}

func TestARenamedChannelIsRecordedOnTheBinding(t *testing.T) {
	relayKeys, _ := nostr.GenerateKeys()
	stranger, _ := nostr.GenerateKeys()
	tests := []struct {
		name     string
		signer   *nostr.Keys
		relayPub string
		want     string
	}{
		{name: "relay-signed metadata", signer: relayKeys, relayPub: relayKeys.PublicKey(), want: "lounge"},
		{name: "metadata someone else signed is ignored", signer: stranger, relayPub: relayKeys.PublicKey(), want: "general"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.store.target.Community.RelayPubKey = tt.relayPub
			meta := &nostr.Event{Kind: KindGroupMetadata, Tags: nostr.Tags{{"d", "chan-1"}, {"name", "lounge"}}, CreatedAt: time.Now().Unix()}
			require.NoError(t, meta.Sign(tt.signer))
			h.relay.metadata = []nostr.Event{*meta}

			h.svc.HandleEvent(h.ctx, h.target.Binding.ID, h.say(t, h.alice, "hi", nil, h.agent.PublicKey()))
			require.Eventually(t, func() bool { return h.turns.count() == 1 }, time.Second, 5*time.Millisecond)
			h.turns.mu.Lock()
			prompt := h.turns.turns[0].Message.Message
			h.turns.mu.Unlock()
			assert.Contains(t, prompt, "(Buzz, #"+tt.want+")")
			h.store.mu.Lock()
			assert.Equal(t, tt.want, h.store.target.Binding.ChannelName)
			h.store.mu.Unlock()
		})
	}
}
