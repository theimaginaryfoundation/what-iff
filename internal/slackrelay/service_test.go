package slackrelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

// fakeStore is an in-memory Store for one owner, one app and one binding.
type fakeStore struct {
	mu         sync.Mutex
	target     models.SlackBindingTarget
	links      []*models.SlackMessageLink
	messages   map[uuid.UUID]*models.ChatMessage
	chat       *models.Chat
	chatErr    error
	pending    []uuid.UUID
	bindStatus models.SlackBindingStatus
	bindError  string
	appStatus  models.SlackAppStatus
	appError   string
	continues  bool
}

func (f *fakeStore) FindSlackBindingTarget(_ context.Context, appID uuid.UUID, channel string) (*models.SlackBindingTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if appID != f.target.App.ID || channel != f.target.Binding.ChannelID {
		return nil, datastore.ErrSlackBindingNotFound
	}
	t := f.target
	return &t, nil
}

func (f *fakeStore) GetSlackBindingTarget(_ context.Context, id uuid.UUID) (*models.SlackBindingTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.target.Binding.ID {
		return nil, datastore.ErrSlackBindingNotFound
	}
	t := f.target
	return &t, nil
}

func (f *fakeStore) RecordInboundSlackMessage(_ context.Context, in models.SlackInbound) (*models.SlackMessageLink, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.Direction == "inbound" && l.MessageTS != nil && *l.MessageTS == in.MessageTS {
			return nil, false, nil
		}
	}
	ts := in.MessageTS
	l := &models.SlackMessageLink{ID: uuid.New(), BindingID: in.BindingID, Direction: "inbound", MessageTS: &ts,
		ThreadTS: in.ThreadTS, AuthorUserID: in.AuthorUserID, AuthorName: in.AuthorName, Status: models.SlackLinkReceived,
		AgentDepth: in.AgentDepth, CreatedAt: time.Now()}
	f.links = append(f.links, l)
	return l, true, nil
}

func (f *fakeStore) AttachSlackLinkMessage(_ context.Context, linkID, msgID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.ID == linkID {
			l.ChatMessageID = &msgID
		}
	}
	return nil
}

func (f *fakeStore) DeleteSlackLink(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links = slices.DeleteFunc(f.links, func(l *models.SlackMessageLink) bool { return l.ID == id })
	return nil
}

func (f *fakeStore) FindInboundSlackLink(_ context.Context, msgID uuid.UUID) (*models.SlackMessageLink, error) {
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

func (f *fakeStore) LastInboundSlackLink(_ context.Context, _ uuid.UUID, since time.Time) (*models.SlackMessageLink, error) {
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

func (f *fakeStore) SlackThreadContinues(context.Context, uuid.UUID, string, string) (bool, error) {
	return f.continues, nil
}

func (f *fakeStore) StartOutboundSlackLink(_ context.Context, bindingID, msgID uuid.UUID, threadTS string, replyTo *uuid.UUID) (*models.SlackMessageLink, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.Direction == "outbound" && *l.ChatMessageID == msgID {
			c := *l
			return &c, false, nil
		}
	}
	l := &models.SlackMessageLink{ID: uuid.New(), BindingID: bindingID, Direction: "outbound", ChatMessageID: &msgID,
		ThreadTS: threadTS, ReplyToLinkID: replyTo, Status: models.SlackLinkPending}
	f.links = append(f.links, l)
	return l, true, nil
}

func (f *fakeStore) FinishOutboundSlackLink(_ context.Context, id uuid.UUID, threadTS string, posted []string, err error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.ID == id {
			l.PostedTS = posted
			if threadTS != "" {
				l.ThreadTS = threadTS
			}
			l.Status = models.SlackLinkSent
			if err != nil {
				l.Status = models.SlackLinkFailed
			}
		}
	}
	return nil
}

func (f *fakeStore) ConsumeSlackPendingPosts(context.Context, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.pending
	f.pending = nil
	return out, nil
}

func (f *fakeStore) SetSlackBindingStatus(_ context.Context, _ uuid.UUID, st models.SlackBindingStatus, msg string) error {
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

func (f *fakeStore) WithdrawSlackBindingAcknowledgement(context.Context, uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.target.Binding.AllowUnrestricted = false
	return nil
}

func (f *fakeStore) TouchSlackBinding(context.Context, uuid.UUID) error { return nil }

func (f *fakeStore) RenameSlackChannel(_ context.Context, appID uuid.UUID, channel, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if appID == f.target.App.ID && channel == f.target.Binding.ChannelID {
		f.target.Binding.ChannelName = name
	}
	return nil
}

func (f *fakeStore) BreakSlackChannel(_ context.Context, appID uuid.UUID, channel, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if appID == f.target.App.ID && channel == f.target.Binding.ChannelID {
		f.bindStatus, f.bindError = models.SlackBindingBroken, reason
	}
	return nil
}

func (f *fakeStore) SetSlackAppState(_ context.Context, _ uuid.UUID, st models.SlackAppStatus, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appStatus, f.appError = st, msg
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

// post is one chat.postMessage the fake received.
type post struct {
	Channel, ThreadTS, Text, TS string
}

// upload is one file the fake received.
type upload struct {
	Channel, ThreadTS, Name string
}

// fakeSlack records posts, reactions and uploads, and answers member and thread lookups.
type fakeSlack struct {
	mu        sync.Mutex
	posts     []post
	reactions []string // "+eyes@ts", "-eyes@ts"
	uploads   []upload
	users     map[string]User
	parents   map[string]Message
	files     map[string][]byte
	// errs is returned by PostMessage, one per call, before it succeeds.
	errs []error
	seq  int
}

func (s *fakeSlack) PostMessage(_ context.Context, _, channel, threadTS, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return "", err
	}
	s.seq++
	ts := fmt.Sprintf("9000.%04d", s.seq)
	s.posts = append(s.posts, post{Channel: channel, ThreadTS: threadTS, Text: text, TS: ts})
	return ts, nil
}

func (s *fakeSlack) AddReaction(_ context.Context, _, _, ts, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reactions = append(s.reactions, "+"+name+"@"+ts)
	return nil
}

func (s *fakeSlack) RemoveReaction(_ context.Context, _, _, ts, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reactions = append(s.reactions, "-"+name+"@"+ts)
	return nil
}

func (s *fakeSlack) UserInfo(_ context.Context, _, id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, &Error{Method: "users.info", Code: "user_not_found"}
	}
	return &u, nil
}

func (s *fakeSlack) ThreadParent(_ context.Context, _, _, threadTS string) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.parents[threadTS]; ok {
		return &m, nil
	}
	return nil, nil
}

func (s *fakeSlack) Download(_ context.Context, _, rawURL string, _ int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.files[rawURL]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

func (s *fakeSlack) UploadFile(_ context.Context, _, channel, threadTS, name string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads = append(s.uploads, upload{Channel: channel, ThreadTS: threadTS, Name: name})
	return nil
}

func (s *fakeSlack) sent() []post {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]post(nil), s.posts...)
}

func (s *fakeSlack) reacted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reactions...)
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

func (f *fakeTurns) prompt(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.turns[i].Message.Message
}

// fakeIngest saves every file it is given.
type fakeIngest struct{ names []string }

func (f *fakeIngest) IngestAttachment(_ context.Context, up plugins.AttachmentUpload) (*models.FileAttachment, error) {
	f.names = append(f.names, up.Name)
	return &models.FileAttachment{ID: uuid.New(), Name: up.Name}, nil
}

type fileReader map[string][]byte

func (f fileReader) DownloadFile(_ context.Context, key string) ([]byte, error) { return f[key], nil }

const (
	botUser = "UVIX"
	botID   = "BVIX"
	team    = "T1"
)

type harness struct {
	ctx    context.Context
	store  *fakeStore
	slack  *fakeSlack
	turns  *fakeTurns
	svc    *Service
	app    models.SlackAppCredentials
	target models.SlackBindingTarget
	seq    int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	app := models.SlackAppCredentials{ID: uuid.New(), OwnerID: uuid.New(), PersonalityID: uuid.New(), TeamID: team,
		AppID: "AVIX", BotUserID: botUser, BotID: botID, BotToken: "xoxb-test", AppToken: "xapp-test"}
	target := models.SlackBindingTarget{
		Binding: models.SlackBinding{ID: uuid.New(), AppID: app.ID, ChatID: uuid.New(), ChannelID: "C1", ChannelName: "general",
			InboundEnabled: true, Status: models.SlackBindingActive, CreatedAt: time.Now().Add(-time.Hour)},
		App: app,
	}
	store := &fakeStore{target: target, messages: map[uuid.UUID]*models.ChatMessage{},
		chat: &models.Chat{ID: target.Binding.ChatID, ContextScope: models.ContextScopeSandbox}}
	slack := &fakeSlack{
		users: map[string]User{
			"UALICE": {ID: "UALICE", Name: "alice"},
			"UBOB":   {ID: "UBOB", Name: "bob", RealName: "Bob Builder"},
			"UCAROL": {ID: "UCAROL", Name: "carol"},
			"UNOVA":  {ID: "UNOVA", Name: "nova", IsBot: true},
		},
		parents: map[string]Message{}, files: map[string][]byte{},
	}
	turns := &fakeTurns{}
	svc := &Service{Store: store, Slack: slack, Turns: turns, Logger: zap.NewNop(),
		PollEvery: time.Hour, TurnTimeout: 200 * time.Millisecond, RetryBackoff: time.Millisecond}
	return &harness{ctx: ctx, store: store, slack: slack, turns: turns, svc: svc, app: app, target: target}
}

// msg is a message from user in the bound channel, now.
func (h *harness) msg(user, text string) Message {
	h.seq++
	return Message{Type: "message", Channel: "C1", User: user, Text: text, TS: fmt.Sprintf("%d.%06d", time.Now().Unix(), h.seq)}
}

// deliver hands a message to the service the way the gateway does.
func (h *harness) deliver(t *testing.T, m Message) {
	t.Helper()
	h.event(t, m)
}

func (h *harness) event(t *testing.T, ev any) {
	t.Helper()
	raw, err := json.Marshal(ev)
	require.NoError(t, err)
	h.svc.HandleEvent(h.ctx, h.app, EventCallback{Type: "event_callback", TeamID: team, AppID: "AVIX", Event: raw})
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
	h.svc.OnReply(h.ctx, replyhook.Event{UserID: h.app.OwnerID, ChatID: h.target.Binding.ChatID, MessageID: replyID, TriggerMessageID: &userMsg})
	return replyID
}

func (h *harness) waitTurns(t *testing.T, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return h.turns.count() == n }, time.Second, 5*time.Millisecond)
}

func TestAMentionStartsATurnAndTheReplyThreadsBack(t *testing.T) {
	h := newHarness(t)
	ask := h.msg("UALICE", "<@UVIX> what's the plan for <#C9|launch>?")
	h.deliver(t, ask)
	h.waitTurns(t, 1)

	turn := h.turns.turns[0]
	assert.Equal(t, "alice (Slack, #general): what's the plan for #launch?", turn.Message.Message)
	assert.Equal(t, TurnSource, turn.Source)
	assert.Equal(t, h.app.OwnerID, turn.UserID)
	assert.Empty(t, turn.Timezone, "the owner's time zone is not exposed")
	require.Eventually(t, func() bool { return h.store.inboundAttached() == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "alice", h.store.links[0].AuthorName)
	assert.Equal(t, ask.TS, h.store.links[0].ThreadTS, "a top-level mention's reply starts a thread under it")
	require.Eventually(t, func() bool { return slices.Contains(h.slack.reacted(), "+eyes@"+ask.TS) }, time.Second, 5*time.Millisecond,
		"the working reaction goes on the message being answered")

	replyID := h.replyTo(t, 0, "**Ship it** on Friday, see [the board](https://x.test/b?a=1&b=2). <!channel>")
	sent := h.slack.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, post{Channel: "C1", ThreadTS: ask.TS, TS: sent[0].TS,
		Text: "*Ship it* on Friday, see <https://x.test/b?a=1&amp;b=2|the board>. &lt;!channel&gt;"}, sent[0])
	require.Eventually(t, func() bool { return slices.Contains(h.slack.reacted(), "-eyes@"+ask.TS) }, time.Second, 5*time.Millisecond,
		"and comes off when the turn ends")

	// A retried hook does not post twice.
	userMsg := h.turns.ids[0]
	h.svc.OnReply(h.ctx, replyhook.Event{UserID: h.app.OwnerID, ChatID: h.target.Binding.ChatID, MessageID: replyID, TriggerMessageID: &userMsg})
	assert.Len(t, h.slack.sent(), 1)
}

func TestWhatIsNeverAnswered(t *testing.T) {
	h := newHarness(t)
	old := h.msg("UALICE", "<@UVIX> ancient")
	old.TS = fmt.Sprintf("%d.000001", time.Now().Add(-2*time.Hour).Unix())
	edited := h.msg("UALICE", "<@UVIX> edited")
	edited.Subtype = "message_changed"
	ownByBotID := h.msg("", "<@UVIX> loop")
	ownByBotID.BotID = botID
	webhook := h.msg("", "<@UVIX> deploy finished")
	webhook.Subtype, webhook.BotID = "bot_message", "BHOOK"
	otherChannel := h.msg("UALICE", "<@UVIX> hi")
	otherChannel.Channel = "C2"
	fromBot := h.msg("UNOVA", "<@UVIX> beep")
	fromBot.BotID, fromBot.BotProfile = "BNOVA", &BotProfile{Name: "Nova"}
	inThread := h.msg("UALICE", "and then?")
	inThread.ThreadTS = "100.000001"

	for _, tc := range []struct {
		name string
		m    Message
	}{
		{"its own message", h.msg(botUser, "<@UVIX> talking to myself")},
		{"its own bot id", ownByBotID},
		{"not mentioned", h.msg("UALICE", "just chatting")},
		{"another channel", otherChannel},
		{"an integration with nobody behind it", webhook},
		{"an edit", edited},
		{"before the binding", old},
		{"another app's bot", fromBot},
		{"a thread reply without a mention, when the persona did not answer the author there", inThread},
	} {
		h.deliver(t, tc.m)
		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, 0, h.turns.count(), tc.name)
	}

	h.store.target.Binding.DenyUserIDs = []string{"UALICE"}
	h.deliver(t, h.msg("UALICE", "<@UVIX> denied"))
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h.turns.count(), "deny wins")
	h.store.target.Binding.DenyUserIDs = nil
	h.store.target.Binding.AllowUserIDs = []string{"UBOB"}
	h.deliver(t, h.msg("UALICE", "<@UVIX> not on the list"))
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h.turns.count(), "an allow list keeps others out")

	h.store.target.Binding.AllowUserIDs = nil
	h.store.target.Binding.AnswerAgents = true
	h.deliver(t, fromBot)
	h.waitTurns(t, 1)
	assert.Equal(t, "Nova (Slack, #general): beep", h.turns.prompt(0), "a bot is named by its app")
}

func TestARedeliveredMessageIsAnsweredOnce(t *testing.T) {
	h := newHarness(t)
	ask := h.msg("UALICE", "<@UVIX> hello")
	h.deliver(t, ask)
	h.waitTurns(t, 1)
	time.Sleep(250 * time.Millisecond) // let the first turn time out of the queue
	h.deliver(t, ask)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, h.turns.count())
}

func TestAThreadContinuationNeedsNoMentionAndQuotesTheThread(t *testing.T) {
	h := newHarness(t)
	h.slack.parents["100.000001"] = Message{User: "UBOB", Text: "should we rewrite it in <https://go.dev|Go>?", TS: "100.000001"}
	follow := h.msg("UALICE", "and then?")
	follow.ThreadTS = "100.000001"
	h.store.continues = true
	h.deliver(t, follow)
	h.waitTurns(t, 1)
	assert.Equal(t, "(in a thread started by Bob Builder: \"should we rewrite it in Go (https://go.dev)?\")\nalice (Slack, #general): and then?",
		h.turns.prompt(0))

	require.Eventually(t, func() bool { return h.store.inboundAttached() == 1 }, time.Second, 5*time.Millisecond)
	h.replyTo(t, 0, "Maybe.")
	assert.Equal(t, "100.000001", h.slack.sent()[0].ThreadTS, "answered in the existing thread")
}

func TestThePersonasOwnThreadIsNotQuoted(t *testing.T) {
	h := newHarness(t)
	h.slack.parents["100.000001"] = Message{User: botUser, BotID: botID, Text: "my earlier answer", TS: "100.000001"}
	ask := h.msg("UBOB", "<@UVIX> more?")
	ask.ThreadTS = "100.000001"
	h.deliver(t, ask)
	h.waitTurns(t, 1)
	assert.Equal(t, "Bob Builder (Slack, #general): more?", h.turns.prompt(0))
}

func TestAnUnsandboxedThreadIsClosedUntilAcknowledged(t *testing.T) {
	h := newHarness(t)
	h.store.chat = &models.Chat{ID: h.target.Binding.ChatID, ContextScope: models.ContextScopeAccount}
	ask := h.msg("UALICE", "<@UVIX> hi")
	h.deliver(t, ask)
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h.turns.count())
	assert.Equal(t, UnrestrictedWarning, h.store.bindError)

	h.store.target.Binding.AllowUnrestricted = true
	h.deliver(t, ask)
	h.waitTurns(t, 1)

	h2 := newHarness(t)
	h2.store.chat, h2.store.chatErr = nil, datastore.ErrChatNotFound
	h2.deliver(t, h2.msg("UALICE", "<@UVIX> hi"))
	assert.Equal(t, models.SlackBindingBroken, h2.store.bindStatus)
	assert.Equal(t, ThreadDeletedError, h2.store.bindError)
}

func TestAFailedTurnPostsANeutralNotice(t *testing.T) {
	h := newHarness(t)
	h.turns.err = errors.New("out of credits for account 1234")
	ask := h.msg("UALICE", "<@UVIX> hi")
	h.deliver(t, ask)
	require.Eventually(t, func() bool { return len(h.slack.sent()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, post{Channel: "C1", ThreadTS: ask.TS, Text: NoticeFailed, TS: h.slack.sent()[0].TS}, h.slack.sent()[0])
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	assert.Empty(t, h.store.links, "the inbound link is removed so a redelivery may try again")
}

func TestPostFailures(t *testing.T) {
	tests := []struct {
		name        string
		errs        []error
		wantPosted  bool
		wantBinding models.SlackBindingStatus
		wantApp     models.SlackAppStatus
	}{
		{name: "a rate limit is waited out", errs: []error{&Error{Code: "ratelimited", RetryAfter: time.Millisecond}}, wantPosted: true},
		{name: "a network error is retried", errs: []error{errors.New("connection reset")}, wantPosted: true},
		{name: "a channel the app left breaks the binding", errs: []error{&Error{Code: "not_in_channel"}}, wantBinding: models.SlackBindingBroken},
		{name: "a revoked token marks the app", errs: []error{&Error{Code: "token_revoked"}}, wantApp: models.SlackAppInvalidToken},
		{name: "other refusals are not retried", errs: []error{&Error{Code: "msg_too_long"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			replyID := uuid.New()
			h.store.messages[replyID] = &models.ChatMessage{ID: replyID, Message: "x"}
			h.slack.errs = tt.errs
			h.svc.post(h.ctx, h.target, replyID, "", nil)
			assert.Equal(t, tt.wantPosted, len(h.slack.sent()) == 1)
			assert.Equal(t, tt.wantBinding, h.store.bindStatus)
			assert.Equal(t, tt.wantApp, h.store.appStatus)
		})
	}
}

func TestAComposerRequestPostsTheNextReply(t *testing.T) {
	h := newHarness(t)
	replyID := uuid.New()
	long := strings.Repeat("para\n\n", 1000) // past one message
	h.store.messages[replyID] = &models.ChatMessage{ID: replyID, Message: long, Attachments: []*models.FileAttachment{
		{ID: uuid.New(), Name: "chart.png", S3Key: "k", FileType: "image/png"},
		{ID: uuid.New(), Name: "fetched.pdf", S3Key: "f", ContextOnly: true},
	}}
	h.store.pending = []uuid.UUID{h.target.Binding.ID}
	h.svc.Files = fileReader{"k": []byte("png"), "f": []byte("pdf")}
	h.svc.OnReply(h.ctx, replyhook.Event{UserID: h.app.OwnerID, ChatID: h.target.Binding.ChatID, MessageID: replyID})

	sent := h.slack.sent()
	require.Len(t, sent, 2)
	assert.Empty(t, sent[0].ThreadTS, "a new top-level message")
	assert.Equal(t, sent[0].TS, sent[1].ThreadTS, "the rest goes in its thread")
	for _, p := range sent {
		assert.LessOrEqual(t, len([]rune(p.Text)), MaxMessageRunes)
	}
	assert.Equal(t, []upload{{Channel: "C1", ThreadTS: sent[0].TS, Name: "chart.png"}}, h.slack.uploads,
		"files the reply produced go in the thread; fetched ones are not posted")
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	assert.Equal(t, sent[0].TS, h.store.links[0].ThreadTS, "the link records the thread it started")
}

func TestMentionsThatWaitAreAnsweredInOneBatch(t *testing.T) {
	h := newHarness(t)
	h.svc.turns.Gather = -1
	h.svc.TurnTimeout = time.Hour // each turn holds the thread until its reply

	h.deliver(t, h.msg("UALICE", "<@UVIX> first"))
	h.waitTurns(t, 1)
	h.deliver(t, h.msg("UBOB", "<@UVIX> second"))
	last := h.msg("UCAROL", "<@UVIX> third")
	h.deliver(t, last)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 1, h.turns.count(), "the burst waits")

	require.Eventually(t, func() bool { return h.store.inboundAttached() == 1 }, time.Second, 5*time.Millisecond)
	h.replyTo(t, 0, "one")
	h.waitTurns(t, 2)
	prompt := h.turns.prompt(1)
	assert.Contains(t, prompt, "(Relay note: 2 messages arrived")
	assert.Contains(t, prompt, "Bob Builder (Slack, #general): second\ncarol (Slack, #general): third")

	require.Eventually(t, func() bool { return h.store.inboundAttached() == 3 }, time.Second, 5*time.Millisecond)
	h.replyTo(t, 1, "both of you")
	sent := h.slack.sent()
	require.Len(t, sent, 2, "one reply for the batch")
	assert.Equal(t, last.TS, sent[1].ThreadTS, "in the newest mention's thread")
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
		{name: "not answered unless the binding allows it", prev: -1},
		{name: "answered when the binding allows it", answerAgents: true, prev: -1, wantTurn: true, wantDepth: 1},
		{name: "wrap-up hint near the cap", answerAgents: true, prev: 1, wantTurn: true, wantDepth: 2, wantHint: "Start wrapping up"},
		{name: "past the cap", answerAgents: true, prev: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.store.target.Binding.AnswerAgents = tt.answerAgents
			if tt.prev >= 0 {
				_, _, err := h.store.RecordInboundSlackMessage(h.ctx, models.SlackInbound{BindingID: h.target.Binding.ID, MessageTS: "before", AgentDepth: tt.prev})
				require.NoError(t, err)
			}
			nova := h.msg("UNOVA", "<@UVIX> hey Vix")
			nova.BotID = "BNOVA"
			h.deliver(t, nova)
			if !tt.wantTurn {
				time.Sleep(30 * time.Millisecond)
				assert.Equal(t, 0, h.turns.count())
				assert.Empty(t, h.slack.sent(), "nothing is posted to an agent")
				return
			}
			h.waitTurns(t, 1)
			last, err := h.store.LastInboundSlackLink(h.ctx, h.target.Binding.ID, time.Now().Add(-time.Hour))
			require.NoError(t, err)
			assert.Equal(t, tt.wantDepth, last.AgentDepth)
			if tt.wantHint == "" {
				assert.NotContains(t, h.turns.prompt(0), "Relay note")
			} else {
				assert.Contains(t, h.turns.prompt(0), tt.wantHint)
			}
		})
	}
}

func TestChannelAndAppEvents(t *testing.T) {
	type channelEvent struct {
		Type    string     `json:"type"`
		Channel ChannelRef `json:"channel"`
	}
	h := newHarness(t)
	h.event(t, channelEvent{Type: "channel_rename", Channel: ChannelRef{ID: "C1", Name: "lounge"}})
	assert.Equal(t, "lounge", h.store.target.Binding.ChannelName)

	h.event(t, map[string]any{"type": "tokens_revoked", "tokens": map[string]any{"oauth": []string{"U1"}}})
	assert.Empty(t, h.store.appStatus, "a revoked user token is not the app's")
	h.event(t, map[string]any{"type": "tokens_revoked", "tokens": map[string]any{"bot": []string{botUser}}})
	assert.Equal(t, models.SlackAppInvalidToken, h.store.appStatus)

	h.event(t, map[string]any{"type": "group_left", "channel": "C1"})
	assert.Equal(t, models.SlackBindingBroken, h.store.bindStatus)
	assert.Equal(t, RemovedFromChannel, h.store.bindError)

	h2 := newHarness(t)
	raw, _ := json.Marshal(h2.msg("UALICE", "<@UVIX> hi"))
	h2.svc.HandleEvent(h2.ctx, h2.app, EventCallback{Type: "event_callback", TeamID: "T-OTHER", AppID: "AVIX", Event: raw})
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, h2.turns.count(), "an event for another workspace is ignored")
}

func TestFilesPostedWithAMention(t *testing.T) {
	h := newHarness(t)
	ingest := &fakeIngest{}
	h.svc.Ingest = ingest
	h.slack.files["https://files.slack.com/files-pri/T1-F1/download/a.png"] = []byte("png")
	ask := h.msg("UALICE", "<@UVIX> look")
	ask.Subtype = "file_share"
	ask.Files = []File{
		{Name: "a.png", Size: 3, URLPrivateDownload: "https://files.slack.com/files-pri/T1-F1/download/a.png"},
		{Name: "gone.png", Mode: "tombstone"},
		{Name: "huge.mov", Size: MaxInboundFileBytes + 1, URLPrivateDownload: "https://files.slack.com/x"},
	}
	h.deliver(t, ask)
	h.waitTurns(t, 1)
	assert.Equal(t, []string{"a.png"}, ingest.names)
	assert.Len(t, h.turns.turns[0].Message.Attachments, 1)
	prompt := h.turns.prompt(0)
	assert.Contains(t, prompt, `[File "gone.png" was not added: it was deleted]`)
	assert.Contains(t, prompt, `[File "huge.mov" was not added: too large]`)
}
