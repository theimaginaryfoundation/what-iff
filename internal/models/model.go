package models

import "github.com/google/uuid"

// SubscriptionTier values for Model.SubscriptionTier, matching the DB enum.
// These determine which plan tier unlocks free-chat access for a given model.
const (
	ModelSubscriptionTierLow    = "low"    // Dabbler plan and above
	ModelSubscriptionTierMedium = "medium" // Chatter plan and above
	ModelSubscriptionTierHigh   = "high"   // Builder plan only
	ModelSubscriptionTierUltra  = "ultra"  // Never included in free chat on any plan
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
	SubscriptionTier   string    `json:"subscription_tier"`
	Deleted            bool      `json:"deleted"`
	// IsDefault marks the application-wide default model. At most one active model
	// has this set; it is used as the default for new users when present.
	IsDefault bool `json:"is_default"`
	// ReasoningSupport is how the model takes a reasoning effort (ADR 0x026): the admin's
	// ReasoningSupportOverride when set, otherwise derived from the provider and name
	// (ReasoningSupportFor). Read-only.
	ReasoningSupport ReasoningSupport `json:"reasoning_support"`
	// ReasoningSupportOverride is the admin's override of ReasoningSupport; nil derives it.
	ReasoningSupportOverride *ReasoningSupport `json:"reasoning_support_override,omitempty"`
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
}
