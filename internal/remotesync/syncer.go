// Package remotesync keeps a copy of a user's threads from another What Iff server (usually the
// hosted app) in this one, pulled through the remote's webhook read routes with a chat:read
// token. See docs/adr/0x033-mullet-mode-remote-sync.md.
package remotesync

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ErrBusy: a run already holds the account's lease.
var ErrBusy = errors.New("a sync is already running")

// ErrDisabled: the account's sync is switched off.
var ErrDisabled = errors.New("sync is switched off")

// Store is the storage a syncer needs; *datastore.Datastore implements it. Every method is
// owner-scoped.
type Store interface {
	GetRemoteSyncAccount(ctx context.Context, userID uuid.UUID) (*models.RemoteSyncAccount, error)
	ListRemoteSyncChatLinks(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]models.RemoteSyncChatLink, error)
	ApplyRemoteSyncThread(ctx context.Context, userID uuid.UUID, thread models.RemoteSyncThread, batch []models.RemoteSyncMessage) (*models.RemoteSyncApplyResult, error)
	ClaimRemoteSync(ctx context.Context, userID uuid.UUID, leaseUntil time.Time) (bool, error)
	ExtendRemoteSyncLease(ctx context.Context, userID uuid.UUID, leaseUntil time.Time) error
	FinishRemoteSyncRun(ctx context.Context, userID uuid.UUID, result models.RemoteSyncRunResult) error
	ListDueRemoteSyncs(ctx context.Context, lastRunBefore time.Time) ([]models.RemoteSyncDue, error)
	RematchRemoteSyncPersonas(ctx context.Context, userID uuid.UUID) (int, error)
}

// Remote is the remote server as a syncer reads it; *Client implements it.
type Remote interface {
	Host() string
	ListChats(ctx context.Context, page int, archived bool) ([]RemoteChat, error)
	ListMessages(ctx context.Context, chatID uuid.UUID, cursor string) ([]models.WebhookMessage, string, error)
	// GetSummary returns the thread's running summary, or nil when the remote has none to give
	// (no summary yet, or a server from before the summary route).
	GetSummary(ctx context.Context, chatID uuid.UUID) (*models.ChatSummary, error)
}

// Config tunes a Syncer. Zero values take the defaults below.
type Config struct {
	// Interval is how often an enabled account is synced in the background, on top of the syncs
	// a wake starts. Each run is due a little later than this, by up to a fifth of it (see
	// backgroundJitter). The plugin may not run the background runner at all (wake-only).
	Interval time.Duration
	// Lease is how long a run holds an account before another may take it; renewed per thread.
	Lease time.Duration
	// Overlap is how far before the last copied message a pull re-reads, to catch a message
	// that committed late with an earlier timestamp. Re-read messages are deduped.
	Overlap time.Duration
	// WakeMinAge is how old the last run must be before a wake starts another.
	WakeMinAge time.Duration
	// MaxThreadsPerRun bounds one run; the rest wait for the next.
	MaxThreadsPerRun int
	// MaxListPages and MaxMessagePages bound one walk, as a guard against a remote that never ends.
	MaxListPages    int
	MaxMessagePages int
	// AllowLocalNetwork lets the default remote reach private and loopback addresses (a server
	// on the owner's own network, or development).
	AllowLocalNetwork bool
	// NewRemote builds the remote for an account. Defaults to NewClient over NewHTTPClient.
	NewRemote func(baseURL, token string) (Remote, error)
}

const (
	DefaultInterval         = time.Hour
	DefaultLease            = 15 * time.Minute
	DefaultOverlap          = 2 * time.Minute
	DefaultMaxThreadsPerRun = 50
	defaultMaxListPages     = 200
	defaultMaxMessagePages  = 2000
)

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = DefaultInterval
	}
	if c.WakeMinAge <= 0 {
		c.WakeMinAge = DefaultWakeMinAge
	}
	if c.Lease <= 0 {
		c.Lease = DefaultLease
	}
	if c.Overlap <= 0 {
		c.Overlap = DefaultOverlap
	}
	if c.MaxThreadsPerRun <= 0 {
		c.MaxThreadsPerRun = DefaultMaxThreadsPerRun
	}
	if c.MaxListPages <= 0 {
		c.MaxListPages = defaultMaxListPages
	}
	if c.MaxMessagePages <= 0 {
		c.MaxMessagePages = defaultMaxMessagePages
	}
	if c.NewRemote == nil {
		httpClient := NewHTTPClient(c.AllowLocalNetwork)
		c.NewRemote = func(baseURL, token string) (Remote, error) { return NewClient(baseURL, token, httpClient) }
	}
	return c
}

// Syncer runs accounts' syncs.
type Syncer struct {
	store  Store
	cfg    Config
	logger *zap.Logger
}

// NewSyncer builds a Syncer. logger may be nil.
func NewSyncer(store Store, cfg Config, logger *zap.Logger) *Syncer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Syncer{store: store, cfg: cfg.withDefaults(), logger: logger}
}

// Interval is how often an enabled account is synced.
func (s *Syncer) Interval() time.Duration { return s.cfg.Interval }

// Start claims the account and runs its sync in the background on ctx (the server's lifecycle,
// not a request). It returns ErrBusy when a run already holds the account, so "sync now" can say
// so, and ErrDisabled when sync is off.
func (s *Syncer) Start(ctx context.Context, userID uuid.UUID) error {
	acct, err := s.store.GetRemoteSyncAccount(ctx, userID)
	if err != nil {
		return err
	}
	if !acct.Enabled {
		return ErrDisabled
	}
	ok, err := s.store.ClaimRemoteSync(ctx, userID, time.Now().Add(s.cfg.Lease))
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy
	}
	go s.runClaimed(ctx, userID)
	return nil
}

// DefaultWakeMinAge is how old an account's last run must be before a wake starts another.
const DefaultWakeMinAge = 2 * time.Minute

// WakeResult is what a wake did.
type WakeResult struct {
	// Started is true when the wake started a run.
	Started bool `json:"started"`
	// Rematched is how many copies just got a persona that now exists here.
	Rematched int `json:"rematched"`
}

// Wake is the local app saying "someone is using me": it starts a run when the account's last
// one is older than the wake minimum age, and reports whether it did. Every wake also gives
// copies without a persona one whose name now matches (no remote call), so a persona created
// or imported a moment ago takes its threads straight away. This is how copies stay
// current without polling: the remote is asked when the copy is about to be read, not on a
// clock. An account that is off, needs its owner, was synced moments ago or is already
// running is left alone; none of those is an error.
func (s *Syncer) Wake(ctx context.Context, userID uuid.UUID) (WakeResult, error) {
	var res WakeResult
	acct, err := s.store.GetRemoteSyncAccount(ctx, userID)
	if err != nil {
		return res, err
	}
	if res.Rematched, err = s.store.RematchRemoteSyncPersonas(ctx, userID); err != nil {
		s.logger.Warn("remote sync: rematch personas", zap.String("user_id", userID.String()), zap.Error(err))
	}
	if !acct.Enabled || acct.Status == models.RemoteSyncStatusNeedsAttention {
		return res, nil
	}
	if acct.LastRunAt != nil && time.Since(*acct.LastRunAt) < s.cfg.WakeMinAge {
		return res, nil
	}
	ok, err := s.store.ClaimRemoteSync(ctx, userID, time.Now().Add(s.cfg.Lease))
	if err != nil || !ok {
		return res, err
	}
	go s.runClaimed(ctx, userID)
	res.Started = true
	return res, nil
}

// RunDue runs every account that is due, one after another. The runner calls it on each tick.
func (s *Syncer) RunDue(ctx context.Context) {
	now := time.Now()
	due, err := s.store.ListDueRemoteSyncs(ctx, now.Add(-s.cfg.Interval))
	if err != nil {
		s.logger.Warn("remote sync: list due accounts", zap.Error(err))
		return
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		if d.LastRunAt != nil && now.Before(d.LastRunAt.Add(s.cfg.Interval+backgroundJitter(d.UserID, *d.LastRunAt, s.cfg.Interval))) {
			continue
		}
		userID := d.UserID
		ok, err := s.store.ClaimRemoteSync(ctx, userID, time.Now().Add(s.cfg.Lease))
		if err != nil {
			s.logger.Warn("remote sync: claim", zap.String("user_id", userID.String()), zap.Error(err))
			continue
		}
		if ok {
			s.runClaimed(ctx, userID)
		}
	}
}

// backgroundJitter is how much later than interval an account's next background run is due: up
// to a fifth of the interval, different for every account and every run.
//
// Without it, servers that start together (after a release, or a power cut) would stay in step
// and all call the remote at the same moment every interval; with it they drift apart within a
// few cycles. It is derived from the account and its last run rather than drawn on each tick, so
// runs spread evenly over the window instead of bunching at its start.
func backgroundJitter(userID uuid.UUID, lastRun time.Time, interval time.Duration) time.Duration {
	spread := interval / 5
	if spread <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write(userID[:])
	_ = binary.Write(h, binary.LittleEndian, lastRun.UnixMicro())
	return time.Duration(h.Sum64() % uint64(spread))
}

// runClaimed runs one claimed account and records the outcome, which also releases the lease.
//
// A run that leaves changed threads for later (more than MaxThreadsPerRun) goes straight on with
// another round, so a big first sync finishes in one go rather than one batch per wake or
// interval. Later rounds walk the whole list, because the newest threads are now unchanged and
// would stop an incremental walk before it reached the rest. Rounds stop when nothing is left,
// on any failure, or when a round leaves no fewer threads than the one before.
func (s *Syncer) runClaimed(ctx context.Context, userID uuid.UUID) {
	defer func() {
		if v := recover(); v != nil {
			s.logger.Error("remote sync: run panicked", zap.String("user_id", userID.String()), zap.Any("panic", v))
		}
	}()
	result := s.run(ctx, userID, false)
	for result.Status == models.RemoteSyncStatusOK && result.Stats.ThreadsDeferred > 0 {
		next := s.run(ctx, userID, true)
		if next.Status == "" {
			break
		}
		progressed := next.Stats.ThreadsDeferred < result.Stats.ThreadsDeferred
		result = addRounds(result, next)
		if !progressed {
			break
		}
	}
	if result.Status == "" {
		// Cancelled (shutdown): leave the lease to expire; the next run starts over safely.
		return
	}
	// Record with a fresh context: a run that ends at shutdown still releases its lease.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.store.FinishRemoteSyncRun(finishCtx, userID, result); err != nil {
		s.logger.Warn("remote sync: record run", zap.String("user_id", userID.String()), zap.Error(err))
	}
	st := result.Stats
	s.logger.Info("remote sync: run finished",
		zap.String("user_id", userID.String()),
		zap.String("status", string(result.Status)),
		zap.Int("threads_checked", st.ThreadsChecked),
		zap.Int("threads_created", st.ThreadsCreated),
		zap.Int("threads_adopted", st.ThreadsAdopted),
		zap.Int("threads_updated", st.ThreadsUpdated),
		zap.Int("threads_forked", st.ThreadsForked),
		zap.Int("threads_failed", st.ThreadsFailed),
		zap.Int("threads_deferred", st.ThreadsDeferred),
		zap.Int("messages_added", st.MessagesAdded))
}

// addRounds folds a follow-up round into a run: counts add up, and the outcome and what is
// still left over are the latest round's.
func addRounds(run, next models.RemoteSyncRunResult) models.RemoteSyncRunResult {
	a, b := run.Stats, next.Stats
	next.Stats = models.RemoteSyncRunStats{
		ThreadsChecked:  a.ThreadsChecked + b.ThreadsChecked,
		ThreadsCreated:  a.ThreadsCreated + b.ThreadsCreated,
		ThreadsAdopted:  a.ThreadsAdopted + b.ThreadsAdopted,
		ThreadsUpdated:  a.ThreadsUpdated + b.ThreadsUpdated,
		ThreadsForked:   a.ThreadsForked + b.ThreadsForked,
		ThreadsFailed:   a.ThreadsFailed + b.ThreadsFailed,
		ThreadsDeferred: b.ThreadsDeferred,
		MessagesAdded:   a.MessagesAdded + b.MessagesAdded,
	}
	return next
}

// run does one round of an account's sync. fullWalk forces a walk of the whole thread list (a
// follow-up round); the account may also ask for one. A zero Status means it was cancelled.
func (s *Syncer) run(ctx context.Context, userID uuid.UUID, fullWalk bool) models.RemoteSyncRunResult {
	fail := func(status models.RemoteSyncStatus, err error, stats models.RemoteSyncRunStats) models.RemoteSyncRunResult {
		if ctx.Err() != nil {
			return models.RemoteSyncRunResult{}
		}
		msg := UserMessage(err)
		if status == models.RemoteSyncStatusError && KindOf(err) == "" {
			s.logger.Warn("remote sync: run failed", zap.String("user_id", userID.String()), zap.Error(err))
		}
		return models.RemoteSyncRunResult{Status: status, Error: &msg, Stats: stats}
	}
	statusFor := func(err error) models.RemoteSyncStatus {
		if NeedsOwner(err) {
			return models.RemoteSyncStatusNeedsAttention
		}
		return models.RemoteSyncStatusError
	}

	acct, err := s.store.GetRemoteSyncAccount(ctx, userID)
	if err != nil {
		return fail(models.RemoteSyncStatusError, err, models.RemoteSyncRunStats{})
	}
	remote, err := s.cfg.NewRemote(acct.BaseURL, acct.Token)
	if err != nil {
		return fail(models.RemoteSyncStatusNeedsAttention, &Error{Kind: KindBadResponse, Err: err}, models.RemoteSyncRunStats{})
	}
	links, err := s.store.ListRemoteSyncChatLinks(ctx, userID)
	if err != nil {
		return fail(models.RemoteSyncStatusError, err, models.RemoteSyncRunStats{})
	}

	changed, err := s.discover(ctx, remote, links, fullWalk || acct.FullWalk)
	if err != nil {
		return fail(statusFor(err), err, models.RemoteSyncRunStats{})
	}

	var stats models.RemoteSyncRunStats
	if len(changed) > s.cfg.MaxThreadsPerRun {
		stats.ThreadsDeferred = len(changed) - s.cfg.MaxThreadsPerRun
		changed = changed[:s.cfg.MaxThreadsPerRun]
	}
	for _, chat := range changed {
		if ctx.Err() != nil {
			return models.RemoteSyncRunResult{}
		}
		stats.ThreadsChecked++
		link, linked := links[chat.ID]
		res, err := s.syncThread(ctx, userID, remote, chat, link, linked)
		if err != nil {
			if NeedsOwner(err) {
				return fail(models.RemoteSyncStatusNeedsAttention, err, stats)
			}
			if ctx.Err() != nil {
				return models.RemoteSyncRunResult{}
			}
			stats.ThreadsFailed++
			s.logger.Warn("remote sync: thread failed",
				zap.String("user_id", userID.String()), zap.String("remote_chat_id", chat.ID.String()), zap.Error(err))
			continue
		}
		countOutcome(&stats, res)
		if err := s.store.ExtendRemoteSyncLease(ctx, userID, time.Now().Add(s.cfg.Lease)); err != nil {
			s.logger.Warn("remote sync: extend lease", zap.String("user_id", userID.String()), zap.Error(err))
		}
	}

	if _, err := s.store.RematchRemoteSyncPersonas(ctx, userID); err != nil {
		s.logger.Warn("remote sync: rematch personas", zap.String("user_id", userID.String()), zap.Error(err))
	}

	if stats.ThreadsFailed > 0 {
		msg := fmt.Sprintf("%d thread(s) could not be synced this time. Sync will try them again.", stats.ThreadsFailed)
		return models.RemoteSyncRunResult{Status: models.RemoteSyncStatusError, Error: &msg, Stats: stats}
	}
	return models.RemoteSyncRunResult{Status: models.RemoteSyncStatusOK, Stats: stats}
}

func countOutcome(stats *models.RemoteSyncRunStats, res *models.RemoteSyncApplyResult) {
	switch res.Outcome {
	case models.RemoteSyncCreated:
		stats.ThreadsCreated++
	case models.RemoteSyncAdopted:
		stats.ThreadsAdopted++
	case models.RemoteSyncAppended:
		stats.ThreadsUpdated++
	case models.RemoteSyncForked:
		stats.ThreadsForked++
	}
	stats.MessagesAdded += res.MessagesAdded
}

// discover lists the remote threads that changed since they were last seen: no link yet, or a
// different last_message_time. The list is newest activity first, so the walk stops at the first
// page with nothing changed, unless fullWalk asks for every page. Threads without messages are
// skipped until they have some.
func (s *Syncer) discover(ctx context.Context, remote Remote, links map[uuid.UUID]models.RemoteSyncChatLink, fullWalk bool) ([]RemoteChat, error) {
	var changed []RemoteChat
	for _, archived := range []bool{false, true} {
		for page := 1; page <= s.cfg.MaxListPages; page++ {
			chats, err := remote.ListChats(ctx, page, archived)
			if err != nil {
				return nil, err
			}
			pageChanged, pageHasMessages := false, false
			for _, c := range chats {
				if c.LastMessageTime == nil {
					continue
				}
				pageHasMessages = true
				if isChanged(c, links) {
					changed = append(changed, c)
					pageChanged = true
				}
			}
			if len(chats) < PageSize {
				break
			}
			if !fullWalk && pageHasMessages && !pageChanged {
				break
			}
		}
	}
	return changed, nil
}

func isChanged(c RemoteChat, links map[uuid.UUID]models.RemoteSyncChatLink) bool {
	link, ok := links[c.ID]
	if !ok || link.RemoteLastMessageTime == nil {
		return true
	}
	return !link.RemoteLastMessageTime.Equal(*c.LastMessageTime)
}

// syncThread pulls one remote thread's new messages and applies them.
func (s *Syncer) syncThread(ctx context.Context, userID uuid.UUID, remote Remote, chat RemoteChat, link models.RemoteSyncChatLink, linked bool) (*models.RemoteSyncApplyResult, error) {
	thread := models.RemoteSyncThread{
		RemoteChatID:    chat.ID,
		Name:            chat.Name,
		PersonalityName: chat.PersonalityName,
		Archived:        chat.Archived != nil && *chat.Archived,
		CreatedAt:       chat.CreatedAt,
		LastMessageTime: chat.LastMessageTime,
		Host:            remote.Host(),
	}
	if linked && link.State == models.RemoteSyncLinkDetached {
		// Deleted here on purpose: record that it was seen, copy nothing.
		return s.store.ApplyRemoteSyncThread(ctx, userID, thread, nil)
	}

	// A new link needs the whole history (a mirror is built from it, an adoption aligned
	// against it); an existing one needs what came after its head, less the overlap.
	var cutoff time.Time
	if linked && link.RemoteHeadSentAt != nil {
		cutoff = link.RemoteHeadSentAt.Add(-s.cfg.Overlap)
	}
	batch, err := s.pull(ctx, remote, chat.ID, cutoff)
	if err != nil {
		if KindOf(err) == KindNotFound {
			// Deleted remotely between the list and the read: nothing to copy, keep the local copy.
			return &models.RemoteSyncApplyResult{Outcome: models.RemoteSyncUnchanged}, nil
		}
		return nil, err
	}
	if len(batch) > 0 {
		// Only a thread that is getting messages can need a newer summary.
		if thread.Summary, err = s.summary(ctx, remote, chat.ID); err != nil {
			return nil, err
		}
	}
	return s.store.ApplyRemoteSyncThread(ctx, userID, thread, batch)
}

// summary fetches the remote's running summary of a thread. It is an optimisation: without it a
// copy is summarized here instead (lazy rehydration, or a catch-up). So any failure but a refused
// token just means no summary this time.
func (s *Syncer) summary(ctx context.Context, remote Remote, chatID uuid.UUID) (*models.ChatSummary, error) {
	sum, err := remote.GetSummary(ctx, chatID)
	switch {
	case NeedsOwner(err):
		return nil, err
	case err != nil:
		if ctx.Err() == nil {
			s.logger.Debug("remote sync: no summary", zap.String("remote_chat_id", chatID.String()), zap.Error(err))
		}
		return nil, nil
	case sum == nil || strings.TrimSpace(sum.Summary) == "" || sum.WindowStart == nil:
		return nil, nil
	}
	return sum, nil
}

// pull walks a thread's messages newest first until one is older than cutoff (or the history
// ends) and returns them oldest first.
func (s *Syncer) pull(ctx context.Context, remote Remote, chatID uuid.UUID, cutoff time.Time) ([]models.RemoteSyncMessage, error) {
	var out []models.RemoteSyncMessage
	cursor := ""
	for page := 0; page < s.cfg.MaxMessagePages; page++ {
		msgs, next, err := remote.ListMessages(ctx, chatID, cursor)
		if err != nil {
			return nil, err
		}
		done := false
		for _, m := range msgs {
			if !cutoff.IsZero() && m.SentAt.Before(cutoff) {
				done = true
				break
			}
			if m.Origin != models.MessageOriginUser && m.Origin != models.MessageOriginAssistant {
				continue
			}
			// A blank reply (a generation that failed or was cut off) has nothing to continue
			// from, and an account export leaves it out: copying it would stop adoption from
			// lining an imported thread up with the remote, and fork it for nothing.
			if strings.TrimSpace(m.Message) == "" {
				continue
			}
			out = append(out, models.RemoteSyncMessage{
				RemoteID:              m.ID,
				Message:               m.Message,
				Origin:                m.Origin,
				SentAt:                m.SentAt,
				GenerationModel:       m.GenerationModel,
				GenerationPersonality: m.GenerationPersonality,
			})
		}
		if done || len(msgs) < PageSize || next == "" {
			break
		}
		cursor = next
	}
	slices.Reverse(out)
	return out, nil
}
