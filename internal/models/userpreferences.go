package models

import "github.com/google/uuid"

type UserPreferences struct {
	ID                   uuid.UUID `json:"id"`
	UserID               uuid.UUID `json:"user_id"`
	DefaultModelID       uuid.UUID `json:"default_model_id"`
	DefaultPersonalityID uuid.UUID `json:"default_personality_id"`
	Theme                string    `json:"theme"`
	LastSeenAnnouncement string    `json:"last_seen_announcement"`
	// FavoriteModelIDs is the user's starred models in the model picker. A nil
	// slice on update means "leave unchanged"; an empty (non-nil) slice clears
	// the list. Deliberately no omitempty: the field is documented as always
	// present in responses, so an empty list must serialize as [] rather than
	// disappearing.
	FavoriteModelIDs []string `json:"favorite_model_ids"`
	// HiddenModelIDs is the user's hidden models — the subtractive twin of
	// FavoriteModelIDs, with the same nil-vs-empty contract: nil on update
	// means "leave unchanged", an empty non-nil slice clears the list.
	//
	// Visibility only. A hidden model is omitted from the picker but still runs
	// when named directly, so hiding one cannot silently break an existing chat
	// or a scheduled job.
	HiddenModelIDs []string `json:"hidden_model_ids"`
}
