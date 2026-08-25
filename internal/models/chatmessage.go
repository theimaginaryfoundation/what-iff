package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// MessageOrigin represents the origin of a chat message
type MessageOrigin string

// Message origin constants
const (
	MessageOriginUser      MessageOrigin = "User"
	MessageOriginAssistant MessageOrigin = "Assistant"
)

// MessageOriginFilter represents a filter for counting messages by origin
type MessageOriginFilter string

// Message origin filter constants
const (
	MessageOriginFilterAll       MessageOriginFilter = "ALL"
	MessageOriginFilterUser      MessageOriginFilter = "USER"
	MessageOriginFilterAssistant MessageOriginFilter = "ASSISTANT"
)

// MessageReadStatus represents read state for a chat message.
type MessageReadStatus string

const (
	MessageReadStatusRead   MessageReadStatus = "read"
	MessageReadStatusUnread MessageReadStatus = "unread"
)

// AdditionalContextTypeMemory tags prefetched memory snippets persisted on a user message.
const AdditionalContextTypeMemory = "MEMORY"

// AdditionalContextItem is a typed snippet (e.g. semantic-search memories) attached to a
// message so later turns can rehydrate the same model context.
type AdditionalContextItem struct {
	Type     string     `json:"type"`
	Content  string     `json:"content"`
	MemoryID *uuid.UUID `json:"memory_id,omitempty"`
	Scope    string     `json:"scope,omitempty"`
}

type ChatMessage struct {
	ID                    uuid.UUID         `json:"id"`
	ChatID                uuid.UUID         `json:"chat_id"`
	Message               string            `json:"message"`
	Origin                MessageOrigin     `json:"origin"`
	ReadStatus            MessageReadStatus `json:"read_status"`
	ResponseID            *string           `json:"-"`
	GenerationModel       string            `json:"generation_model,omitempty"`
	GenerationPersonality string            `json:"generation_personality,omitempty"`
	GenerationMoodID      *uuid.UUID        `json:"generation_mood_id,omitempty"`
	// GenerationMoodName is the display name of the mood active when this message was generated.
	GenerationMoodName string `json:"generation_mood_name,omitempty"`
	// GenerationMoodThumbnail is the base64-encoded JPEG portrait of the mood active when this
	// message was generated. Populated on load so the chat UI can render per-message portraits.
	GenerationMoodThumbnail string `json:"generation_mood_thumbnail,omitempty"`
	// GenerationExpressionKey / ImageURL / Label are set when an expression portrait was chosen for this
	// assistant message. Label comes from the expression row at load time. Reasoning is persisted by the
	// expression classifier when a portrait was chosen. Omitted when absent or when the expression row was deleted.
	GenerationExpressionKey *string `json:"generation_expression_key,omitempty"`
	// GenerationExpressionImageID is the gallery file attachment for the portrait (agent continuity / multimodal).
	GenerationExpressionImageID   *uuid.UUID              `json:"generation_expression_image_id,omitempty"`
	GenerationExpressionImageURL  *string                 `json:"generation_expression_image_url,omitempty"`
	GenerationExpressionLabel     *string                 `json:"generation_expression_label,omitempty"`
	GenerationExpressionReasoning *string                 `json:"generation_expression_reasoning,omitempty"`
	SentAt                        time.Time               `json:"sent_at"`
	Tokens                        int64                   `json:"-"`
	Attachments                   []*FileAttachment       `json:"attachments,omitempty"`
	Rituals                       []*Ritual               `json:"rituals,omitempty"`
	ToolCalls                     []*ToolCall             `json:"tool_calls,omitempty"`
	AdditionalContext             []AdditionalContextItem `json:"-"`
	// ContextBreakdown is a snapshot of the model context assembled for this assistant turn
	// (segment-by-segment token estimates + budget). Set on assistant messages when captured
	// at generation time; nil on user messages and older assistant messages.
	ContextBreakdown *modeltypes.ContextBreakdown `json:"context_breakdown,omitempty"`
	// LastErrorMessage is set when async generation failed for this user turn; cleared on successful delivery.
	LastErrorMessage *string `json:"last_error_message,omitempty"`
	// CheckpointCompletedAt is set on assistant messages after a successful checkpoint (scratchpad, memories, summary).
	CheckpointCompletedAt *time.Time `json:"checkpoint_completed_at,omitempty"`
}

type ChatMessageFilters struct {
	Origin  *MessageOrigin `json:"origin,omitempty"`
	Query   *string        `json:"query,omitempty"`
	MinDate *time.Time     `json:"min_date,omitempty"`
	MaxDate *time.Time     `json:"max_date,omitempty"`
}

type ChatMessageResponse struct {
	ID    uuid.UUID `json:"id"`
	JobID string    `json:"job_id"`
	Type  string    `json:"type"`
}

// ActiveChatMessageJobResponse is returned when a non-terminal chat_message job exists for a user turn.
type ActiveChatMessageJobResponse struct {
	JobID  uuid.UUID `json:"job_id"`
	Status JobStatus `json:"status"`
}

// ChatMessageExport is the user-facing export of a chat message
type ChatMessageExport struct {
	Message     string        `json:"message"`
	Origin      MessageOrigin `json:"origin"`
	SentAt      time.Time     `json:"sent_at"`
	Attachments []string      `json:"attachments,omitempty"`
	Rituals     []string      `json:"rituals,omitempty"`
	ToolCalls   []string      `json:"tool_calls,omitempty"`
}
