package models

import (
	"time"

	"github.com/google/uuid"
)

type ToolCall struct {
	ID            uuid.UUID `json:"id"`
	ChatMessageID uuid.UUID `json:"chat_message_id"`
	ToolName      string    `json:"tool_name"`
	ToolInput     string    `json:"tool_input"`
	ToolOutput    string    `json:"tool_output"`
	ToolError     string    `json:"tool_error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
