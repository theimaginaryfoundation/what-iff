package models

import "github.com/google/uuid"

// SubscriptionTier values for Model.SubscriptionTier, matching the DB enum,
// from lowest to highest. A tier is a billing grade a linked meter may use;
// without one (metering.Linked) it has no effect and the API omits it.
const (
	ModelSubscriptionTierLow    = "low"
	ModelSubscriptionTierMedium = "medium"
	ModelSubscriptionTierHigh   = "high"
	ModelSubscriptionTierUltra  = "ultra"
)

type Model struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	DisplayName        string    `json:"display_name"`
	Description        string    `json:"description"`
	Provider           string    `json:"provider"`
	ToolSupport        bool      `json:"tool_support"`
	VisionSupport      bool      `json:"vision_support"`
	BaseCreditsPerSlab int64     `json:"base_credits_per_slab"`
	SubscriptionTier   string    `json:"subscription_tier,omitempty"`
	Deleted            bool      `json:"deleted"`
	// IsDefault marks the application-wide default model. At most one active model
	// has this set; it is used as the default for new users when present.
	IsDefault bool `json:"is_default"`
	// ConnectionID is the provider connection serving the model (ADR 0x027), or
	// nil for the deployment's own catalog.
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
	// ConnectionName is that connection's name, as the account named it, where the
	// list it came from loaded it (an account's model list does); empty otherwise.
	ConnectionName string `json:"connection_name,omitempty"`
	// ReasoningSupport is how the model takes a reasoning effort (ADR 0x026): the admin's
	// ReasoningSupportOverride when set, otherwise derived from the provider and name
	// (ReasoningSupportFor). Read-only.
	ReasoningSupport ReasoningSupport `json:"reasoning_support"`
	// ReasoningSupportOverride is the admin's override of ReasoningSupport; nil derives it.
	ReasoningSupportOverride *ReasoningSupport `json:"reasoning_support_override,omitempty"`
	// Kind is what the model does (ADR 0x052); empty reads as chat.
	Kind ModelKind `json:"kind"`
	// SpeechVoices are the voices a speech model offers; empty for other kinds.
	SpeechVoices []SpeechVoice `json:"speech_voices,omitempty"`
}

type CreateModelRequest struct {
	Name               string `json:"name"`
	DisplayName        string `json:"display_name"`
	Description        string `json:"description"`
	Provider           string `json:"provider"`
	ToolSupport        bool   `json:"tool_support"`
	VisionSupport      bool   `json:"vision_support"`
	BaseCreditsPerSlab int64  `json:"base_credits_per_slab"`
	SubscriptionTier   string `json:"subscription_tier"`
	// Kind is chat (the default), speech or transcription (ADR 0x052). A model the
	// known speech models table knows takes the table's kind.
	Kind ModelKind `json:"kind,omitempty"`
	// SpeechVoices are the voices a speech model offers; nil takes the table's.
	SpeechVoices []SpeechVoice `json:"speech_voices,omitempty"`
}

type UpdateModelRequest struct {
	Name               string  `json:"name,omitempty"`
	DisplayName        string  `json:"display_name,omitempty"`
	Description        string  `json:"description,omitempty"`
	Provider           string  `json:"provider,omitempty"`
	ToolSupport        *bool   `json:"tool_support,omitempty"`
	VisionSupport      *bool   `json:"vision_support,omitempty"`
	BaseCreditsPerSlab *int64  `json:"base_credits_per_slab,omitempty"`
	SubscriptionTier   *string `json:"subscription_tier,omitempty"`
	// ReasoningSupportOverride sets the admin override ("none"|"optional"|"toggle"|"always");
	// "" clears it (derive from the name again); nil leaves it.
	ReasoningSupportOverride *string `json:"reasoning_support_override,omitempty"`
	// SpeechVoices replaces a speech model's offered voices; nil leaves them. A
	// model's kind does not change.
	SpeechVoices []SpeechVoice `json:"speech_voices,omitempty"`
}

// EffectiveKind is the model's kind, reading an empty one as chat.
func (m *Model) EffectiveKind() ModelKind {
	if m == nil || m.Kind == "" {
		return ModelKindChat
	}
	return m.Kind
}
