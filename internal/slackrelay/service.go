package slackrelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/plugins"
	"github.com/theimaginaryfoundation/what-iff/internal/relaykit"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"go.uber.org/zap"
)

// Store is the storage the relay needs (the datastore satisfies it).
type Store interface {
	FindSlackBindingTarget(ctx context.Context, appID uuid.UUID, channelID string) (*models.SlackBindingTarget, error)
	GetSlackBindingTarget(ctx context.Context, bindingID uuid.UUID) (*models.SlackBindingTarget, error)
	RecordInboundSlackMessage(ctx context.Context, in models.SlackInbound) (*models.SlackMessageLink, bool, error)
	AttachSlackLinkMessage(ctx context.Context, linkID, chatMessageID uuid.UUID) error
	DeleteSlackLink(ctx context.Context, linkID uuid.UUID) error
	FindInboundSlackLink(ctx context.Context, chatMessageID uuid.UUID) (*models.SlackMessageLink, error)
	SlackThreadContinues(ctx context.Context, bindingID uuid.UUID, threadTS, author string) (bool, error)
	StartOutboundSlackLink(ctx context.Context, bindingID, chatMessageID uuid.UUID, threadTS string, replyTo *uuid.UUID) (*models.SlackMessageLink, bool, error)
	FinishOutboundSlackLink(ctx context.Context, linkID uuid.UUID, threadTS string, postedTS []string, postErr error) error
	ConsumeSlackPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error)
	SetSlackBindingStatus(ctx context.Context, id uuid.UUID, status models.SlackBindingStatus, lastError string) error
	WithdrawSlackBindingAcknowledgement(ctx context.Context, id uuid.UUID) error
	TouchSlackBinding(ctx context.Context, id uuid.UUID) error
	// LastInboundSlackLink is the binding's newest inbound message since a time, for the loop
	// guard.
	LastInboundSlackLink(ctx context.Context, bindingID uuid.UUID, since time.Time) (*models.SlackMessageLink, error)
	// RenameSlackChannel and BreakSlackChannel follow a channel's rename, and the app's removal
	// from it, on the app's bindings of it.
	RenameSlackChannel(ctx context.Context, appID uuid.UUID, channelID, name string) error
	BreakSlackChannel(ctx context.Context, appID uuid.UUID, channelID, reason string) error
	SetSlackAppState(ctx context.Context, id uuid.UUID, status models.SlackAppStatus, lastError string) error
	GetChatMessage(ctx context.Context, userID, id uuid.UUID) (*models.ChatMessage, error)
	GetJob(ctx context.Context, userID, id uuid.UUID) (*models.Job, error)
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
}

// Slack is how the service reaches Slack's Web API (*API satisfies it).
type Slack interface {
	PostMessage(ctx context.Context, botToken, channel, threadTS, text string) (string, error)
	AddReaction(ctx context.Context, botToken, channel, ts, name string) error
	RemoveReaction(ctx context.Context, botToken, channel, ts, name string) error
	UserInfo(ctx context.Context, botToken, userID string) (*User, error)
	ThreadParent(ctx context.Context, botToken, channel, threadTS string) (*Message, error)
	Download(ctx context.Context, botToken, rawURL string, limit int64) ([]byte, error)
	UploadFile(ctx context.Context, botToken, channel, threadTS, name string, data []byte) error
}

// Notices posted to Slack when a mention cannot be answered. They are neutral on purpose:
// nothing about the owner's account (credits, plan) is revealed.
const (
	NoticeFailed = "Sorry, I couldn't reply to that just now."
	NoticeBusy   = "I'm getting a lot of messages at once, so I skipped some. Mention me again if I missed yours."
)

// TurnSource is how the relay's turns are metered (metering.TurnSource).
const TurnSource = "slack"

// WorkingReaction is the emoji the persona's bot puts on the message it is answering while the
// turn runs (Slack has no typing indicator for apps).
const WorkingReaction = "eyes"

// Recorded on a binding or app (shown in the app, never posted to Slack).
const (
	UnrestrictedWarning = "Paused: this thread is no longer sandboxed, so Slack mentions are not answered. " +
		"Sandbox the thread again, or acknowledge that anyone allowed to mention the persona may use everything the thread can read."
	ThreadDeletedError     = "The relay thread was deleted. Connect this channel to another thread."
	RemovedFromChannel     = "The persona's app was removed from this channel. Invite it back in Slack (/invite), then reactivate."
	AppUninstalledError    = "The app was uninstalled from the workspace, or its token was revoked. Reinstall it and paste the new bot token."
	invalidBotTokenMessage = "Slack refused the app's bot token (%s). Paste a new one."
)

// Limits on the files a reply posts.
const (
	MaxReplyFiles = 4
	MaxReplyBytes = 50 << 20
)

// busyNoticeEvery bounds how often one binding posts NoticeBusy.
const busyNoticeEvery = time.Minute

// bindingSkew tolerates clock differences when dropping messages older than their binding.
const bindingSkew = 5 * time.Second

// Service is the relay: it turns mentions in bound channels into turns in their relay threads
// and posts replies back.
type Service struct {
	Store  Store
	Slack  Slack
	Turns  plugins.TurnStarter
	Logger *zap.Logger
	// Files reads the files saved with a reply, to upload them to Slack. Nil posts text only.
	Files relaykit.FileReader
	// Ingest saves the files posted with a mention to the relay thread. Nil leaves them out,
	// with a note to the persona.
	Ingest plugins.AttachmentIngester

	// PollEvery is how often a waiting turn checks its job for failure.
	PollEvery time.Duration
	// TurnTimeout bounds how long one inbound turn holds its thread's queue.
	TurnTimeout time.Duration
	// Policy paces turns: how many mentions may wait as the next batch, and the agent-to-agent
	// loop guard (relaykit.Policy; zero fields take the defaults).
	Policy relaykit.Policy
	// PostRetries is how many times a failed post is retried (transient errors only).
	PostRetries int
	// RetryBackoff is the wait before the first retry; it doubles each time. A rate limit
	// waits as long as Slack says instead.
	RetryBackoff time.Duration

	once    sync.Once
	turns   relaykit.TurnQueue[inboundWork]
	guard   *relaykit.LoopGuard
	replies relaykit.Replies
	busy    relaykit.Throttle
	names   nameCache
}

type inboundWork struct {
	target models.SlackBindingTarget
	msg    Message
	// agent: another app's bot wrote it.
	agent bool
}

func (s *Service) defaults() { s.once.Do(s.setDefaults) }

func (s *Service) setDefaults() {
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
	// A thread's mentions batch together, and so do the channel's top-level ones (thread "").
	s.turns.Place = func(w inboundWork) string { return w.target.Binding.ID.String() + "/" + w.msg.ThreadTS }
	s.turns.Run = s.runInbound
	s.guard = s.Policy.Guard()
	s.replies.TTL = s.TurnTimeout
	s.busy.Every = busyNoticeEvery
	s.names.ttl = 10 * time.Minute
}

// HandleEvent is called for every Events API payload an app's Socket Mode connection delivers.
// It returns at once; an accepted mention starts a turn, or, while one runs in the relay
// thread, waits with the other mentions of its thread to be answered together.
func (s *Service) HandleEvent(ctx context.Context, app models.SlackAppCredentials, cb EventCallback) {
	s.defaults()
	if cb.Type != "event_callback" || (cb.TeamID != "" && cb.TeamID != app.TeamID) || (cb.AppID != "" && cb.AppID != app.AppID) {
		return
	}
	log := s.Logger.With(zap.String("slack_app_id", app.ID.String()))
	switch cb.EventType() {
	case "message":
		var m Message
		if json.Unmarshal(cb.Event, &m) == nil {
			s.handleMessage(ctx, app, m)
		}
	case "channel_rename", "group_rename":
		var ev struct {
			Channel ChannelRef `json:"channel"`
		}
		if json.Unmarshal(cb.Event, &ev) == nil && ev.Channel.ID != "" {
			if err := s.Store.RenameSlackChannel(ctx, app.ID, ev.Channel.ID, ev.Channel.Name); err != nil {
				log.Warn("slack relay: record channel rename", zap.Error(err))
			}
		}
	case "channel_left", "group_left":
		var ev struct {
			Channel string `json:"channel"`
		}
		if json.Unmarshal(cb.Event, &ev) == nil && ev.Channel != "" {
			if err := s.Store.BreakSlackChannel(ctx, app.ID, ev.Channel, RemovedFromChannel); err != nil {
				log.Warn("slack relay: record removal from channel", zap.Error(err))
			}
		}
	case "tokens_revoked":
		var ev struct {
			Tokens struct {
				Bot []string `json:"bot"`
			} `json:"tokens"`
		}
		if json.Unmarshal(cb.Event, &ev) != nil || len(ev.Tokens.Bot) == 0 {
			return // only user tokens, which the relay does not hold
		}
		fallthrough
	case "app_uninstalled":
		if err := s.Store.SetSlackAppState(ctx, app.ID, models.SlackAppInvalidToken, AppUninstalledError); err != nil {
			log.Warn("slack relay: record uninstall", zap.Error(err))
		}
	}
}

// handleMessage applies the trigger rules to a message and queues an accepted mention.
func (s *Service) handleMessage(ctx context.Context, app models.SlackAppCredentials, m Message) {
	if m.Channel == "" {
		return
	}
	target, err := s.Store.FindSlackBindingTarget(ctx, app.ID, m.Channel)
	if err != nil || target == nil {
		return // not a bound channel
	}
	agent, ok := s.admits(ctx, *target, m)
	if !ok || !s.threadOpen(ctx, *target) {
		return
	}
	if s.turns.Enqueue(ctx, target.Binding.ChatID, inboundWork{target: *target, msg: m, agent: agent}) {
		return
	}
	s.Logger.Warn("slack relay: queue full; dropping mention",
		zap.String("binding_id", target.Binding.ID.String()), zap.String("ts", m.TS))
	// Never to an agent: a notice it answers would feed the flood.
	if !agent && s.busy.Due(target.Binding.ID) {
		go s.notice(context.WithoutCancel(ctx), *target, m, NoticeBusy)
	}
}

// admits applies the trigger rules to a message on a binding: something someone wrote in the
// bound channel, newer than the binding, not the persona's own nor an integration's, from
// someone the lists allow, that mentions the persona or continues a thread with it, and not
// from another app's bot unless the binding answers agents. It reports whether the author is
// a bot. The bound thread is checked apart (threadOpen).
func (s *Service) admits(ctx context.Context, t models.SlackBindingTarget, m Message) (agent, ok bool) {
	b, app := t.Binding, t.App
	if !m.IsPlain() || m.Channel != b.ChannelID {
		return false, false
	}
	if m.User == app.BotUserID || (app.BotID != "" && m.BotID == app.BotID) {
		return false, false // the persona's own
	}
	if m.User == "" {
		return false, false // an incoming webhook or another integration: nobody to answer
	}
	if !b.InboundEnabled || !t.Live() {
		return false, false
	}
	if m.Time().Before(b.CreatedAt.Add(-bindingSkew)) {
		return false, false // said before the channel was bound
	}
	if !relaykit.Allowed(m.User, b.AllowUserIDs, b.DenyUserIDs) {
		return false, false
	}
	if !m.MentionsUser(app.BotUserID) {
		if !m.InThread() {
			return false, false
		}
		ok, err := s.Store.SlackThreadContinues(ctx, b.ID, m.ThreadTS, m.User)
		if err != nil || !ok {
			return false, false
		}
	}
	if m.FromBot() && !b.AnswerAgents {
		return true, false
	}
	return m.FromBot(), true
}

// threadOpen is the fail-closed check on the bound thread (relaykit.ThreadOpen).
func (s *Service) threadOpen(ctx context.Context, t models.SlackBindingTarget) bool {
	b := t.Binding
	chat, err := s.Store.GetChat(ctx, t.App.OwnerID, b.ChatID)
	open := relaykit.ThreadOpen(relaykit.Gate{
		Chat: chat, ChatErr: err,
		Acknowledged: b.AllowUnrestricted, Broken: b.Status == models.SlackBindingBroken, LastError: b.LastError,
		Warning: UnrestrictedWarning, DeletedError: ThreadDeletedError,
		MarkBroken: func(reason string) {
			_ = s.Store.SetSlackBindingStatus(ctx, b.ID, models.SlackBindingBroken, reason)
		},
		SetLastError: func(msg string) { _ = s.Store.SetSlackBindingStatus(ctx, b.ID, b.Status, msg) },
		Withdraw: func() {
			if err := s.Store.WithdrawSlackBindingAcknowledgement(ctx, b.ID); err != nil {
				s.Logger.Warn("slack relay: could not withdraw a stale acknowledgement",
					zap.String("binding_id", b.ID.String()), zap.Error(err))
			}
		},
	})
	if !open {
		s.Logger.Warn("slack relay: bound thread is missing, or not sandboxed and not acknowledged; dropping mention",
			zap.String("binding_id", b.ID.String()), zap.String("chat_id", b.ChatID.String()), zap.Error(err))
	}
	return open
}

// OnReply is the reply hook: it posts a finished reply when the turn came from Slack (into the
// thread it came from) or when a post was requested for the chat (the composer toggle or the
// post_to_slack tool).
func (s *Service) OnReply(ctx context.Context, ev replyhook.Event) {
	s.defaults()
	postedTo := map[uuid.UUID]bool{}

	if ev.TriggerMessageID != nil {
		inbound, err := s.Store.FindInboundSlackLink(ctx, *ev.TriggerMessageID)
		if err != nil {
			s.Logger.Warn("slack relay: find inbound link", zap.Error(err))
		}
		if inbound != nil {
			s.postInboundReply(ctx, inbound, ev, postedTo)
		} else if err == nil && s.turns.Active(ev.ChatID) {
			s.replies.KeepOrphan(ev)
			// runInbound attaches the link right after the turn starts, then takes any orphan.
			// If it attached between the lookup above and KeepOrphan, look once more.
			if again, err := s.Store.FindInboundSlackLink(ctx, *ev.TriggerMessageID); err == nil && again != nil {
				if _, mine := s.replies.TakeOrphan(*ev.TriggerMessageID); mine {
					s.postInboundReply(ctx, again, ev, postedTo)
				}
			}
		}
	}

	bindings, err := s.Store.ConsumeSlackPendingPosts(ctx, ev.UserID, ev.ChatID)
	if err != nil {
		s.Logger.Warn("slack relay: pending posts", zap.Error(err))
		return
	}
	for _, id := range bindings {
		if postedTo[id] {
			continue
		}
		target, err := s.Store.GetSlackBindingTarget(ctx, id)
		if err != nil || target.App.OwnerID != ev.UserID {
			continue
		}
		s.post(ctx, *target, ev.MessageID, "", nil)
	}
}

// postInboundReply posts a finished reply into the thread its inbound link came from.
func (s *Service) postInboundReply(ctx context.Context, inbound *models.SlackMessageLink, ev replyhook.Event, postedTo map[uuid.UUID]bool) {
	if target, err := s.Store.GetSlackBindingTarget(ctx, inbound.BindingID); err == nil {
		linkID := inbound.ID
		s.post(ctx, *target, ev.MessageID, inbound.ThreadTS, &linkID)
		postedTo[target.Binding.ID] = true
	}
	s.replies.Release(*ev.TriggerMessageID)
}

// post sends one saved reply to a binding's channel, at most once (the outbound link is the
// record): into threadTS, or, when it is "", as a new message with any further parts and files
// in its thread. Transient failures are retried; Slack refusing the token or the channel is
// recorded on the app or binding.
func (s *Service) post(ctx context.Context, t models.SlackBindingTarget, messageID uuid.UUID, threadTS string, replyToLink *uuid.UUID) {
	s.defaults()
	b, app := t.Binding, t.App
	log := s.Logger.With(zap.String("binding_id", b.ID.String()), zap.String("message_id", messageID.String()))
	if !t.Live() {
		// Paused or broken since the post was asked for: a pause also stops replies already
		// armed or in flight.
		log.Info("slack relay: binding not live; not posting")
		return
	}
	link, created, err := s.Store.StartOutboundSlackLink(ctx, b.ID, messageID, threadTS, replyToLink)
	if err != nil {
		log.Error("slack relay: start outbound", zap.Error(err))
		return
	}
	if !created && link.Status == models.SlackLinkSent {
		return
	}
	reply, err := s.Store.GetChatMessage(ctx, app.OwnerID, messageID)
	if err != nil {
		log.Error("slack relay: read reply", zap.Error(err))
		_ = s.Store.FinishOutboundSlackLink(ctx, link.ID, "", nil, err)
		return
	}

	files, skipped := relaykit.LoadReplyFiles(ctx, s.Files, reply, MaxReplyFiles, MaxReplyBytes, log)
	text := ToMrkdwn(reply.Message)
	if len(skipped) > 0 {
		text = strings.TrimRight(text, "\n") + "\n\n_" + escapeMrkdwn(skippedNote(skipped)) + "_"
	}
	parts := relaykit.SplitText(text, MaxMessageRunes)
	if len(parts) == 0 && len(files) == 0 {
		_ = s.Store.FinishOutboundSlackLink(ctx, link.ID, "", nil, nil)
		return
	}

	thread := threadTS
	var posted []string
	for _, part := range parts {
		var ts string
		ts, err = s.withRetry(ctx, func() (string, error) {
			return s.Slack.PostMessage(ctx, app.BotToken, b.ChannelID, thread, part)
		})
		if err != nil {
			break
		}
		posted = append(posted, ts)
		if thread == "" {
			thread = ts // the rest goes in the first part's thread
		}
	}
	if err == nil {
		for _, f := range files {
			_, ferr := s.withRetry(ctx, func() (string, error) {
				return "", s.Slack.UploadFile(ctx, app.BotToken, b.ChannelID, thread, f.Name, f.Data)
			})
			if ferr != nil {
				log.Warn("slack relay: upload reply file", zap.String("file", f.Name), zap.Error(ferr))
			}
		}
	}
	if ferr := s.Store.FinishOutboundSlackLink(ctx, link.ID, thread, posted, err); ferr != nil {
		log.Error("slack relay: finish outbound", zap.Error(ferr))
	}
	s.recordPostError(ctx, t, err)
	if err == nil {
		_ = s.Store.TouchSlackBinding(ctx, b.ID)
	} else {
		log.Warn("slack relay: post failed", zap.Error(err))
	}
}

// withRetry runs a call, retrying transient failures: after Slack's Retry-After for a rate
// limit, with doubling backoff otherwise.
func (s *Service) withRetry(ctx context.Context, call func() (string, error)) (string, error) {
	backoff := s.RetryBackoff
	for attempt := 0; ; attempt++ {
		out, err := call()
		if err == nil || attempt >= s.PostRetries || !IsTransient(err) {
			return out, err
		}
		wait := backoff
		var slackErr *Error
		if errors.As(err, &slackErr) && slackErr.RetryAfter > 0 {
			wait = slackErr.RetryAfter
		} else {
			backoff *= 2
		}
		if !relaykit.SleepCtx(ctx, wait) {
			return out, err
		}
	}
}

// recordPostError marks the app or binding when Slack refused the post for good, so the app
// can say why replies stop arriving. Transient failures leave both alone.
func (s *Service) recordPostError(ctx context.Context, t models.SlackBindingTarget, err error) {
	switch {
	case err == nil:
	case IsAuthError(err):
		_ = s.Store.SetSlackAppState(ctx, t.App.ID, models.SlackAppInvalidToken, invalidBotToken(err))
	case IsChannelError(err):
		_ = s.Store.SetSlackBindingStatus(ctx, t.Binding.ID, models.SlackBindingBroken,
			"Slack refused the persona's message in this channel ("+codeOf(err)+"). Check the app is in the channel, then reactivate.")
	}
}

func invalidBotToken(err error) string { return fmt.Sprintf(invalidBotTokenMessage, codeOf(err)) }

// notice posts a short reply in the thread of the message that asked; failures are ignored.
func (s *Service) notice(ctx context.Context, t models.SlackBindingTarget, m Message, text string) {
	if _, err := s.Slack.PostMessage(ctx, t.App.BotToken, t.Binding.ChannelID, m.ThreadRoot(), escapeMrkdwn(text)); err != nil {
		s.Logger.Debug("slack relay: notice failed", zap.Error(err))
	}
}

// skippedNote names the reply's files that were not posted.
func skippedNote(names []string) string { return relaykit.SkippedNote("Slack", names) }
