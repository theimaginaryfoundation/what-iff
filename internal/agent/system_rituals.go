package agent

import (
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// IsSystemRitual reports whether the given ritual ID is a reserved system ritual.
func IsSystemRitual(id uuid.UUID) bool {
	_, ok := systemRitualIDs[id]
	return ok
}

// ListSystemRituals returns the canonical list of system rituals (without hotkeys).
// Hotkeys are overlaid per-user by the caller.
func ListSystemRituals(now time.Time) []models.Ritual {
	out := make([]models.Ritual, 0, len(systemRitualRegistry))
	for _, r := range systemRitualRegistry {
		out = append(out, models.Ritual{
			ID:            r.ID,
			PersonalityID: uuid.Nil,
			Name:          r.Name,
			Description:   r.Description,
			Content:       r.Content,
			Hotkeys:       "",
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return out
}

// SplitRituals partitions rituals into DB-backed rituals and system rituals.
//
// DB rituals are safe to persist via Ent edges; system rituals must never be persisted.
func SplitRituals(in []*models.Ritual) (db []*models.Ritual, system []*models.Ritual) {
	if len(in) == 0 {
		return nil, nil
	}
	db = make([]*models.Ritual, 0, len(in))
	system = make([]*models.Ritual, 0, len(in))
	for _, r := range in {
		if r == nil {
			continue
		}
		if IsSystemRitual(r.ID) {
			system = append(system, r)
			continue
		}
		db = append(db, r)
	}
	return db, system
}

func hasSystemRitual(rituals []*models.Ritual, id uuid.UUID) bool {
	for _, r := range rituals {
		if r != nil && r.ID == id {
			return true
		}
	}
	return false
}
