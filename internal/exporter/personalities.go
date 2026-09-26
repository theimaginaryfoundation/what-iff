package exporter

import (
	"bytes"
	"encoding/json"
	"path"
	"time"

	"github.com/google/uuid"
)

// PersonalityInput is a clean, allowlisted personality projection. Personalities carry no secrets,
// but we still allowlist fields explicitly so future sensitive additions are opt-in, not automatic.
type PersonalityInput struct {
	ID              uuid.UUID
	Name            string
	SystemPrompt    string
	Scratchpad      string
	AutoPinMemories bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PersonalityFile is the per-personality JSON written to the archive (and read back on import). The
// scratchpad is included here (rather than a separate file) so a personality round-trips from a
// single self-contained document.
type PersonalityFile struct {
	ID              uuid.UUID `json:"whatiff_personality_id"`
	Name            string    `json:"name"`
	SystemPrompt    string    `json:"system_prompt"`
	Scratchpad      string    `json:"scratchpad,omitempty"`
	AutoPinMemories bool      `json:"auto_pin_memories"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PersonalityDir returns the archive directory for a personality; exported so the importer can find
// personality.json files.
func PersonalityDir(id uuid.UUID) string {
	return path.Join("personalities", id.String())
}

// PersonalityFileName is the per-personality metadata file name within PersonalityDir.
const PersonalityFileName = "personality.json"

// WritePersonalities projects each personality to personalities/{id}/personality.json.
func WritePersonalities(tree Tree, ps []PersonalityInput) error {
	for _, p := range ps {
		b, err := marshalIndent(PersonalityFile{
			ID:              p.ID,
			Name:            p.Name,
			SystemPrompt:    p.SystemPrompt,
			Scratchpad:      p.Scratchpad,
			AutoPinMemories: p.AutoPinMemories,
			CreatedAt:       p.CreatedAt.UTC(),
			UpdatedAt:       p.UpdatedAt.UTC(),
		})
		if err != nil {
			return err
		}
		if err := tree.Write(path.Join(PersonalityDir(p.ID), PersonalityFileName), b); err != nil {
			return err
		}
	}
	return nil
}

// marshalIndent encodes v as indented JSON with HTML escaping off (for readable, diffable output)
// and a trailing newline.
func marshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
