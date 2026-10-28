package models

import (
	"time"

	"github.com/google/uuid"
)

// Remote sync keeps a copy of the owner's threads on another What Iff server (ADR 0x033).

// RemoteSyncStatus is how the last run of an account's sync went.
type RemoteSyncStatus string

const (
	// RemoteSyncStatusPending: saved, never run.
	RemoteSyncStatusPending RemoteSyncStatus = "pending"
	// RemoteSyncStatusOK: the last run succeeded.
	RemoteSyncStatusOK RemoteSyncStatus = "ok"
	// RemoteSyncStatusError: the last run failed for a reason that may pass (network, 5xx, 429);
	// the next run retries.
	RemoteSyncStatusError RemoteSyncStatus = "error"
	// RemoteSyncStatusNeedsAttention: the remote refused the token (revoked, or missing chat:read).
	// Runs stop until the settings are saved again.
	RemoteSyncStatusNeedsAttention RemoteSyncStatus = "needs_attention"
)

// RemoteSyncRunStats counts what one run did. Never content.
type RemoteSyncRunStats struct {
	// ThreadsChecked is how many remote threads were found changed and looked at.
	ThreadsChecked int `json:"threads_checked"`
	// ThreadsCreated got a new local mirror; ThreadsAdopted were matched to an existing local
	// thread (an earlier account-export import); ThreadsUpdated had messages appended;
	// ThreadsForked diverged and continued in a new local thread.
	ThreadsCreated int `json:"threads_created"`
	ThreadsAdopted int `json:"threads_adopted"`
	ThreadsUpdated int `json:"threads_updated"`
	ThreadsForked  int `json:"threads_forked"`
	// ThreadsFailed could not be synced this run and will be retried.
	ThreadsFailed int `json:"threads_failed"`
	// ThreadsDeferred were changed but left for the next run by the per-run cap.
	ThreadsDeferred int `json:"threads_deferred"`
	MessagesAdded   int `json:"messages_added"`
}

// AsMap is the stored form of the stats.
func (s RemoteSyncRunStats) AsMap() map[string]int {
	return map[string]int{
		"threads_checked":  s.ThreadsChecked,
		"threads_created":  s.ThreadsCreated,
		"threads_adopted":  s.ThreadsAdopted,
		"threads_updated":  s.ThreadsUpdated,
		"threads_forked":   s.ThreadsForked,
		"threads_failed":   s.ThreadsFailed,
		"threads_deferred": s.ThreadsDeferred,
		"messages_added":   s.MessagesAdded,
	}
}

// RemoteSyncRunStatsFromMap reverses AsMap. Missing keys are zero.
func RemoteSyncRunStatsFromMap(m map[string]int) RemoteSyncRunStats {
	return RemoteSyncRunStats{
		ThreadsChecked:  m["threads_checked"],
		ThreadsCreated:  m["threads_created"],
		ThreadsAdopted:  m["threads_adopted"],
		ThreadsUpdated:  m["threads_updated"],
		ThreadsForked:   m["threads_forked"],
		ThreadsFailed:   m["threads_failed"],
		ThreadsDeferred: m["threads_deferred"],
		MessagesAdded:   m["messages_added"],
	}
}

// RemoteSyncTotals is what sync has brought over so far.
type RemoteSyncTotals struct {
	// Threads is how many remote threads are linked (active links only).
	Threads  int `json:"threads"`
	Messages int `json:"messages"`
	Forks    int `json:"forks"`
}

// RemoteSyncSettings is the account's sync as the settings page shows it. The token itself is
// never included, only its last four characters.
type RemoteSyncSettings struct {
	Configured    bool                `json:"configured"`
	Enabled       bool                `json:"enabled"`
	BaseURL       string              `json:"base_url,omitempty"`
	TokenHint     string              `json:"token_hint,omitempty"`
	Status        RemoteSyncStatus    `json:"status,omitempty"`
	LastError     *string             `json:"last_error,omitempty"`
	LastRunAt     *time.Time          `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time          `json:"last_success_at,omitempty"`
	LastRun       *RemoteSyncRunStats `json:"last_run,omitempty"`
	// Running is true while a run holds the account's lease.
	Running bool              `json:"running"`
	Totals  *RemoteSyncTotals `json:"totals,omitempty"`
	// UnmatchedPersonas names the remote personas whose copied threads have no persona here yet.
	// Creating (or importing) a persona with one of these names gives those threads to it.
	UnmatchedPersonas []string `json:"unmatched_personas,omitempty"`
	// IntervalSeconds is how often the server syncs an enabled account.
	IntervalSeconds int `json:"interval_seconds,omitempty"`
	// DefaultBaseURL is the address the settings page suggests (the hosted app's API).
	DefaultBaseURL string `json:"default_base_url,omitempty"`
	// AllowLocalNetwork reports whether this server accepts http:// remotes (development only).
	AllowLocalNetwork bool `json:"allow_local_network,omitempty"`
}

// RemoteSyncUpsert saves an account's sync settings. Token is required the first time; nil
// keeps the stored one.
type RemoteSyncUpsert struct {
	BaseURL string  `json:"base_url"`
	Token   *string `json:"token,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// RemoteSyncAccount is an account's sync with its decrypted token, for the syncer only.
type RemoteSyncAccount struct {
	ID      uuid.UUID
	UserID  uuid.UUID
	BaseURL string
	Token   string
	Enabled bool
	Status  RemoteSyncStatus
	// LastRunAt is when the last run finished (nil: never), so a wake can skip a fresh account.
	LastRunAt *time.Time
	// FullWalk asks the syncer to list every remote thread instead of stopping at the first
	// unchanged page: there has been no successful run yet, or the last one left threads for
	// later, which may sit beyond pages that are already unchanged.
	FullWalk bool
}

// RemoteSyncRunResult is what the syncer records at the end of a run.
type RemoteSyncRunResult struct {
	Status RemoteSyncStatus
	// Error is shown to the user; it names the class of failure, never an upstream body.
	Error *string
	Stats RemoteSyncRunStats
}

// RemoteSyncLinkState is whether a remote thread is still being copied.
type RemoteSyncLinkState string

const (
	RemoteSyncLinkActive RemoteSyncLinkState = "active"
	// RemoteSyncLinkDetached: the local thread was deleted, so the remote thread is no longer copied.
	RemoteSyncLinkDetached RemoteSyncLinkState = "detached"
)

// RemoteSyncChatLink ties one remote thread to the local thread tracking it.
type RemoteSyncChatLink struct {
	ID           uuid.UUID
	RemoteChatID uuid.UUID
	// ChatID is the local thread new remote messages go to.
	ChatID                uuid.UUID
	RemoteName            string
	RemotePersonalityName string
	RemoteLastMessageTime *time.Time
	RemoteHeadSentAt      *time.Time
	Forks                 int
	State                 RemoteSyncLinkState
}

// RemoteSyncThread is a remote thread to apply, as the syncer read it.
type RemoteSyncThread struct {
	RemoteChatID    uuid.UUID
	Name            string
	PersonalityName string
	Archived        bool
	CreatedAt       time.Time
	LastMessageTime *time.Time
	// Host labels forks: "<name> (from <host>)".
	Host string
}

// RemoteSyncMessage is a remote message to copy, oldest first within a batch. Moods and
// expression portraits are local records the remote's ids do not resolve to, so only the
// generation model and persona names are carried.
type RemoteSyncMessage struct {
	RemoteID              uuid.UUID
	Message               string
	Origin                MessageOrigin
	SentAt                time.Time
	GenerationModel       string
	GenerationPersonality string
}

// RemoteSyncApplyOutcome is what applying one remote thread did locally.
type RemoteSyncApplyOutcome string

const (
	RemoteSyncUnchanged RemoteSyncApplyOutcome = "unchanged"
	RemoteSyncCreated   RemoteSyncApplyOutcome = "created"
	RemoteSyncAdopted   RemoteSyncApplyOutcome = "adopted"
	RemoteSyncAppended  RemoteSyncApplyOutcome = "appended"
	RemoteSyncForked    RemoteSyncApplyOutcome = "forked"
	RemoteSyncDetached  RemoteSyncApplyOutcome = "detached"
)

// RemoteSyncApplyResult reports one applied thread.
type RemoteSyncApplyResult struct {
	Outcome RemoteSyncApplyOutcome
	// ChatID is the local thread now tracking the remote one (the fork, after a fork).
	ChatID        uuid.UUID
	MessagesAdded int
}
