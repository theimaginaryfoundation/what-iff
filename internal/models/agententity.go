package models

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Agent entity limits (ADR 0x034). An entity is read whole, so the body cap is also the most an
// entity_get call puts in context.
const (
	MaxEntityKeyRunes  = 200
	MaxEntityBodyRunes = 5000
)

var (
	// ErrInvalidEntityKey is returned for a key that is empty after normalizing, or too long.
	ErrInvalidEntityKey = errors.New("entity key must be 1-200 characters")
	// ErrEntityBodyTooLong is returned when a write would leave the body over MaxEntityBodyRunes.
	ErrEntityBodyTooLong = errors.New("entity body is over the 5000-character limit")
)

// AgentEntity is one page of the agent-written entity wiki.
type AgentEntity struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	// Key is the normalized lookup key (see NormalizeEntityKey); Title is the key as first written.
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// PinnedPersonalityID is set for an entity only that personality sees; nil is global.
	PinnedPersonalityID *uuid.UUID `json:"pinned_personality_id,omitempty"`
	// PinnedPersonalityName is filled for listings, for the scope chip.
	PinnedPersonalityName string     `json:"pinned_personality_name,omitempty"`
	LastPersonalityID     *uuid.UUID `json:"last_personality_id,omitempty"`
	LastChatID            *uuid.UUID `json:"last_chat_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	// ArchivedAt is set when an agent deleted the entity: it is archived, hidden from agents, and
	// the user can restore it. Live entities have nil.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// EntityScope names who sees an entity, in the tool results: EntityScopeUser for every
// personality of the account, EntityScopePersonality for the one it is pinned to.
const (
	EntityScopeUser        = "user"
	EntityScopePersonality = "personality"
)

// Scope is the entity's EntityScope.
func (e *AgentEntity) Scope() string {
	if e.PinnedPersonalityID != nil {
		return EntityScopePersonality
	}
	return EntityScopeUser
}

// AgentEntityWriter says who is writing an entity. ActivePersonalityID is the personality of the
// conversation (uuid.Nil for none); it decides which pinned entities are visible and, for a new
// entity, whether it is pinned (with the personality's auto_pin_memories on). ChatID is the
// conversation, or uuid.Nil for a write the user makes in the UI.
type AgentEntityWriter struct {
	ActivePersonalityID uuid.UUID
	ChatID              uuid.UUID
}

// AgentEntityFilters narrows ListAgentEntities.
type AgentEntityFilters struct {
	// Query matches key, title or body, case-insensitively.
	Query *string
	// PinnedPersonalityID: nil applies no narrowing; uuid.Nil keeps global entities only.
	PinnedPersonalityID *uuid.UUID
	// Archived lists archived entities instead of live ones.
	Archived bool
	Limit    int
	Offset   int
}

// NormalizeEntityKey lowercases key, trims it and collapses inner whitespace, so "Alex  Rivera" and
// "alex rivera" name the same entity. Matching is exact after this; fuzzier matching is later work.
func NormalizeEntityKey(key string) (string, error) {
	norm := strings.ToLower(strings.Join(strings.Fields(key), " "))
	if norm == "" || utf8.RuneCountInString(norm) > MaxEntityKeyRunes {
		return "", ErrInvalidEntityKey
	}
	return norm, nil
}

// EntityTitle is the display form of key: trimmed with inner whitespace collapsed, case kept.
func EntityTitle(key string) string {
	return strings.Join(strings.Fields(key), " ")
}

// CheckEntityBody returns ErrEntityBodyTooLong when body is over MaxEntityBodyRunes.
func CheckEntityBody(body string) error {
	if utf8.RuneCountInString(body) > MaxEntityBodyRunes {
		return ErrEntityBodyTooLong
	}
	return nil
}
