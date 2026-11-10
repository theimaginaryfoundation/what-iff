package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// Agent prompts (ADR 0x042) live in modeltypes so the Ent schema can store them.
type (
	AgentPromptSource   = modeltypes.AgentPromptSource
	AgentPromptKind     = modeltypes.AgentPromptKind
	AgentPromptStatus   = modeltypes.AgentPromptStatus
	AgentPromptAction   = modeltypes.AgentPromptAction
	AgentPromptOption   = modeltypes.AgentPromptOption
	AgentPromptQuestion = modeltypes.AgentPromptQuestion
	AgentPromptPayload  = modeltypes.AgentPromptPayload
	AgentPromptAnswer   = modeltypes.AgentPromptAnswer
	AgentPromptResponse = modeltypes.AgentPromptResponse
	AgentPromptView     = modeltypes.AgentPromptView
)

const (
	AgentPromptSourceAgent       = modeltypes.AgentPromptSourceAgent
	AgentPromptSourceElicitation = modeltypes.AgentPromptSourceElicitation
	AgentPromptKindQuestions     = modeltypes.AgentPromptKindQuestions
	AgentPromptKindApproval      = modeltypes.AgentPromptKindApproval
	AgentPromptKindForm          = modeltypes.AgentPromptKindForm
	AgentPromptKindURL           = modeltypes.AgentPromptKindURL
	AgentPromptPending           = modeltypes.AgentPromptPending
	AgentPromptAnswered          = modeltypes.AgentPromptAnswered
	AgentPromptExpired           = modeltypes.AgentPromptExpired
	AgentPromptCancelled         = modeltypes.AgentPromptCancelled
	AgentPromptAccept            = modeltypes.AgentPromptAccept
	AgentPromptDecline           = modeltypes.AgentPromptDecline
	AgentPromptCancel            = modeltypes.AgentPromptCancel
)

// AgentPrompt is a stored prompt (ent AgentPrompt).
type AgentPrompt struct {
	ID         uuid.UUID            `json:"id"`
	UserID     uuid.UUID            `json:"-"`
	ChatID     uuid.UUID            `json:"chat_id"`
	JobID      uuid.UUID            `json:"job_id"`
	Source     AgentPromptSource    `json:"source"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	Payload    AgentPromptPayload   `json:"payload"`
	Status     AgentPromptStatus    `json:"status"`
	Response   *AgentPromptResponse `json:"response,omitempty"`
	ExpiresAt  time.Time            `json:"expires_at"`
	AnsweredAt *time.Time           `json:"answered_at,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
}
