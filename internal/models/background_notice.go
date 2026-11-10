package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// BackgroundStep names background work on a chat that can fail or fall back.
type BackgroundStep string

const (
	// A checkpoint's steps, on the memory role.
	BackgroundStepScratchpad BackgroundStep = "scratchpad"
	BackgroundStepMemory     BackgroundStep = "memory"
	BackgroundStepSummary    BackgroundStep = "summary"
	// A turn's own calls, on the simple role. Each is its own step, so one succeeding never
	// clears another's failure; a client shows a role's notices together.
	BackgroundStepMemoryQuery    BackgroundStep = "memory_query"
	BackgroundStepChatName       BackgroundStep = "chat_name"
	BackgroundStepMoodSelect     BackgroundStep = "mood_select"
	BackgroundStepExpressionPick BackgroundStep = "expression_pick"
)

// backgroundFallbackPrefix starts the step of a role passing over a chosen model.
const backgroundFallbackPrefix = "fallback_"

// BackgroundFallbackStep is the step for a utility role passing over a chosen model it
// couldn't use.
func BackgroundFallbackStep(role UtilityRole) BackgroundStep {
	return BackgroundStep(backgroundFallbackPrefix + string(role))
}

// Role is the utility role a step runs on, or "" for a step this server doesn't know.
func (s BackgroundStep) Role() UtilityRole {
	switch s {
	case BackgroundStepScratchpad, BackgroundStepMemory, BackgroundStepSummary:
		return UtilityRoleMemory
	case BackgroundStepMemoryQuery, BackgroundStepChatName, BackgroundStepMoodSelect, BackgroundStepExpressionPick:
		return UtilityRoleSimple
	}
	if role := UtilityRole(strings.TrimPrefix(string(s), backgroundFallbackPrefix)); role != UtilityRole(s) && role.Valid() {
		return role
	}
	return ""
}

// Why a background step didn't go as it should.
const (
	// BackgroundNoticeProviderNotConfigured: the step needed a provider the account has no key
	// for (Provider names it).
	BackgroundNoticeProviderNotConfigured = ErrCodeProviderNotConfigured
	// BackgroundNoticeProviderError: the provider was asked and failed.
	BackgroundNoticeProviderError = "provider_error"
	// BackgroundNoticeModelUnavailable: the model chosen for a role couldn't be used. On a
	// fallback step the role ran on its next choice; on any other step, it had none, so the step
	// didn't run.
	BackgroundNoticeModelUnavailable = "model_unavailable"
	// BackgroundNoticeModelNotSet: no model is chosen for the step's role, so it didn't run.
	BackgroundNoticeModelNotSet = "model_not_set"
)

// BackgroundNotice is background work on a chat that failed or fell back, shown to the
// account until the same step succeeds. It states the fact; the client says what to do.
type BackgroundNotice struct {
	ID     uuid.UUID      `json:"id"`
	ChatID uuid.UUID      `json:"chat_id"`
	Step   BackgroundStep `json:"step"`
	// Role is the utility role the step runs on (Step.Role), empty for a step this server
	// doesn't know. It isn't stored.
	Role     UtilityRole `json:"role,omitempty"`
	Code     string      `json:"code"`
	Provider string      `json:"provider,omitempty"`
	Model    string      `json:"model,omitempty"`
	// Count is how many times it has happened since it was last cleared.
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BackgroundOutcome is what a run of background steps on a chat found: the steps that failed
// or fell back, and the steps that succeeded, whose notices it clears. It passes between the
// agent and the datastore only and is never serialized; the API returns BackgroundNotice.
type BackgroundOutcome struct {
	Failed    []BackgroundNotice
	Succeeded []BackgroundStep
}
