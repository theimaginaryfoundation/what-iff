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
const UnrestrictedWarning = "Paused: this thread is no longer sandboxed, so Discord tags are not answered. " +
	"Sandbox the thread again, or acknowledge that anyone allowed to tag the bot may use everything the thread can read."

// busyNoticeEvery bounds how often one binding posts NoticeBusy, so a flood of
// tags cannot make the bot flood the channel back.
const busyNoticeEvery = time.Minute

// RelayThreadOpenWithoutAcknowledgement reports whether a thread may be driven from
// Discord without the owner's acknowledgement: only a sandboxed one. Anything else
// can read the owner's account (their name, memories, other conversations, files,
// scratchpad) on behalf of whoever may tag the bot.
func RelayThreadOpenWithoutAcknowledgement(chat *models.Chat) bool {
	return chat.IsSandboxed()
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

	once    sync.Once
	mu      sync.Mutex
	queues  map[uuid.UUID]chan inboundWork // per relay thread
	waiting map[uuid.UUID]chan struct{}    // saved user message id -> reply posted
	// early records replies that finished before their waiter was registered.
	early map[uuid.UUID]time.Time
	// orphans holds replies that finished before their inbound link was attached to
	// the user message (the turn starter returns after the turn starts), keyed by that
	// message; runInbound posts them once the link is attached.
	orphans map[uuid.UUID]orphanReply
	// lastBusy throttles NoticeBusy per binding.
	lastBusy map[uuid.UUID]time.Time
}

type orphanReply struct {
	ev replyhook.Event
	at time.Time
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastBusy == nil {
		s.lastBusy = map[uuid.UUID]time.Time{}
	}
	if at, ok := s.lastBusy[bindingID]; ok && time.Since(at) < busyNoticeEvery {
		return false
	}
	s.lastBusy[bindingID] = time.Now()
	return true
}

// threadOpen is the relay's fail-closed check that the bound thread may be driven
// from Discord: it must be sandboxed
// (RelayThreadOpenWithoutAcknowledgement), or the owner must have acknowledged the
// binding. The chat's CURRENT sandbox flag is read every time, since it can be switched
// off in the app after binding. Any doubt (thread missing, lookup failing) means closed.
// When it closes on an unacknowledged thread, the reason is recorded on the binding
// so the app can show it; nothing is posted to Discord.
func (s *Service) threadOpen(ctx context.Context, t models.DiscordBindingTarget) bool {
	b := t.Binding
	chat, err := s.Store.GetChat(ctx, t.Bot.OwnerID, b.ChatID)
	if err != nil || chat == nil {
		s.Logger.Warn("discord relay: cannot read the bound thread; dropping tag",
			zap.String("binding_id", b.ID.String()), zap.Error(err))
		return false
	}
	if RelayThreadOpenWithoutAcknowledgement(chat) {
		s.clearUnrestrictedWarning(ctx, b)
		if b.AllowUnrestricted {
			// The acknowledgement only means something while the thread is not sandboxed.
			// Withdraw it now, so switching the sandbox off later pauses the binding
			// instead of silently re-opening it.
			if err := s.Store.WithdrawDiscordBindingAcknowledgement(ctx, b.ID); err != nil {
				s.Logger.Warn("discord relay: could not withdraw a stale acknowledgement",
					zap.String("binding_id", b.ID.String()), zap.Error(err))
			}
		}
		return true
	}
	if b.AllowUnrestricted {
		s.clearUnrestrictedWarning(ctx, b)
		return true
	}
	s.Logger.Warn("discord relay: bound thread is not sandboxed and the binding is not acknowledged; dropping tag",
		zap.String("binding_id", b.ID.String()), zap.String("chat_id", b.ChatID.String()))
	if b.LastError == nil || *b.LastError != UnrestrictedWarning {
		_ = s.Store.SetDiscordBindingStatus(ctx, b.ID, b.Status, UnrestrictedWarning)
	}
	return false
}

func (s *Service) clearUnrestrictedWarning(ctx context.Context, b models.DiscordBinding) {
	if b.LastError != nil && *b.LastError == UnrestrictedWarning {
		_ = s.Store.SetDiscordBindingStatus(ctx, b.ID, b.Status, "")
	}
}

// enqueue adds w to the thread's queue, starting its worker on first use, and
// reports false when the queue is full. The send happens under the lock the worker
// takes before it retires an idle queue, so a tag can never land in a queue whose
// worker has just exited. The worker exits once the queue has been idle, so idle
// threads hold nothing.
func (s *Service) enqueue(ctx context.Context, chatID uuid.UUID, w inboundWork) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queues == nil {
		s.queues = map[uuid.UUID]chan inboundWork{}
	}
	q, ok := s.queues[chatID]
	if !ok {
		q = make(chan inboundWork, s.QueueDepth)
		s.queues[chatID] = q
		go s.work(ctx, chatID, q)
	}
	select {
	case q <- w:
		return true
	default:
		return false
	}
}

const workerIdle = time.Minute

func (s *Service) work(ctx context.Context, chatID uuid.UUID, q chan inboundWork) {
	idle := time.NewTimer(workerIdle)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case w := <-q:
			s.runInbound(ctx, w)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(workerIdle)
		case <-idle.C:
			s.mu.Lock()
			if len(q) == 0 {
				delete(s.queues, chatID)
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			idle.Reset(workerIdle)
		}
	}
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

// setWaiter registers ch to be closed when the reply to user message id is
// posted. If that already happened (a reply can finish before the turn starter
// returns), ch is closed at once.
func (s *Service) setWaiter(id uuid.UUID, ch chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, at := range s.early {
		if time.Since(at) > s.TurnTimeout {
			delete(s.early, k)
		}
	}
	if _, ok := s.early[id]; ok {
		delete(s.early, id)
		close(ch)
		return
	}
	if s.waiting == nil {
		s.waiting = map[uuid.UUID]chan struct{}{}
	}
	s.waiting[id] = ch
}

func (s *Service) clearWaiter(id uuid.UUID) {
	s.mu.Lock()
	delete(s.waiting, id)
	s.mu.Unlock()
}

func (s *Service) release(id uuid.UUID) {
	s.mu.Lock()
	ch, ok := s.waiting[id]
	delete(s.waiting, id)
	if !ok {
		if s.early == nil {
			s.early = map[uuid.UUID]time.Time{}
		}
		s.early[id] = time.Now()
	}
	s.mu.Unlock()
	if ok {
		close(ch)
	}
}

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
		} else if err == nil {
			s.keepOrphan(ev)
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
	case errors.Is(err, ErrNoAccess):
		_ = s.Store.SetDiscordBindingStatus(ctx, b.ID, models.DiscordBindingBroken, "The bot can no longer post in this channel.")
	case err != nil:
		log.Warn("discord relay: post failed", zap.Error(err))
	default:
		_ = s.Store.TouchDiscordBinding(ctx, b.ID)
	}
}

// keepOrphan remembers a reply whose trigger has no inbound link yet, when a relay
// turn is running in its thread (so ordinary chats' replies are never kept).
func (s *Service) keepOrphan(ev replyhook.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, active := s.queues[ev.ChatID]; !active || ev.TriggerMessageID == nil {
		return
	}
	if s.orphans == nil {
		s.orphans = map[uuid.UUID]orphanReply{}
	}
	for k, o := range s.orphans {
		if time.Since(o.at) > s.TurnTimeout {
			delete(s.orphans, k)
		}
	}
	s.orphans[*ev.TriggerMessageID] = orphanReply{ev: ev, at: time.Now()}
}

// takeOrphan returns and forgets the reply kept for a user message, if any.
func (s *Service) takeOrphan(messageID uuid.UUID) (replyhook.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orphans[messageID]
	if ok {
		delete(s.orphans, messageID)
	}
	return o.ev, ok
}

// notice posts a short reply to the triggering message; failures are ignored.
func (s *Service) notice(ctx context.Context, token, channelID, replyTo, text string) {
	if _, err := s.Discord.Post(ctx, token, channelID, replyTo, []string{text}, nil); err != nil {
		s.Logger.Debug("discord relay: notice failed", zap.Error(err))
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
