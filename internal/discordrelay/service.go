package discordrelay

import (
	"context"
	"errors"
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
	FindDiscordBindingTarget(ctx context.Context, botID uuid.UUID, channelID string) (*models.DiscordBindingTarget, error)
	GetDiscordBindingTarget(ctx context.Context, bindingID uuid.UUID) (*models.DiscordBindingTarget, error)
	RecordInboundDiscordMessage(ctx context.Context, in models.DiscordInbound) (*models.DiscordMessageLink, bool, error)
	AttachDiscordLinkMessage(ctx context.Context, linkID, chatMessageID uuid.UUID) error
	DeleteDiscordLink(ctx context.Context, linkID uuid.UUID) error
	FindInboundDiscordLink(ctx context.Context, chatMessageID uuid.UUID) (*models.DiscordMessageLink, error)
	StartOutboundDiscordLink(ctx context.Context, bindingID, chatMessageID uuid.UUID, channelID string, replyTo *uuid.UUID) (*models.DiscordMessageLink, bool, error)
	FinishOutboundDiscordLink(ctx context.Context, linkID uuid.UUID, postedIDs []string, postErr error) error
	ConsumeDiscordPendingPosts(ctx context.Context, userID, chatID uuid.UUID) ([]uuid.UUID, error)
	SetDiscordBindingStatus(ctx context.Context, id uuid.UUID, status models.DiscordBindingStatus, lastError string) error
	// WithdrawDiscordBindingAcknowledgement clears the "not sandboxed" acknowledgement on a binding.
	WithdrawDiscordBindingAcknowledgement(ctx context.Context, id uuid.UUID) error
	SetDiscordBotStatus(ctx context.Context, id uuid.UUID, status models.DiscordBotStatus, lastError string) error
	TouchDiscordBinding(ctx context.Context, id uuid.UUID) error
	GetChatMessage(ctx context.Context, userID, id uuid.UUID) (*models.ChatMessage, error)
	GetJob(ctx context.Context, userID, id uuid.UUID) (*models.Job, error)
	// GetChat reads the bound thread, for the sandbox check on every tag.
	GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
}

// Notices posted to Discord when a tag cannot be answered. They are neutral on
// purpose: nothing about the owner's account (credits, plan) is revealed.
const (
	NoticeFailed = "Sorry, I couldn't reply to that just now."
	NoticeBusy   = "I'm getting a lot of messages at once, so I skipped some. Tag me again if I missed yours."
)

// UnrestrictedWarning is recorded on a binding (its last_error, shown in the app,
// never posted to Discord) when tags are being dropped because the bound thread
// is no longer sandboxed and the binding carries no acknowledgement.
// ThreadDeletedError is recorded on a binding (status broken) when its relay thread no longer
// exists, so the app shows why tags go unanswered. Repointing the binding clears it.
const ThreadDeletedError = "The relay thread was deleted. Connect this channel to another thread."

// TurnSource is how the relay's turns are metered (metering.TurnSource): a turn a stranger
// starts from Discord is not the owner's own chat traffic.
const TurnSource = "discord"

const UnrestrictedWarning = "Paused: this thread is no longer sandboxed, so Discord tags are not answered. " +
	"Sandbox the thread again, or acknowledge that anyone allowed to tag the bot may use everything the thread can read."

// busyNoticeEvery bounds how often one binding posts NoticeBusy, so a flood of
// tags cannot make the bot flood the channel back.
const busyNoticeEvery = time.Minute

// RelayThreadOpenWithoutAcknowledgement reports whether a thread may be driven from
// Discord without the owner's acknowledgement: only a sandboxed one
// (relaykit.RelayThreadOpenWithoutAcknowledgement).
func RelayThreadOpenWithoutAcknowledgement(chat *models.Chat) bool {
	return relaykit.RelayThreadOpenWithoutAcknowledgement(chat)
}

// Service is the relay: it turns tags into turns in the relay thread and posts
// replies back out.
type Service struct {
	Store   Store
	Discord Discord
	Turns   plugins.TurnStarter
	Logger  *zap.Logger
	// Files reads the files saved with a reply, to upload them to Discord. Nil
	// posts text only.
	Files FileReader
	// Ingest saves the files posted with a tag to the relay thread, and Fetch
	// downloads them from Discord. Either nil leaves files out, with a note to the
	// persona.
	Ingest plugins.AttachmentIngester
	Fetch  Fetcher

	// TypingEvery refreshes the typing indicator (Discord shows it for ~10 s).
	TypingEvery time.Duration
	// PollEvery is how often a waiting turn checks its job for failure.
	PollEvery time.Duration
	// TurnTimeout bounds how long one inbound turn holds its thread's queue.
	TurnTimeout time.Duration
	// QueueDepth bounds the tags waiting behind a running turn, per thread.
	QueueDepth int
	// PostRetries is how many times a failed post is retried (transient errors only).
	PostRetries int
	// RetryBackoff is the wait before the first retry; it doubles each time.
	RetryBackoff time.Duration

	once sync.Once
	// turns runs one inbound turn at a time per relay thread.
	turns relaykit.TurnQueue[inboundWork]
	// replies matches finished replies with the inbound turns waiting for them.
	replies relaykit.Replies
	// busy throttles NoticeBusy per binding.
	busy relaykit.Throttle
}

type inboundWork struct {
	target models.DiscordBindingTarget
	msg    InboundMessage
}

func (s *Service) defaults() {
	s.once.Do(s.setDefaults)
}

// setDefaults fills unset tuning fields; it runs once (defaults is called from
// concurrent goroutines).
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
	if s.QueueDepth <= 0 {
		s.QueueDepth = 5
	}
	if s.PostRetries < 0 {
		s.PostRetries = 0
	} else if s.PostRetries == 0 {
		s.PostRetries = 2
	}
	if s.RetryBackoff <= 0 {
		s.RetryBackoff = 2 * time.Second
	}
	s.turns.Depth = s.QueueDepth
	s.turns.Run = s.runInbound
	s.replies.TTL = s.TurnTimeout
	s.busy.Every = busyNoticeEvery
}

// HandleInbound is called for every message a connected bot sees. It returns at
// once; accepted tags are queued behind any turn already running in the relay
// thread, one turn at a time.
func (s *Service) HandleInbound(ctx context.Context, botID uuid.UUID, botUserID string, msg InboundMessage) {
	s.defaults()
	if _, ok := Triggered(msg, botUserID); !ok {
		return
	}
	target, err := s.Store.FindDiscordBindingTarget(ctx, botID, msg.BindingChannelID())
	if err != nil {
		// Not a bound channel (the common case for a tag elsewhere) or a lookup error.
		return
	}
	b := target.Binding
	if !b.InboundEnabled || b.Status != models.DiscordBindingActive {
		return
	}
	if !Allowed(msg.AuthorID, b.AllowUserIDs, b.DenyUserIDs) {
		return
	}
	if !s.threadOpen(ctx, *target) {
		return
	}

	if s.enqueue(ctx, b.ChatID, inboundWork{target: *target, msg: msg}) {
		return
	}
	s.Logger.Warn("discord relay: queue full; dropping tag",
		zap.String("binding_id", b.ID.String()), zap.String("discord_message_id", msg.ID))
	if s.busyNoticeDue(b.ID) {
		go s.notice(ctx, target.Bot.Token, msg.ChannelID, msg.ID, NoticeBusy)
	}
}

// busyNoticeDue reports whether the binding may post NoticeBusy now, and records it.
func (s *Service) busyNoticeDue(bindingID uuid.UUID) bool {
	return s.busy.Due(bindingID)
}

// threadOpen is the relay's fail-closed check that the bound thread may be driven from
// Discord (relaykit.ThreadOpen): sandboxed, or acknowledged by the owner, read fresh on every
// tag. When it closes, the reason is recorded on the binding so the app can show it; nothing
// is posted to Discord.
func (s *Service) threadOpen(ctx context.Context, t models.DiscordBindingTarget) bool {
	b := t.Binding
	chat, err := s.Store.GetChat(ctx, t.Bot.OwnerID, b.ChatID)
	open := relaykit.ThreadOpen(relaykit.Gate{
		Chat: chat, ChatErr: err,
		Acknowledged: b.AllowUnrestricted, Broken: b.Status == models.DiscordBindingBroken, LastError: b.LastError,
		Warning: UnrestrictedWarning, DeletedError: ThreadDeletedError,
		MarkBroken: func(reason string) {
			_ = s.Store.SetDiscordBindingStatus(ctx, b.ID, models.DiscordBindingBroken, reason)
		},
		SetLastError: func(msg string) { _ = s.Store.SetDiscordBindingStatus(ctx, b.ID, b.Status, msg) },
		Withdraw: func() {
			if err := s.Store.WithdrawDiscordBindingAcknowledgement(ctx, b.ID); err != nil {
				s.Logger.Warn("discord relay: could not withdraw a stale acknowledgement",
					zap.String("binding_id", b.ID.String()), zap.Error(err))
			}
		},
	})
	if !open {
		s.Logger.Warn("discord relay: bound thread is missing, or not sandboxed and not acknowledged; dropping tag",
			zap.String("binding_id", b.ID.String()), zap.String("chat_id", b.ChatID.String()), zap.Error(err))
	}
	return open
}

// enqueue adds w to the thread's queue (relaykit.TurnQueue) and reports false when it is full.
func (s *Service) enqueue(ctx context.Context, chatID uuid.UUID, w inboundWork) bool {
	return s.turns.Enqueue(ctx, chatID, w)
}

// runInbound turns one tag into a turn and waits for its reply to be posted (or
// the turn to fail), keeping the typing indicator up meanwhile.
func (s *Service) runInbound(ctx context.Context, w inboundWork) {
	msg := w.msg
	log := s.Logger.With(zap.String("binding_id", w.target.Binding.ID.String()), zap.String("discord_message_id", msg.ID))

	// The tag may have waited behind a running turn for minutes; re-check against the
	// binding and thread as they are now, and act on that state from here on. A binding
	// repointed to another thread meanwhile drops the tag: it was accepted for this one.
	fresh, err := s.Store.GetDiscordBindingTarget(ctx, w.target.Binding.ID)
	if err != nil || fresh == nil || fresh.Binding.ChatID != w.target.Binding.ChatID ||
		!fresh.Binding.InboundEnabled || fresh.Binding.Status != models.DiscordBindingActive ||
		!Allowed(msg.AuthorID, fresh.Binding.AllowUserIDs, fresh.Binding.DenyUserIDs) ||
		!s.threadOpen(ctx, *fresh) {
		log.Info("discord relay: binding or thread no longer open; dropping queued tag")
		return
	}
	b, bot := fresh.Binding, fresh.Bot

	link, created, err := s.Store.RecordInboundDiscordMessage(ctx, models.DiscordInbound{
		BindingID:        b.ID,
		DiscordMessageID: msg.ID,
		DiscordChannelID: msg.ChannelID,
		AuthorID:         msg.AuthorID,
		AuthorName:       msg.AuthorName,
	})
	if err != nil {
		log.Error("discord relay: record inbound", zap.Error(err))
		return
	}
	if !created {
		return // already answered (a redelivered event)
	}

	attachments, notes := s.ingestInbound(ctx, bot.OwnerID, b.ChatID, msg)
	prompt := PromptText(msg, bot.BotUserID, b.ChannelName)
	if len(notes) > 0 {
		prompt += "\n" + strings.Join(notes, "\n")
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
		_ = s.Store.DeleteDiscordLink(ctx, link.ID)
		s.notice(ctx, bot.Token, msg.ChannelID, msg.ID, NoticeFailed)
		return
	}
	s.setWaiter(resp.ID, done)
	defer s.clearWaiter(resp.ID)
	if err := s.Store.AttachDiscordLinkMessage(ctx, link.ID, resp.ID); err != nil {
		log.Error("discord relay: attach inbound message", zap.Error(err))
	}
	_ = s.Store.TouchDiscordBinding(ctx, b.ID)
	// A reply that finished before the link was attached found no link; post it now.
	if ev, ok := s.takeOrphan(resp.ID); ok {
		s.OnReply(ctx, ev)
	}

	jobID, _ := uuid.Parse(resp.JobID)
	s.waitForReply(ctx, bot, msg, jobID, done)
}

func (s *Service) waitForReply(ctx context.Context, bot models.DiscordBotCredentials, msg InboundMessage, jobID uuid.UUID, done <-chan struct{}) {
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
				s.notice(ctx, bot.Token, msg.ChannelID, msg.ID, NoticeFailed)
				return
			}
			if job.Status == models.JobStatusComplete {
				// The reply hook posts it; the thread's next tag need not wait for that.
				return
			}
		}
	}
}

// setWaiter registers ch to be closed when the reply to user message id is posted
// (relaykit.Replies). If that already happened, ch is closed at once.
func (s *Service) setWaiter(id uuid.UUID, ch chan struct{}) { s.replies.Wait(id, ch) }

func (s *Service) clearWaiter(id uuid.UUID) { s.replies.Forget(id) }

func (s *Service) release(id uuid.UUID) { s.replies.Release(id) }

// OnReply is the reply hook: it posts a finished reply to Discord when the turn
// came from Discord (back to where it came from) or when a post was requested
// for the chat (by the composer toggle or the post_to_discord tool).
func (s *Service) OnReply(ctx context.Context, ev replyhook.Event) {
	s.defaults()
	postedTo := map[uuid.UUID]bool{}

	if ev.TriggerMessageID != nil {
		inbound, err := s.Store.FindInboundDiscordLink(ctx, *ev.TriggerMessageID)
		if err != nil {
			s.Logger.Warn("discord relay: find inbound link", zap.Error(err))
		}
		if inbound != nil {
			s.postInboundReply(ctx, inbound, ev, postedTo)
		} else if err == nil {
			s.keepOrphan(ev)
			// runInbound attaches the link right after the turn starts, then takes any orphan.
			// If it attached between the lookup above and keepOrphan, it found no orphan and
			// nobody would post: look once more, and post it ourselves if the link is there now.
			if again, err := s.Store.FindInboundDiscordLink(ctx, *ev.TriggerMessageID); err == nil && again != nil {
				if _, mine := s.takeOrphan(*ev.TriggerMessageID); mine {
					s.postInboundReply(ctx, again, ev, postedTo)
				}
			}
		}
	}

	bindings, err := s.Store.ConsumeDiscordPendingPosts(ctx, ev.UserID, ev.ChatID)
	if err != nil {
		s.Logger.Warn("discord relay: pending posts", zap.Error(err))
		return
	}
	for _, id := range bindings {
		if postedTo[id] {
			continue
		}
		target, err := s.Store.GetDiscordBindingTarget(ctx, id)
		if err != nil || target.Bot.OwnerID != ev.UserID {
			continue
		}
		s.post(ctx, *target, ev.MessageID, target.Binding.ChannelID, "", nil)
	}
}

// post sends one saved reply to a channel, at most once (the outbound link is the
// record), retrying transient failures and marking the bot or binding when
// Discord says the token or channel is no longer usable.
func (s *Service) post(ctx context.Context, target models.DiscordBindingTarget, messageID uuid.UUID, channelID, replyTo string, replyToLink *uuid.UUID) {
	s.defaults()
	b, bot := target.Binding, target.Bot
	log := s.Logger.With(zap.String("binding_id", b.ID.String()), zap.String("message_id", messageID.String()))

	link, created, err := s.Store.StartOutboundDiscordLink(ctx, b.ID, messageID, channelID, replyToLink)
	if err != nil {
		log.Error("discord relay: start outbound", zap.Error(err))
		return
	}
	if !created && link.Status == models.DiscordLinkSent {
		return
	}
	reply, err := s.Store.GetChatMessage(ctx, bot.OwnerID, messageID)
	if err != nil {
		log.Error("discord relay: read reply", zap.Error(err))
		_ = s.Store.FinishOutboundDiscordLink(ctx, link.ID, nil, err)
		return
	}
	parts := SplitForDiscord(reply.Message)
	files, skipped := s.loadReplyFiles(ctx, reply)
	if len(skipped) > 0 {
		parts = withNote(parts, skippedNote(skipped))
	}
	if len(parts) == 0 && len(files) == 0 {
		_ = s.Store.FinishOutboundDiscordLink(ctx, link.ID, nil, nil)
		return
	}
	if len(parts) == 0 {
		// Files alone: Discord accepts an empty body when the message carries an upload.
		parts = []string{""}
	}

	var ids []string
	backoff := s.RetryBackoff
	for attempt := 0; ; attempt++ {
		var posted []string
		posted, err = s.Discord.Post(ctx, bot.Token, channelID, replyTo, parts[len(ids):], toFiles(files))
		ids = append(ids, posted...)
		if errors.Is(err, ErrNoAccess) && len(files) > 0 && len(ids) < len(parts) {
			// Missing the Attach Files permission is common and does not stop
			// the text: send it alone, with a note, before judging the channel.
			files = nil
			parts = withNote(parts, noPermissionNote)
			continue
		}
		if err == nil || attempt >= s.PostRetries || errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrNoAccess) {
			break
		}
		if !sleepCtx(ctx, backoff) {
			break
		}
		backoff *= 2
		if len(ids) > 0 {
			replyTo = "" // the first part already went out as the reply
		}
	}
	if ferr := s.Store.FinishOutboundDiscordLink(ctx, link.ID, ids, err); ferr != nil {
		log.Error("discord relay: finish outbound", zap.Error(ferr))
	}
	switch {
	case errors.Is(err, ErrInvalidToken):
		_ = s.Store.SetDiscordBotStatus(ctx, bot.ID, models.DiscordBotInvalidToken, "Discord rejected the bot token. Paste a new one.")
	case errors.Is(err, ErrNoAccess) && channelID == b.ChannelID:
		_ = s.Store.SetDiscordBindingStatus(ctx, b.ID, models.DiscordBindingBroken, "The bot can no longer post in this channel.")
	case errors.Is(err, ErrNoAccess):
		// A Discord thread inside the bound channel (locked, archived or deleted): the binding
		// itself still works, so only this reply is lost.
		log.Warn("discord relay: cannot post in the Discord thread; the channel binding is left as is",
			zap.String("discord_channel_id", channelID), zap.Error(err))
	case err != nil:
		log.Warn("discord relay: post failed", zap.Error(err))
	default:
		_ = s.Store.TouchDiscordBinding(ctx, b.ID)
	}
}

// postInboundReply posts a finished reply back to the channel (or Discord thread) its inbound
// link came from, and records the binding it went to.
func (s *Service) postInboundReply(ctx context.Context, inbound *models.DiscordMessageLink, ev replyhook.Event, postedTo map[uuid.UUID]bool) {
	if target, err := s.Store.GetDiscordBindingTarget(ctx, inbound.BindingID); err == nil {
		replyTo := ""
		if inbound.DiscordMessageID != nil {
			replyTo = *inbound.DiscordMessageID
		}
		channel := inbound.DiscordChannelID
		if channel == "" {
			channel = target.Binding.ChannelID
		}
		linkID := inbound.ID
		s.post(ctx, *target, ev.MessageID, channel, replyTo, &linkID)
		postedTo[target.Binding.ID] = true
	}
	s.release(*ev.TriggerMessageID)
}

// keepOrphan remembers a reply whose trigger has no inbound link yet, when a relay
// turn is running in its thread (so ordinary chats' replies are never kept).
func (s *Service) keepOrphan(ev replyhook.Event) {
	if s.turns.Active(ev.ChatID) {
		s.replies.KeepOrphan(ev)
	}
}

// takeOrphan returns and forgets the reply kept for a user message, if any.
func (s *Service) takeOrphan(messageID uuid.UUID) (replyhook.Event, bool) {
	return s.replies.TakeOrphan(messageID)
}

// notice posts a short reply to the triggering message; failures are ignored.
func (s *Service) notice(ctx context.Context, token, channelID, replyTo, text string) {
	if _, err := s.Discord.Post(ctx, token, channelID, replyTo, []string{text}, nil); err != nil {
		s.Logger.Debug("discord relay: notice failed", zap.Error(err))
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool { return relaykit.SleepCtx(ctx, d) }
