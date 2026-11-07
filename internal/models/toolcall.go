package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// ToolCallApp lives in modeltypes (shared with the Ent schema).
type ToolCallApp = modeltypes.ToolCallApp

type ToolCall struct {
	ID            uuid.UUID `json:"id"`
	ChatMessageID uuid.UUID `json:"chat_message_id"`
	ToolName      string    `json:"tool_name"`
	ToolInput     string    `json:"tool_input"`
	ToolOutput    string    `json:"tool_output"`
	ToolError     string    `json:"tool_error"`
	// Step is the Advanced-loop step (1-based) the call ran in; 0 for Standard turns.
	Step int `json:"step,omitempty"`
	// App is set when the call's MCP tool has an MCP App (ADR 0x042): the chat renders its view.
	App *ToolCallApp `json:"app,omitempty"`
	// JobDraft marks a call already saved as a ToolCall row linked to its turn's job (an
	// Advanced run writes each call as it finishes); saving the message moves that row onto it
	// instead of creating a second one. Internal-only.
	JobDraft  bool      `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
