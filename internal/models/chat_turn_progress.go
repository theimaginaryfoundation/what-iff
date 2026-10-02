package models

import "time"

// ChatTurnToolStatus is the lifecycle state of one tool call in an in-flight chat turn.
type ChatTurnToolStatus string

const (
	ChatTurnToolRunning  ChatTurnToolStatus = "running"
	ChatTurnToolComplete ChatTurnToolStatus = "complete"
	ChatTurnToolError    ChatTurnToolStatus = "error"
)

// ChatTurnProgress is the Job.Progress payload for chat_message jobs: a live timeline of the
// tool calls the agent loop has started so far. It is display-only — the durable record is
// still the ToolCall rows saved with the assistant message when the turn completes.
type ChatTurnProgress struct {
	ToolCalls []ChatTurnToolCall `json:"tool_calls"`
}

// ChatTurnToolCall is one tool call in ChatTurnProgress. Input and Output are truncated
// previews; the full values are on the persisted ToolCall once the turn is saved.
type ChatTurnToolCall struct {
	// ID is the provider's tool-call id, unique within the turn.
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Input  string             `json:"input,omitempty"`
	Status ChatTurnToolStatus `json:"status"`
	// Output is the tool result (or error text) preview, set once the call finishes.
	Output string `json:"output,omitempty"`
	// Round is the zero-based agent-loop round that requested the call.
	Round      int        `json:"round"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}
