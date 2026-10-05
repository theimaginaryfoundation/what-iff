package exporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/stcard"
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
	// CharacterCard is the imported character card's `data` object (nil for native personalities).
	CharacterCard map[string]any
	// OmittedCardFields are card fields import left out of the prompt (see stcard.ExportInput).
	OmittedCardFields []string
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
	// CharacterCard carries the imported card's passthrough blob so a personality that came from a
	// SillyTavern card keeps its greetings, tags and extensions through an account export/import.
	CharacterCard     map[string]any `json:"character_card,omitempty"`
	OmittedCardFields []string       `json:"omitted_card_fields,omitempty"`
}

// PersonalityDir returns the archive directory for a personality; exported so the importer can find
// personality.json files.
func PersonalityDir(id uuid.UUID) string {
	return path.Join("personalities", id.String())
}

// PersonalityFileName is the per-personality metadata file name within PersonalityDir.
const PersonalityFileName = "personality.json"

// SillyTavernCardFileName is the per-personality SillyTavern card (chara_card_v2) within
// PersonalityDir. It is a courtesy export for other tools: account import reads personality.json.
const SillyTavernCardFileName = "sillytavern.json"

// WritePersonalities projects each personality to personalities/{id}/personality.json.
func WritePersonalities(tree Tree, ps []PersonalityInput) error {
	for _, p := range ps {
		b, err := marshalIndent(PersonalityFile{
			ID:                p.ID,
			Name:              p.Name,
			SystemPrompt:      p.SystemPrompt,
			Scratchpad:        p.Scratchpad,
			AutoPinMemories:   p.AutoPinMemories,
			CreatedAt:         p.CreatedAt.UTC(),
			UpdatedAt:         p.UpdatedAt.UTC(),
			CharacterCard:     p.CharacterCard,
			OmittedCardFields: p.OmittedCardFields,
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

// WriteSillyTavernCards writes each personality as a SillyTavern chara_card_v2 document to
// personalities/{id}/sillytavern.json. creator is the account's username, stamped on cards that
// did not come with an author of their own.
func WriteSillyTavernCards(tree Tree, ps []PersonalityInput, creator string) error {
	for _, p := range ps {
		b, err := stcard.Export(stcard.ExportInput{
			Name:          p.Name,
			SystemPrompt:  p.SystemPrompt,
			Creator:       creator,
			Card:          p.CharacterCard,
			OmittedFields: p.OmittedCardFields,
		})
		if err != nil {
			return fmt.Errorf("personality %s: %w", p.ID, err)
		}
		if err := tree.Write(path.Join(PersonalityDir(p.ID), SillyTavernCardFileName), b); err != nil {
			return err
		}
	}
	return nil
}
