package models

import (
	"time"

	"github.com/google/uuid"
)

// ForkChatParams describes a "What if…" branch of an existing thread.
type ForkChatParams struct {
	ParentChatID uuid.UUID
	// MessageID is the branch point in the parent thread.
	MessageID uuid.UUID
	// IncludeMessage keeps the branch-point message in the branch (continue after it). When false
	// the branch ends just before it — used to re-ask a user turn differently ("what if I'd said…").
	IncludeMessage bool
	// Name overrides the generated "What if: <parent>" name when non-empty.
	Name string
}

// ForkChatResult is the new branch plus what the caller must do to make its context correct.
type ForkChatResult struct {
	Chat *Chat
	// CopiedMessages is how many parent messages the branch starts with.
	CopiedMessages int
	// NeedsRehydration is true when the parent's checkpoint summary covers turns after the branch
	// point (so it cannot be reused) or the parent is an unsummarized import. The branch is created
	// with rehydration_state=pending and the caller must enqueue summarization of the copied prefix.
	NeedsRehydration bool
}

// ChatBranchSummary is a lightweight view of a branch for lineage UI.
type ChatBranchSummary struct {
	ID                  uuid.UUID  `json:"id"`
	Name                string     `json:"name"`
	ForkedFromMessageID *uuid.UUID `json:"forked_from_message_id,omitempty"`
	LastMessageTime     *time.Time `json:"last_message_time,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// ChatLineageParent identifies the thread a branch diverged from.
type ChatLineageParent struct {
	ID        uuid.UUID  `json:"id"`
	MessageID *uuid.UUID `json:"message_id,omitempty"`
	// Name is empty and Deleted true when the parent thread no longer exists.
	Name    string `json:"name,omitempty"`
	Deleted bool   `json:"deleted"`
}

// ChatLineage is a thread's place in its branch tree: where it came from and what branched off it.
type ChatLineage struct {
	Parent   *ChatLineageParent  `json:"parent"`
	Branches []ChatBranchSummary `json:"branches"`
}
