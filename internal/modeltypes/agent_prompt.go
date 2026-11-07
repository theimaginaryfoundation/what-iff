package modeltypes

import (
	"encoding/json"
	"time"
)

// Agent prompts (ADR 0x042): a running turn stops to ask the person something and waits for the
// answer. The agent asks with ask_user (questions, or an approval), an MCP server asks through
// elicitation, and the answer comes back through the API. The payload is what the card shows; the
// response is what the person sent.

// AgentPromptSource is who is asking.
type AgentPromptSource string

const (
	// AgentPromptSourceAgent is the agent's own ask_user call.
	AgentPromptSourceAgent AgentPromptSource = "agent"
	// AgentPromptSourceElicitation is an MCP server's elicitation/create request, made while one of
	// its tools runs.
	AgentPromptSourceElicitation AgentPromptSource = "mcp_elicitation"
)

// AgentPromptKind is the shape of the card.
type AgentPromptKind string

const (
	// AgentPromptKindQuestions is one to four questions, each with options or free text.
	AgentPromptKindQuestions AgentPromptKind = "questions"
	// AgentPromptKindApproval asks to go ahead with an action: approve or deny, with a note.
	AgentPromptKindApproval AgentPromptKind = "approval"
	// AgentPromptKindForm is an MCP elicitation form (a flat JSON Schema of primitive fields).
	AgentPromptKindForm AgentPromptKind = "form"
	// AgentPromptKindURL is an MCP URL-mode elicitation: the person opens a link to do something
	// outside the app (sign in, enter a key), then says they are done.
	AgentPromptKindURL AgentPromptKind = "url"
)

// AgentPromptStatus is where a prompt stands.
type AgentPromptStatus string

const (
	AgentPromptPending AgentPromptStatus = "pending"
	// AgentPromptAnswered: the person answered (for an approval or an elicitation, the response's
	// Action says whether they accepted, declined or cancelled).
	AgentPromptAnswered AgentPromptStatus = "answered"
	// AgentPromptExpired: nobody answered in time.
	AgentPromptExpired AgentPromptStatus = "expired"
	// AgentPromptCancelled: the turn ended (Stop, a failure) before an answer.
	AgentPromptCancelled AgentPromptStatus = "cancelled"
)

// Terminal reports whether the prompt can no longer be answered.
func (s AgentPromptStatus) Terminal() bool { return s != AgentPromptPending }

// AgentPromptAction is what the person did with the prompt.
type AgentPromptAction string

const (
	AgentPromptAccept  AgentPromptAction = "accept"
	AgentPromptDecline AgentPromptAction = "decline"
	AgentPromptCancel  AgentPromptAction = "cancel"
)

// Valid reports whether a is one of the three actions.
func (a AgentPromptAction) Valid() bool {
	return a == AgentPromptAccept || a == AgentPromptDecline || a == AgentPromptCancel
}

// AgentPromptOption is one choice of a question.
type AgentPromptOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AgentPromptQuestion is one question of a questions prompt. With no options it is answered in
// free text; with options the person picks one (or several, MultiSelect) and may always write
// their own answer instead.
type AgentPromptQuestion struct {
	// ID keys the answer (q1, q2, ...).
	ID string `json:"id"`
	// Header is a short label for the question (a chip), optional.
	Header      string              `json:"header,omitempty"`
	Question    string              `json:"question"`
	Options     []AgentPromptOption `json:"options,omitempty"`
	MultiSelect bool                `json:"multi_select,omitempty"`
}

// AgentPromptPayload is what a prompt shows.
type AgentPromptPayload struct {
	Kind AgentPromptKind `json:"kind"`
	// Title is a short heading; Message the longer text (an approval's details, an elicitation's
	// message).
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
	// Questions is set for a questions prompt.
	Questions []AgentPromptQuestion `json:"questions,omitempty"`
	// Schema is an elicitation form's requestedSchema, as the server sent it.
	Schema json.RawMessage `json:"schema,omitempty"`
	// URL is a URL-mode elicitation's link.
	URL string `json:"url,omitempty"`
	// ServerName names the MCP connector asking (elicitation), so the person knows who it is.
	ServerName string `json:"server_name,omitempty"`
	// ToolName is the tool that was running when an MCP server asked (elicitation).
	ToolName string `json:"tool_name,omitempty"`
}

// AgentPromptAnswer is the answer to one question: the options picked and/or the person's own
// text.
type AgentPromptAnswer struct {
	Selected []string `json:"selected,omitempty"`
	Text     string   `json:"text,omitempty"`
}

// AgentPromptResponse is what the person sent.
type AgentPromptResponse struct {
	Action AgentPromptAction `json:"action"`
	// Answers are a questions prompt's answers, by question id.
	Answers map[string]AgentPromptAnswer `json:"answers,omitempty"`
	// Content is an elicitation form's values.
	Content map[string]any `json:"content,omitempty"`
	// Note is free text sent with an approval or a decline ("not that file, use the other one").
	Note string `json:"note,omitempty"`
}

// AgentPromptView is a pending prompt as the live turn shows it (ChatTurnProgress.Prompt).
type AgentPromptView struct {
	ID        string             `json:"id"`
	Source    AgentPromptSource  `json:"source"`
	Payload   AgentPromptPayload `json:"payload"`
	ExpiresAt time.Time          `json:"expires_at"`
	// ToolCallID is the tool call the prompt belongs to (ask_user's own call, or the MCP tool an
	// elicitation interrupted), so the client can place the card next to it.
	ToolCallID string `json:"tool_call_id,omitempty"`
}
