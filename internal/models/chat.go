package models

import (
	"time"

	"github.com/google/uuid"
)

// Chat represents a named chat session that allows users to maintain specific contexts for particular tasks
type Chat struct {
	ID                uuid.UUID  `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
	Name              string     `json:"name"`
	UnreadCount       int        `json:"unread_count"`
	LastMessageTime   *time.Time `json:"last_message_time,omitempty"`
	ResponseID        *string    `json:"-"`
	CheckpointSummary string     `json:"-"`
	// CheckpointUserMessageCount stores the total number of assistant messages at the time of the last checkpoint.
	// It is used by the checkpoint decision policy (postprocessing_policy.go) to determine *when* to
	// trigger the next checkpoint based on message cadence.
	CheckpointUserMessageCount int `json:"-"`
	// LastCheckpointAt is the wall-clock time at which the last checkpoint was written. It is used as
	// a DB range cursor to scope history fetches to the current checkpoint period (messages with
	// sent_at >= LastCheckpointAt). Both this and CheckpointUserMessageCount are always written
	// atomically in UpdateChatCheckpointStateAndClearResponseID — they represent the same event and
	// will never drift relative to each other.
	LastCheckpointAt *time.Time `json:"-"`
	ModelID          uuid.UUID  `json:"model_id"`
	ModelName        string     `json:"model_name,omitempty"`
	ToolsEnabled     bool       `json:"-"`
	DisabledTools    []string   `json:"disabled_tools,omitempty"`
	Tags             []string   `json:"tags,omitempty"`
	// IsFavorite is nil when the field was not provided (PATCH semantics).
	// When non-nil, it reflects an explicit true/false from the caller.
	IsFavorite      *bool     `json:"is_favorite,omitempty"`
	PersonalityID   uuid.UUID `json:"personality_id"`
	PersonalityName string    `json:"personality_name,omitempty"`
	// PersonalityExpressionsEnabled is populated from the personality edge on GetChat
	// (not stored on the chat row). Defaults to true when no personality is assigned.
	PersonalityExpressionsEnabled bool   `json:"-"`
	SystemPrompt                  string `json:"-"`
	Scratchpad                    string `json:"-"`
	// ActiveMoodID is the effective mood currently used for response generation.
	ActiveMoodID *uuid.UUID `json:"active_mood_id,omitempty"`
	// IsAutoMood controls mood selection policy (true = auto, false = manually pinned mood).
	IsAutoMood bool `json:"is_auto_mood"`
	// Archived reflects stored archive state when loaded from the DB (non-nil).
	// Nil means "omit on UpdateChat" so PATCH callers that do not send archived preserve the row.
	Archived *bool `json:"archived,omitempty"`
	// Source is the origin of an imported chat (e.g. "openai", "anthropic"). Internal-only; not exposed in JSON.
	Source *string `json:"-"`
	// ImportHash is the per-conversation dedup key used by the import pipeline. Internal-only; not exposed in JSON.
	ImportHash *string `json:"-"`
	// RehydrationState tracks lazy summarization of imported threads on unarchive:
	// "" (none) / "pending" / "processing" / "ready" / "failed". Surfaced read-only so the UI can
	// show a "preparing thread" affordance while the summary is generated.
	RehydrationState string    `json:"rehydration_state,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// IsFirstChat is an internal-only flag set by CreateChat to indicate whether
	// this row is the user's first chat at creation time.
	IsFirstChat bool `json:"-"`
}

// ChatFilters defines filters for listing chats
type ChatFilters struct {
	Name          *string    `json:"name,omitempty"`
	Query         *string    `json:"query,omitempty"`
	Tag           *string    `json:"tag,omitempty"`
	PersonalityID *uuid.UUID `json:"personality_id,omitempty"`
	IsFavorite    *bool      `json:"is_favorite,omitempty"`
	// Archived, when non-nil, filters list results: true = only archived, false = only non-archived.
	// When nil, list endpoints default to non-archived threads only.
	Archived *bool `json:"archived,omitempty"`
	// IncludeArchived disables the default archived=false predicate so callers can
	// discover both active and archived threads without changing HTTP list behavior.
	// IDs takes precedence: explicit IDs always bypass archive filtering.
	IncludeArchived bool       `json:"-"`
	MinDate         *time.Time `json:"min_date,omitempty"`
	MaxDate         *time.Time `json:"max_date,omitempty"`
	// Source, when set, filters to chats imported from that source (e.g. ChatSourceOpenAI,
	// ChatSourceAnthropic). Used by the post-import thread picker to list just-imported threads.
	Source *string `json:"source,omitempty"`
	// IDs, when set, restricts results to these chat IDs (still scoped to the authenticated user).
	IDs []uuid.UUID `json:"ids,omitempty"`
	// HasMessages, when true, excludes empty shells (no chat messages yet). Uses last_message_time
	// IS NOT NULL — that field is set on first message create.
	HasMessages *bool `json:"has_messages,omitempty"`
}

// ChatExport is the user-facing export of a chat
type ChatExport struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	LastMessageTime *time.Time `json:"last_message_time,omitempty"`
	ModelName       string     `json:"model_name"`
	PersonalityName string     `json:"personality_name"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ChatContext struct {
	ChatID           uuid.UUID `json:"chat_id"`
	ActiveScratchpad string    `json:"active_scratchpad"`
	Summary          string    `json:"summary"`
}
