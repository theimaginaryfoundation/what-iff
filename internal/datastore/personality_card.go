package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// GetPersonalityCard returns the character-card passthrough blob the personality was imported
// from, or nil when it has none. The blob lives in its own table so ordinary personality reads
// (every message send, every list page) never pay for it; only card exports call this.
// ErrPersonalityNotFound is returned when the personality does not exist or is not the user's.
func (d *Datastore) GetPersonalityCard(ctx context.Context, userID, personalityID uuid.UUID) (*models.PersonalityCard, error) {
	row, err := d.dbClient.Personality.Query().
		Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
		WithCard().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrPersonalityNotFound
	}
	if err != nil {
		return nil, err
	}
	if row.Edges.Card == nil {
		return nil, nil
	}
	return &models.PersonalityCard{Data: row.Edges.Card.Data, OmittedFields: row.Edges.Card.OmittedFields}, nil
}
