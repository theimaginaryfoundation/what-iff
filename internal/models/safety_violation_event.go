package models

import (
	"time"

	"github.com/google/uuid"
)

type SafetyViolationProvider string

// Mirrors the model provider values in ent/schema/model.go, plus "local".
const (
	SafetyViolationProviderOpenAI    SafetyViolationProvider = "openai"
	SafetyViolationProviderAnthropic SafetyViolationProvider = "anthropic"
	SafetyViolationProviderGoogle    SafetyViolationProvider = "google"
	SafetyViolationProviderZAI       SafetyViolationProvider = "zai"
	SafetyViolationProviderMistral   SafetyViolationProvider = "mistral"
	SafetyViolationProviderDeepSeek  SafetyViolationProvider = "deepseek"
	SafetyViolationProviderQwen      SafetyViolationProvider = "qwen"
	SafetyViolationProviderXiaomi    SafetyViolationProvider = "xiaomi"
	SafetyViolationProviderLocal     SafetyViolationProvider = "local"
)

type SafetyViolationEvent struct {
	ID              uuid.UUID               `json:"id"`
	OccurredAt      time.Time               `json:"occurred_at"`
	Provider        SafetyViolationProvider `json:"provider"`
	ViolationType   string                  `json:"violation_type,omitempty"`
	ProviderCode    string                  `json:"provider_code,omitempty"`
	ProviderMessage string                  `json:"provider_message"`
	RawError        string                  `json:"raw_error"`
	UserID          uuid.UUID               `json:"user_id"`
	ChatID          *uuid.UUID              `json:"chat_id,omitempty"`
	ChatMessageID   *uuid.UUID              `json:"chat_message_id,omitempty"`
	ChatName        string                  `json:"chat_name"`
	ChatMessageText string                  `json:"chat_message_text"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type CreateSafetyViolationEventInput struct {
	OccurredAt      time.Time
	Provider        SafetyViolationProvider
	ViolationType   string
	ProviderCode    string
	ProviderMessage string
	RawError        string
	UserID          uuid.UUID
	ChatID          *uuid.UUID
	ChatMessageID   *uuid.UUID
	ChatName        string
	ChatMessageText string
}
