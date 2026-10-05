// Package stcard converts between WhatIff personalities and SillyTavern character cards
// (chara_card_v2; chara_card_v3 is accepted on import).
//
// The mapping is deliberately lossy in behaviour and lossless in data: a card's prompt-shaped
// fields are flattened into the single system prompt WhatIff understands, while the card's whole
// `data` object is kept verbatim as an opaque passthrough blob so export can hand back everything
// we do not model (greetings, tags, extensions, the character book, ...).
package stcard

import (
	"errors"
	"strings"
)

const (
	// SpecV2 and SpecV3 are the `spec` strings we recognise on import. We only ever export v2.
	SpecV2 = "chara_card_v2"
	SpecV3 = "chara_card_v3"
	// SpecVersionV2 is the `spec_version` we stamp on exported cards.
	SpecVersionV2 = "2.0"
)

// Structural headers the importer writes into the system prompt and the exporter splits on. They
// are matched as a whole line, exactly, so the splitter stays deterministic.
const (
	HeaderDescription = "### [Description]"
	HeaderPersonality = "### [Personality]"
	HeaderScenario    = "### [Scenario]"
	HeaderLore        = "### [Lore]"
)

// HeaderWarning is appended to the exported card's creator_notes whenever the export relied on the
// headers above, so a card author editing the prompt in another tool knows what not to break.
const HeaderWarning = "Segment headers (### [Description] etc.) are structural — if you remove or edit them, everything exports as system_prompt."

// LoreFlattenMax is the largest number of character-book entries folded into the prompt under a
// [Lore] header; a bigger book becomes one file attachment per entry instead.
const LoreFlattenMax = 5

var (
	// ErrInvalidCard means the payload is not a card at all (bad JSON, no data object).
	ErrInvalidCard = errors.New("not a valid character card")
	// ErrUnsupportedSpec means the card declares a spec we do not import.
	ErrUnsupportedSpec = errors.New("unsupported character card spec")
	// ErrNoContent means the card has nothing that can become a system prompt.
	ErrNoContent = errors.New("character card has no prompt content")
	// ErrPromptTooLong means the flattened prompt exceeds ImportOptions.MaxPromptUnits.
	ErrPromptTooLong = errors.New("character card prompt is too long")
)

// Segments is a system prompt broken into the parts a card keeps separately.
type Segments struct {
	SystemPrompt string
	Description  string
	Personality  string
	Scenario     string
	Lore         string
}

// headers lists the structural headers in canonical output order.
var headers = []struct {
	line string
	get  func(*Segments) *string
}{
	{HeaderDescription, func(s *Segments) *string { return &s.Description }},
	{HeaderPersonality, func(s *Segments) *string { return &s.Personality }},
	{HeaderScenario, func(s *Segments) *string { return &s.Scenario }},
	{HeaderLore, func(s *Segments) *string { return &s.Lore }},
}

// Compose joins the non-empty segments into one prompt: the bare system prompt verbatim, then each
// remaining segment under its header. A card with only a system prompt gets no headers at all.
func Compose(s Segments) string {
	parts := make([]string, 0, 5)
	if p := strings.TrimSpace(s.SystemPrompt); p != "" {
		parts = append(parts, p)
	}
	for _, h := range headers {
		body := strings.TrimSpace(*h.get(&s))
		if body == "" {
			continue
		}
		parts = append(parts, h.line+"\n"+body)
	}
	return strings.Join(parts, "\n\n")
}

// Split is the inverse of Compose. Everything before the first header is the bare system prompt;
// each header owns the text up to the next one. The bool reports whether any structural header was
// found. When headers are absent, or one appears twice (a hand edit we cannot interpret), the
// whole prompt comes back as SystemPrompt and the bool is false.
func Split(prompt string) (Segments, bool) {
	whole := Segments{SystemPrompt: strings.TrimSpace(prompt)}

	buf := make(map[string][]string, len(headers)+1)
	seen := make(map[string]bool, len(headers))
	current := ""
	for _, line := range strings.Split(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n") {
		if h, ok := headerFor(line); ok {
			if seen[h] {
				return whole, false
			}
			seen[h] = true
			current = h
			continue
		}
		buf[current] = append(buf[current], line)
	}
	if len(seen) == 0 {
		return whole, false
	}

	var out Segments
	out.SystemPrompt = strings.TrimSpace(strings.Join(buf[""], "\n"))
	for _, h := range headers {
		*h.get(&out) = strings.TrimSpace(strings.Join(buf[h.line], "\n"))
	}
	return out, true
}

// headerFor reports whether line is exactly one of the structural headers (trailing whitespace
// tolerated, nothing else).
func headerFor(line string) (string, bool) {
	line = strings.TrimRight(line, " \t")
	for _, h := range headers {
		if line == h.line {
			return h.line, true
		}
	}
	return "", false
}
