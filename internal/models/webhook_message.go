package models

import (
	"time"

	"github.com/google/uuid"
)

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
	ModelID        *uuid.UUID         `json:"model_id,omitempty"`
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

// WebhookMessage is a chat message as the webhook read routes return it.
//
// It is a deliberate subset of ChatMessage, not the same struct. Field names match ChatMessage
// for every field it keeps, so a client written against the session API reads it unchanged. What
// it leaves out is internal or heavy and not something an integration needs: tool calls, model
// reasoning, the context breakdown, per-message portrait thumbnails (base64 images), rituals,
// bookmarks, and storage details such as file keys. Keeping the shape explicit also means a field
// added to ChatMessage later does not start leaking through this surface by accident.
type WebhookMessage struct {
	ID                        uuid.UUID           `json:"id"`
	ChatID                    uuid.UUID           `json:"chat_id"`
	Message                   string              `json:"message"`
	Origin                    MessageOrigin       `json:"origin"`
	ReadStatus                MessageReadStatus   `json:"read_status"`
	SentAt                    time.Time           `json:"sent_at"`
	GenerationModel           string              `json:"generation_model,omitempty"`
	GenerationPersonality     string              `json:"generation_personality,omitempty"`
	GenerationMoodName        string              `json:"generation_mood_name,omitempty"`
	GenerationExpressionKey   *string             `json:"generation_expression_key,omitempty"`
	GenerationExpressionLabel *string             `json:"generation_expression_label,omitempty"`
	LastErrorMessage          *string             `json:"last_error_message,omitempty"`
	Attachments               []WebhookAttachment `json:"attachments,omitempty"`
}

// WebhookAttachment is attachment metadata only. File contents are not served on this surface.
type WebhookAttachment struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	FileType    string    `json:"file_type"`
	Description *string   `json:"description,omitempty"`
}

// NewWebhookMessage projects a ChatMessage onto the webhook read shape.
func NewWebhookMessage(m *ChatMessage) WebhookMessage {
	out := WebhookMessage{
		ID:                        m.ID,
		ChatID:                    m.ChatID,
		Message:                   m.Message,
		Origin:                    m.Origin,
		ReadStatus:                m.ReadStatus,
		SentAt:                    m.SentAt,
		GenerationModel:           m.GenerationModel,
		GenerationPersonality:     m.GenerationPersonality,
		GenerationMoodName:        m.GenerationMoodName,
		GenerationExpressionKey:   m.GenerationExpressionKey,
		GenerationExpressionLabel: m.GenerationExpressionLabel,
		LastErrorMessage:          m.LastErrorMessage,
	}
	for _, a := range m.Attachments {
		if a == nil {
			continue
		}
		out.Attachments = append(out.Attachments, WebhookAttachment{
			ID: a.ID, Name: a.Name, FileType: a.FileType, Description: a.Description,
		})
	}
	return out
}

// WebhookPersonality is the persona list entry on the webhook read surface: enough to choose a
// persona and filter threads by it. It deliberately carries no system prompt, scratchpad, memory
// prompts or attachments; those are the persona's private working state.
type WebhookPersonality struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	AccentColor   *string   `json:"accent_color,omitempty"`
	CoverImageURL *string   `json:"cover_image_url,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NewWebhookPersonality projects a Personality onto the webhook list shape.
func NewWebhookPersonality(p *Personality) WebhookPersonality {
	return WebhookPersonality{
		ID:            p.ID,
		Name:          p.Name,
		AccentColor:   p.AccentColor,
		CoverImageURL: p.CoverImageURL,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}
