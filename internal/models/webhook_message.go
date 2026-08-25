package models

import "github.com/google/uuid"

// WebhookMessageMode controls how webhook chat input is handled.
type WebhookMessageMode string

const (
	WebhookMessageModeUser       WebhookMessageMode = "user"
	WebhookMessageModeAssistant  WebhookMessageMode = "assistant"
	WebhookMessageModeBackground WebhookMessageMode = "background"
)

// WebhookChatMessageRequest is the payload for webhook-triggered chat messages.
type WebhookChatMessageRequest struct {
	Mode           WebhookMessageMode `json:"mode"`
	Message        string             `json:"message"`
	ResponseID     *string            `json:"response_id,omitempty"`
	Attachments    []*FileAttachment  `json:"attachments,omitempty"`
	Rituals        []*Ritual          `json:"rituals,omitempty"`
	ClientTimezone string             `json:"client_timezone,omitempty"`
}

// WebhookChatMessageResponse is returned by webhook chat message endpoints.
type WebhookChatMessageResponse struct {
	ID    uuid.UUID          `json:"id"`
	JobID *string            `json:"job_id,omitempty"`
	Type  string             `json:"type"`
	Mode  WebhookMessageMode `json:"mode"`
}
