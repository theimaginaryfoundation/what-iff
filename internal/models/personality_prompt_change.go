package models

import (
	"time"

	"github.com/google/uuid"
)

type PersonalityPromptChangeAction string

const (
	PersonalityPromptChangeActionEdit   PersonalityPromptChangeAction = "edit"
	PersonalityPromptChangeActionRevert PersonalityPromptChangeAction = "revert"
)

// PersonalityPromptChange describes one immutable transition of a personality's
// system prompt. A revert is represented by another transition and optionally
// points at the historical change selected for restoration.
type PersonalityPromptChange struct {
	ID               uuid.UUID                     `json:"id"`
	UserID           uuid.UUID                     `json:"user_id"`
	PersonalityID    uuid.UUID                     `json:"personality_id"`
	OldPrompt        string                        `json:"old_prompt"`
	NewPrompt        string                        `json:"new_prompt"`
	Action           PersonalityPromptChangeAction `json:"action"`
	RevertedChangeID *uuid.UUID                    `json:"reverted_change_id,omitempty"`
	CreatedAt        time.Time                     `json:"created_at"`
}
