package agent

import (
	"github.com/google/uuid"
)

// System rituals are defined in code only and do not exist in the ritual DB.
// system_ritual_binding.ritual_id stores these reserved UUIDs with no FK to ritual.
// Do not assume ritual_id in bindings has a corresponding ritual table row.

var (
	// SystemRitualIDImageGenerate triggers the custom image generation flow.
	// IMPORTANT: Not stored in DB; frontend may send it, backend strips before persisting.
	SystemRitualIDImageGenerate = uuid.MustParse("00000000-0000-0000-0000-000000000101")
)

// systemRitualDef is the single source of truth for system ritual metadata.
// To add a ritual: 1) add ID var above, 2) add one entry to systemRitualRegistry below.
type systemRitualDef struct {
	ID          uuid.UUID
	Name        string
	Description string
	Content     string
}

var systemRitualRegistry = []systemRitualDef{
	{
		ID:          SystemRitualIDImageGenerate,
		Name:        "Generate image",
		Description: "Generate an image from the conversation.",
		Content:     "",
	},
}

var systemRitualIDs map[uuid.UUID]struct{}

func init() {
	systemRitualIDs = make(map[uuid.UUID]struct{}, len(systemRitualRegistry))
	for _, r := range systemRitualRegistry {
		systemRitualIDs[r.ID] = struct{}{}
	}
	// Ensure registry and ID set stay in sync
	if len(systemRitualIDs) != len(systemRitualRegistry) {
		panic("system ritual registry has duplicate IDs")
	}
}
