package models

import (
	"time"

	"github.com/google/uuid"
)

// Import source identifiers persisted on Chat.source for imported threads.
const (
	ChatSourceOpenAI    = "openai"
	ChatSourceAnthropic = "anthropic"
)

// Rehydration lifecycle states for imported threads (Chat.rehydration_state).
const (
	RehydrationStateNone       = ""
	RehydrationStatePending    = "pending"
	RehydrationStateProcessing = "processing"
	RehydrationStateReady      = "ready"
	RehydrationStateFailed     = "failed"
)

// ImportResult summarizes a chat import run.
// Errors contains client-visible per-conversation error strings (capped at models.MaxImportErrors).
// Strings in this slice are crafted by both the handler and the datastore layer and must not
// include sensitive internal details (e.g. raw SQL errors, stack traces).
type ImportResult struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
	// ImportedIDs lists chat rows created in this run (append order). Used by the post-import picker.
	ImportedIDs []uuid.UUID `json:"imported_ids,omitempty"`
}

// ImportProgress is the JSON payload stored on Job.progress for chat_import jobs so the UI can
// render an active-looking progress indicator while the backend works through the export.
type ImportProgress struct {
	// Phase is a coarse stage label: "uploading"/"parsing"/"importing"/"complete"/"failed".
	Phase string `json:"phase"`
	// Source is the detected export origin ("openai"/"anthropic"), when known.
	Source string `json:"source,omitempty"`
	// Total is the number of conversations to import (known after parsing).
	Total int `json:"total"`
	// Imported and Skipped accumulate as conversations are persisted.
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	// ImportedIDs lists chat rows created in this run (present on phase=complete). Used by the
	// post-import picker so it only offers threads from the current import, not older archives.
	ImportedIDs []uuid.UUID `json:"imported_ids,omitempty"`
}

// ImportConversation is a parsed, role-filtered conversation ready for the datastore to persist.
type ImportConversation struct {
	Title     string
	CreatedAt time.Time
	// Source is the export origin persisted on the created chat (e.g. "openai", "anthropic").
	// Empty defaults to "openai" in the datastore for backward compatibility.
	Source string
	// ImportHash is sha256(conversationID) and must be unique per user; enforced by the
	// (owner_id, import_hash) unique index on the chats table. Using the export's own
	// conversation ID (not timestamps) ensures stable dedup across repeated imports.
	ImportHash string
	Messages   []ChatMessage
}

// MaxImportTitleLen is the maximum number of runes from a conversation title included in
// import error strings. Both the handler and the datastore use this constant to guarantee
// consistent user-visible message lengths.
const MaxImportTitleLen = 80

// TruncateImportTitle shortens s to at most MaxImportTitleLen runes for safe inclusion in
// import error strings. Handles multi-byte Unicode correctly by operating on runes.
func TruncateImportTitle(s string) string {
	runes := []rune(s)
	if len(runes) <= MaxImportTitleLen {
		return s
	}
	return string(runes[:MaxImportTitleLen]) + "…"
}
