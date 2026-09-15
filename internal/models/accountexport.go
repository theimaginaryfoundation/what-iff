package models

import "github.com/google/uuid"

// JobTypeAccountExport is the Job.job_type for a user's full-account export. It reuses the generic
// Job entity/queue (like chat_import); the export runs in-process and writes progress onto
// Job.progress. The download link itself is delivered ONLY by email (a deliberate control: app
// access alone cannot exfiltrate a full account), so it is never placed on the job.
const JobTypeAccountExport = "account_export"
const JobTypeAccountImport = "account_import"

// Account-export progress phases (stored on Job.progress as AccountExportProgress.Phase).
const (
	AccountExportPhaseQueued    = "queued"
	AccountExportPhaseBuilding  = "building"
	AccountExportPhaseUploading = "uploading"
	AccountExportPhaseComplete  = "complete"
	AccountExportPhaseFailed    = "failed"
)

// AccountExportProgress is the JSON payload on Job.progress for account_export jobs. It intentionally
// carries no download URL — the link is emailed, not surfaced in the API — only the phase, section
// counts, and a user-safe message.
type AccountExportProgress struct {
	Phase   string         `json:"phase"`
	Counts  map[string]int `json:"counts,omitempty"`
	Message string         `json:"message,omitempty"`
}

// AccountImportResult summarizes an account import.
type AccountImportResult struct {
	Conversations ImportResult        `json:"conversations"`
	Memories      MemoryImportResult  `json:"memories"`
	Personalities SectionImportCounts `json:"personalities"`
	Warnings      []string            `json:"warnings,omitempty"`
}

// AccountImportProgress is persisted on an account_import Job while the staged archive is restored.
type AccountImportProgress struct {
	Phase         string               `json:"phase"`
	Message       string               `json:"message,omitempty"`
	Counts        map[string]int       `json:"counts,omitempty"`
	Conversations ImportResult         `json:"conversations"`
	Memories      MemoryImportResult   `json:"memories"`
	Personalities SectionImportCounts  `json:"personalities"`
	Warnings      []string             `json:"warnings,omitempty"`
	Result        *AccountImportResult `json:"result,omitempty"`
}

// SectionImportCounts is a simple created/skipped tally for a straightforward import section.
type SectionImportCounts struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
}

// AccountImportSelection narrows which items of an export are restored. It is OPTIONAL on
// POST /account/import (sent as a JSON `selection` multipart field); when absent, the whole
// export is restored (backward-compatible). The IDs are the SOURCE ids as they appear in the
// export ZIP — personality ids and conversation uuids — which the client reads from the archive
// to build its selection ledger.
//
// A selection that IS present is authoritative: only the listed personalities and conversations
// are restored (empty list ⇒ none). Memories are all-or-nothing via IncludeMemories, since an
// account can carry thousands and itemizing them is impractical.
type AccountImportSelection struct {
	PersonalityIDs  []uuid.UUID `json:"personality_ids"`
	ConversationIDs []uuid.UUID `json:"conversation_ids"`
	IncludeMemories bool        `json:"include_memories"`
}
