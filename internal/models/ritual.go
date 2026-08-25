package models

import (
	"time"

	"github.com/google/uuid"
)

// Ritual represents a named ritual that allows users to save reusable prompt snippets for particular tasks
type Ritual struct {
	ID            uuid.UUID `json:"id"`
	PersonalityID uuid.UUID `json:"personality_id"`
	// MCPServerIDs replaces MCP edges when non-nil on create/update; omit to leave edges unchanged on update.
	MCPServerIDs *[]uuid.UUID `json:"mcp_server_ids,omitempty"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Content      string       `json:"content"`
	Hotkeys      string       `json:"hotkeys"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// RitualFilters defines filters for listing rituals
type RitualFilters struct {
	Name           *string     `json:"name,omitempty"`
	PersonalityID  *uuid.UUID  `json:"personality_id,omitempty"`
	PersonalityIDs []uuid.UUID `json:"personality_ids,omitempty"`
	GlobalOnly     *bool       `json:"global_only,omitempty"`
	HasHotkeys     *bool       `json:"has_hotkeys,omitempty"`
	Query          *string     `json:"query,omitempty"`
	Sort           *RitualSort `json:"sort,omitempty"`
	MinDate        *time.Time  `json:"min_date,omitempty"`
	MaxDate        *time.Time  `json:"max_date,omitempty"`
}

type RitualSort string

const (
	RitualSortNameAsc     RitualSort = "name_asc"
	RitualSortCreatedDesc RitualSort = "created_desc"
	RitualSortUpdatedDesc RitualSort = "updated_desc"
)
