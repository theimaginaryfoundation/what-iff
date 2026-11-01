package models

import (
	"time"

	"github.com/google/uuid"
)

// Ritual represents a named ritual that allows users to save reusable prompt snippets for particular tasks.
// It is also an Agent Skills bundle's record (ADR 0x035): the SKILL.md fields live here as columns and
// SKILL.md itself is rendered from them.
//
// The pointer fields follow MCPServerIDs' rule: in a request, omitting one leaves the stored value
// unchanged on update and takes the default on create; in a response they are always set.
type Ritual struct {
	ID            uuid.UUID `json:"id"`
	PersonalityID uuid.UUID `json:"personality_id"`
	// MCPServerIDs replaces MCP edges when non-nil on create/update; omit to leave edges unchanged on update.
	MCPServerIDs *[]uuid.UUID `json:"mcp_server_ids,omitempty"`
	// Slug is the skill's name in the Agent Skills sense (1-64 chars of [a-z0-9-], unique per owner).
	// Empty in a create request derives it from Name; empty in an update keeps the stored slug.
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Hotkeys     string `json:"hotkeys"`
	// License and Compatibility are the SKILL.md fields of the same names (free text, 500 chars).
	License       *string `json:"license,omitempty"`
	Compatibility *string `json:"compatibility,omitempty"`
	// Metadata is SKILL.md metadata: string-to-string client extensions. Our own keys start with "whatiff-".
	Metadata *map[string]string `json:"metadata,omitempty"`
	// AllowedTools is SKILL.md allowed-tools: the tools loaded with the skill, never a permission grant.
	AllowedTools *[]string `json:"allowed_tools,omitempty"`
	// DisableModelInvocation keeps the skill out of the model's index; only the user, a mode or a job can activate it.
	DisableModelInvocation *bool `json:"disable_model_invocation,omitempty"`
	// UserInvocable false hides the skill from the /skill picker and hotkeys; the model may still load it.
	UserInvocable *bool     `json:"user_invocable,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// LicenseText returns the license, or "" when unset.
func (r *Ritual) LicenseText() string { return derefString(r.License) }

// CompatibilityText returns the compatibility note, or "" when unset.
func (r *Ritual) CompatibilityText() string { return derefString(r.Compatibility) }

// MetadataMap returns the metadata, never nil.
func (r *Ritual) MetadataMap() map[string]string {
	if r.Metadata == nil || *r.Metadata == nil {
		return map[string]string{}
	}
	return *r.Metadata
}

// AllowedToolList returns the allowed tools, never nil.
func (r *Ritual) AllowedToolList() []string {
	if r.AllowedTools == nil || *r.AllowedTools == nil {
		return []string{}
	}
	return *r.AllowedTools
}

// IsModelInvocable reports whether the model may list and load the skill itself.
func (r *Ritual) IsModelInvocable() bool {
	return r.DisableModelInvocation == nil || !*r.DisableModelInvocation
}

// IsUserInvocable reports whether the /skill picker and hotkeys offer the skill (the default).
func (r *Ritual) IsUserInvocable() bool { return r.UserInvocable == nil || *r.UserInvocable }

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RitualFilters defines filters for listing rituals
type RitualFilters struct {
	Name           *string     `json:"name,omitempty"`
	Slug           *string     `json:"slug,omitempty"`
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
