package models

import "github.com/google/uuid"

type UserPreferences struct {
	ID                   uuid.UUID `json:"id"`
	UserID               uuid.UUID `json:"user_id"`
	DefaultModelID       uuid.UUID `json:"default_model_id"`
	DefaultPersonalityID uuid.UUID `json:"default_personality_id"`
	Theme                string    `json:"theme"`
	// FavoriteModelIDs is the user's starred models in the model picker. A nil
	// slice on update means "leave unchanged"; an empty (non-nil) slice clears
	// the list. Deliberately no omitempty: the field is documented as always
	// present in responses, so an empty list must serialize as [] rather than
	// disappearing.
	FavoriteModelIDs []string `json:"favorite_model_ids"`
	// ImageGenerationProvider is the vendor the generate_image tool and image
	// ritual call (a ModelProvider with an image generator). Responses always
	// carry it, "openai" when the user never chose; on update "" leaves it alone.
	ImageGenerationProvider string `json:"image_generation_provider"`
	// ImageGenerationProviders is read-only: the vendors this deployment can
	// generate images with, which is what the setting may be set to. Ignored on update.
	ImageGenerationProviders []string `json:"image_generation_providers,omitempty"`
	// TurnSettingValues are the account-default reasoning effort and agent loop (ADR 0x026),
	// which seed a new thread whose personality sets none. On update a nil field is left alone
	// and "" clears it.
	TurnSettingValues
}

// DefaultImageGenerationProvider is the image vendor for a user who never chose one.
const DefaultImageGenerationProvider = ModelProviderOpenAI
