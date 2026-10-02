package models

import (
	"time"

	"github.com/google/uuid"
)

// Workspace roots. A root decides who a file belongs to: WorkspaceRootAgent is a personality's
// notebook (shared by that personality's conversations), WorkspaceRootChat is one conversation's
// working files.
const (
	WorkspaceRootAgent = "agent"
	WorkspaceRootChat  = "chat"
)

// Workspace file states.
const (
	WorkspaceFileLive    = "live"
	WorkspaceFileDeleted = "deleted"
)

// Workspace write operations, recorded on each revision.
const (
	WorkspaceOpCreate = "create"
	WorkspaceOpWrite  = "write"
	WorkspaceOpAppend = "append"
	WorkspaceOpEdit   = "edit"
	WorkspaceOpDelete = "delete"
)

// Author classes (trust classes) for workspace changes.
const (
	WorkspaceAuthorUser   = "user"
	WorkspaceAuthorAgent  = "agent"
	WorkspaceAuthorSystem = "system"
)

// WorkspaceFile is an agent-writable text file and the metadata of its current revision.
type WorkspaceFile struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"user_id"`
	Root            string     `json:"root"`
	RootRef         uuid.UUID  `json:"root_ref"`
	Path            string     `json:"path"`
	ContentType     string     `json:"content_type"`
	CurrentRevision int        `json:"current_revision"`
	Size            int64      `json:"size"`
	SHA256          string     `json:"sha256"`
	State           string     `json:"state"`
	AuthorClass     string     `json:"author_class"`
	StorageKey      string     `json:"-"`
	ReadCount       int        `json:"read_count"`
	LastReadAt      *time.Time `json:"last_read_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// WorkspaceRevisionInput describes one write to commit. The content object must already be in
// storage at StorageKey (empty for a delete).
type WorkspaceRevisionInput struct {
	Root        string
	RootRef     uuid.UUID
	Path        string
	ContentType string
	Op          string
	// BaseRevision is the revision the writer last saw. The commit is refused with a conflict when
	// the file's current revision differs. Use WorkspaceAnyRevision for writes that do not need the
	// check (creating a new file, appending); they are still serialized against concurrent writes.
	BaseRevision int
	StorageKey   string
	Size         int64
	SHA256       string
	AuthorClass  string
	ChatID       *uuid.UUID
}

// WorkspaceAnyRevision skips the base-revision check on commit.
const WorkspaceAnyRevision = -1

// WorkspaceUsage is a user's live workspace footprint, for quotas.
type WorkspaceUsage struct {
	Files int
	Bytes int64
}
