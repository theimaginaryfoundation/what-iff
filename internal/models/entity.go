package models

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Entity states.
const (
	EntityStateActive   = "active"
	EntityStateArchived = "archived"
)

// Entity is a person, place, project or other thing the agent keeps a short card about. When a
// message mentions one of its names, the card is brought into context.
type Entity struct {
	ID                  uuid.UUID  `json:"id"`
	UserID              uuid.UUID  `json:"user_id"`
	Name                string     `json:"name"`
	Type                string     `json:"type,omitempty"`
	Card                string     `json:"card"`
	Aliases             []string   `json:"aliases,omitempty"`
	Revision            int        `json:"revision"`
	State               string     `json:"state"`
	AuthorClass         string     `json:"author_class"`
	PinnedPersonalityID *uuid.UUID `json:"pinned_personality_id,omitempty"`
	CardUpdatedAt       time.Time  `json:"card_updated_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// EntityInput is a create or update. Aliases are the names besides Name; Name is always an alias.
type EntityInput struct {
	Name                string
	Type                string
	Card                string
	Aliases             []string
	PinnedPersonalityID *uuid.UUID
	AuthorClass         string
}

// EntityAliasMatch is one matchable name: what mention spotting loads per turn.
type EntityAliasMatch struct {
	EntityID  uuid.UUID
	AliasNorm string
	// Pinned is true when the entity is pinned to a personality (it wins over a same-named
	// entity every personality can see).
	Pinned bool
}

// NormalizeEntityName lowercases a name and collapses everything that isn't a letter or digit
// into single spaces, so "John  O'Neil" and "john o neil" match.
func NormalizeEntityName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	return b.String()
}
