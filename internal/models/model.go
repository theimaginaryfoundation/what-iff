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
	BaseCreditsPerSlab int64     `json:"base_credits_per_slab"`
	SubscriptionTier   string    `json:"subscription_tier"`
	Deleted            bool      `json:"deleted"`
	// IsDefault marks the application-wide default model. At most one active model
	// has this set; it is used as the default for new users when present.
	IsDefault bool `json:"is_default"`
}

type CreateModelRequest struct {
	Name               string `json:"name"`
	DisplayName        string `json:"display_name"`
	Description        string `json:"description"`
	Provider           string `json:"provider"`
	ToolSupport        bool   `json:"tool_support"`
	BaseCreditsPerSlab int64  `json:"base_credits_per_slab"`
	SubscriptionTier   string `json:"subscription_tier"`
}

type UpdateModelRequest struct {
	Name               string  `json:"name,omitempty"`
	DisplayName        string  `json:"display_name,omitempty"`
	Description        string  `json:"description,omitempty"`
	Provider           string  `json:"provider,omitempty"`
	ToolSupport        *bool   `json:"tool_support,omitempty"`
	BaseCreditsPerSlab *int64  `json:"base_credits_per_slab,omitempty"`
	SubscriptionTier   *string `json:"subscription_tier,omitempty"`
}
