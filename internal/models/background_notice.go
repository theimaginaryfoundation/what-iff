package models

import (
	"time"

	"github.com/google/uuid"
)

// BackgroundStep names background work on a chat that can fail or fall back.
type BackgroundStep string

const (
	BackgroundStepScratchpad BackgroundStep = "scratchpad"
	BackgroundStepMemory     BackgroundStep = "memory"
	BackgroundStepSummary    BackgroundStep = "summary"
)

// BackgroundFallbackStep is the step for a utility role passing over a chosen model it
// couldn't use.
func BackgroundFallbackStep(role UtilityRole) BackgroundStep {
	return BackgroundStep("fallback_" + string(role))
}

// Why a background step didn't go as it should.
const (
	// BackgroundNoticeProviderNotConfigured: the step needed a provider the account has no key
	// for (Provider names it).
	BackgroundNoticeProviderNotConfigured = ErrCodeProviderNotConfigured
	// BackgroundNoticeProviderError: the provider was asked and failed.
	BackgroundNoticeProviderError = "provider_error"
	// BackgroundNoticeModelUnavailable: the model chosen for a role couldn't be used, so the
	// role ran on its next choice or its default.
	BackgroundNoticeModelUnavailable = "model_unavailable"
)

// BackgroundNotice is background work on a chat that failed or fell back, shown to the
// account until the same step succeeds. It states the fact; the client says what to do.
type BackgroundNotice struct {
	ID       uuid.UUID      `json:"id"`
	ChatID   uuid.UUID      `json:"chat_id"`
	Step     BackgroundStep `json:"step"`
	Code     string         `json:"code"`
	Provider string         `json:"provider,omitempty"`
	Model    string         `json:"model,omitempty"`
	// Count is how many times it has happened since it was last cleared.
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BackgroundOutcome is what a run of background steps on a chat found: the steps that failed
// or fell back, and the steps that succeeded, whose notices it clears.
type BackgroundOutcome struct {
	Failed    []BackgroundNotice
	Succeeded []BackgroundStep
}
