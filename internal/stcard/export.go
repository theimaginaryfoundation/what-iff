package stcard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Card is a chara_card_v2 document.
type Card struct {
	Spec        string         `json:"spec"`
	SpecVersion string         `json:"spec_version"`
	Data        map[string]any `json:"data"`
}

// ExportInput is the personality state an exported card is built from.
type ExportInput struct {
	Name         string
	SystemPrompt string
	// Creator is the personality owner's WhatIff username. It is only used when the original card
	// had no creator of its own, so re-exporting someone else's card never rewrites its author.
	Creator string
	// Card is the passthrough blob stored at import (the original card `data`); nil for
	// personalities that were not imported from a card.
	Card map[string]any
	// OmittedFields are card fields (`scenario`, `personality`) import left out of the prompt to
	// fit the length limit. While the prompt still has no section for one, the blob's original
	// text is exported instead of an empty string.
	OmittedFields []string
}

// Build reconstructs a card: the passthrough blob supplies everything we do not model, then the
// personality's current name and prompt are laid over it.
func Build(in ExportInput) (Card, error) {
	data, err := cloneMap(in.Card)
	if err != nil {
		return Card{}, fmt.Errorf("copy card data: %w", err)
	}

	segs, split := Split(in.SystemPrompt)

	// The character book is carried by the blob. When the prompt holds a [Lore] section and the
	// blob has the book, the section is just the flattened copy and is dropped; with no book to
	// hold it, the section stays in the prompt rather than silently vanishing.
	if _, hasBook := data["character_book"].(map[string]any); segs.Lore != "" && !hasBook {
		segs.SystemPrompt = Compose(Segments{SystemPrompt: segs.SystemPrompt, Lore: segs.Lore})
	}

	original := make(map[string]any, len(in.OmittedFields))
	for _, f := range in.OmittedFields {
		original[f] = data[f]
	}

	data["name"] = strings.TrimSpace(in.Name)
	data["system_prompt"] = segs.SystemPrompt
	data["description"] = segs.Description
	data["personality"] = segs.Personality
	data["scenario"] = segs.Scenario

	for f, v := range original {
		if asString(data[f]) == "" && v != nil {
			data[f] = v
		}
	}

	if str(data, "creator") == "" {
		data["creator"] = in.Creator
	}
	notes := strings.TrimSpace(strings.ReplaceAll(asString(data["creator_notes"]), HeaderWarning, ""))
	if split {
		notes = strings.TrimSpace(notes + "\n\n" + HeaderWarning)
	}
	data["creator_notes"] = notes

	// Fields the v2 spec requires to exist; keep whatever the blob had.
	setDefault(data, "first_mes", "")
	setDefault(data, "mes_example", "")
	setDefault(data, "post_history_instructions", "")
	setDefault(data, "character_version", "")
	setDefault(data, "alternate_greetings", []any{})
	setDefault(data, "tags", []any{})
	setDefault(data, "extensions", map[string]any{})

	return Card{Spec: SpecV2, SpecVersion: SpecVersionV2, Data: data}, nil
}

// Marshal renders a card as indented JSON with a trailing newline and no HTML escaping.
func Marshal(c Card) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Export is Build followed by Marshal.
func Export(in ExportInput) ([]byte, error) {
	c, err := Build(in)
	if err != nil {
		return nil, err
	}
	return Marshal(c)
}

// cloneMap deep-copies a JSON object so Build never mutates the caller's blob.
func cloneMap(m map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if len(m) == 0 {
		return out, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func setDefault(m map[string]any, key string, def any) {
	if v, ok := m[key]; !ok || v == nil {
		m[key] = def
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
