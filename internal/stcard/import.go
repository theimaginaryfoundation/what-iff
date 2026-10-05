package stcard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ImportOptions tunes Parse.
type ImportOptions struct {
	// MaxPromptUnits caps the flattened prompt, in UTF-16 code units (matching the editor's limit).
	// Zero means unlimited. A character book that would push the prompt over it is imported as
	// files instead of being flattened; a prompt too long even without the book is an error.
	MaxPromptUnits int
}

// LoreEntry is one character-book entry that should become its own file attachment.
type LoreEntry struct {
	// Name is the entry's display name (its comment, else its keys).
	Name string `json:"name"`
	// FileName is a filesystem-safe, batch-unique name with an extension the file pipeline accepts.
	FileName string `json:"file_name"`
	// Keys are the entry's trigger keywords, useful as the attachment description.
	Keys    []string `json:"keys"`
	Content string   `json:"content"`
}

// Imported is the result of parsing a card.
type Imported struct {
	// Name is the card's character name, unmodified (the caller makes it unique).
	Name         string
	SystemPrompt string
	// LoreFiles is non-empty only when the character book was not flattened into the prompt.
	LoreFiles []LoreEntry
	// OmittedFields names the card fields (`scenario`, `personality`) left out of the prompt to
	// fit the length limit. They stay in Data, and export restores them.
	OmittedFields []string
	// Data is the card's entire `data` object, verbatim, for the passthrough blob.
	Data     map[string]any
	Warnings []string
}

// Parse reads a SillyTavern v2 (or v3) card, either as JSON or as a PNG with the card embedded in
// it. See the package comment for the mapping rules.
func Parse(raw []byte, opts ImportOptions) (*Imported, error) {
	if IsPNG(raw) {
		card, err := ExtractFromPNG(raw)
		if err != nil {
			return nil, err
		}
		raw = card
	}

	var env struct {
		Spec        string         `json:"spec"`
		SpecVersion string         `json:"spec_version"`
		Data        map[string]any `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep numbers exact through the passthrough blob
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCard, err)
	}

	var warnings []string
	switch env.Spec {
	case SpecV2:
		if !strings.HasPrefix(env.SpecVersion, "2.") {
			warnings = append(warnings, fmt.Sprintf("Unexpected spec_version %q for %s; imported on a best-effort basis.", env.SpecVersion, SpecV2))
		}
	case SpecV3:
		if !strings.HasPrefix(env.SpecVersion, "3.") {
			warnings = append(warnings, fmt.Sprintf("Unexpected spec_version %q for %s; imported on a best-effort basis.", env.SpecVersion, SpecV3))
		}
	case "":
		return nil, fmt.Errorf("%w: no \"spec\" field (only %s and %s cards are supported)", ErrUnsupportedSpec, SpecV2, SpecV3)
	default:
		return nil, fmt.Errorf("%w: %q (only %s and %s cards are supported)", ErrUnsupportedSpec, env.Spec, SpecV2, SpecV3)
	}
	if env.Data == nil {
		return nil, fmt.Errorf("%w: missing \"data\" object", ErrInvalidCard)
	}

	data := env.Data
	segs := Segments{
		SystemPrompt: str(data, "system_prompt"),
		Description:  str(data, "description"),
		Personality:  str(data, "personality"),
		Scenario:     str(data, "scenario"),
	}

	// Over the limit: shed the ancillary fields, scenario first, rather than refuse the card.
	// The book is not shed (it can always become files), so it is judged separately below.
	var omitted []string
	if full := Compose(segs); !fits(full, opts.MaxPromptUnits) {
		for _, shed := range []struct {
			field string
			clear func(*Segments)
		}{
			{"scenario", func(s *Segments) { s.Scenario = "" }},
			{"personality", func(s *Segments) { s.Personality = "" }},
		} {
			if fits(Compose(segs), opts.MaxPromptUnits) {
				break
			}
			shed.clear(&segs)
			omitted = append(omitted, shed.field)
		}
		if p := Compose(segs); p == "" || !fits(p, opts.MaxPromptUnits) {
			detail := ""
			if len(omitted) > 0 {
				detail = ", even without the " + strings.Join(omitted, " and ")
			}
			return nil, fmt.Errorf("%w: %s characters against a limit of %s%s", ErrPromptTooLong, groupDigits(utf16Len(full)), groupDigits(opts.MaxPromptUnits), detail)
		}
		warnings = append(warnings, fmt.Sprintf("The prompt was over the %s character limit, so the card's %s %s left out of it. %s kept with the card and come back when you export it.",
			groupDigits(opts.MaxPromptUnits), strings.Join(omitted, " and "), plural(len(omitted), "field was", "fields were"), plural(len(omitted), "It is", "They are")))
	}

	entries, disabled := loreEntries(data)
	if disabled > 0 {
		warnings = append(warnings, fmt.Sprintf("Skipped %d disabled character-book entr%s.", disabled, plural(disabled, "y", "ies")))
	}

	var files []LoreEntry
	switch {
	case len(entries) == 0:
	case len(entries) <= LoreFlattenMax:
		withLore := segs
		withLore.Lore = flattenLore(entries)
		if fits(Compose(withLore), opts.MaxPromptUnits) {
			segs = withLore
		} else {
			files = loreFiles(entries)
			warnings = append(warnings, "The character book is too long to fit in the prompt, so its entries were imported as files.")
		}
	default:
		files = loreFiles(entries)
	}

	prompt := Compose(segs)
	if prompt == "" {
		return nil, ErrNoContent
	}

	name := str(data, "name")
	if name == "" {
		name = "Imported character"
		warnings = append(warnings, "The card has no name; used a placeholder.")
	}

	return &Imported{
		Name:          name,
		SystemPrompt:  prompt,
		LoreFiles:     files,
		OmittedFields: omitted,
		Data:          data,
		Warnings:      warnings,
	}, nil
}

type rawLore struct {
	name    string
	keys    []string
	content string
	order   float64
}

// flattenLore renders entries as sub-sections of the [Lore] segment.
func flattenLore(entries []rawLore) string {
	blocks := make([]string, 0, len(entries))
	for _, e := range entries {
		blocks = append(blocks, "#### "+e.name+"\n"+e.content)
	}
	return strings.Join(blocks, "\n\n")
}

// loreEntries returns the enabled, non-empty character-book entries in insertion order, and how
// many were skipped for being disabled.
func loreEntries(data map[string]any) (entries []rawLore, disabled int) {
	book, ok := data["character_book"].(map[string]any)
	if !ok {
		return nil, 0
	}
	list, _ := book["entries"].([]any)
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if enabled, ok := m["enabled"].(bool); ok && !enabled {
			disabled++
			continue
		}
		content := str(m, "content")
		if content == "" {
			continue
		}
		keys := strs(m["keys"])
		name := firstNonEmpty(str(m, "comment"), str(m, "name"), strings.Join(keys, ", "), fmt.Sprintf("Lore entry %d", i+1))
		entries = append(entries, rawLore{name: name, keys: keys, content: content, order: num(m["insertion_order"])})
	}
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].order < entries[b].order })
	return entries, disabled
}

// loreFiles turns entries into attachments with safe, unique file names.
func loreFiles(entries []rawLore) []LoreEntry {
	used := make(map[string]bool, len(entries))
	out := make([]LoreEntry, 0, len(entries))
	for _, e := range entries {
		stem := FileStem(e.name)
		candidate, n := stem, 1
		for used[strings.ToLower(candidate)] {
			n++
			candidate = fmt.Sprintf("%s (%d)", stem, n)
		}
		used[strings.ToLower(candidate)] = true
		out = append(out, LoreEntry{Name: e.name, FileName: candidate + ".md", Keys: e.keys, Content: e.content})
	}
	return out
}

// FileStem makes a display name safe to use as a file name (without extension); it is never empty.
func FileStem(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	stem := strings.Join(strings.Fields(b.String()), " ")
	if runes := []rune(stem); len(runes) > 80 {
		stem = string(runes[:80])
	}
	stem = strings.Trim(stem, ". ")
	if stem == "" {
		return "lore"
	}
	return stem
}

// groupDigits formats n with thousands separators (25000 -> "25,000").
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func fits(s string, max int) bool { return max <= 0 || utf16Len(s) <= max }

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// str returns the trimmed string at key, or "" when absent or not a string.
func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

// strs returns the non-empty trimmed strings of a JSON array.
func strs(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// num returns v as a float64 when it is a JSON number, else 0.
func num(v any) float64 {
	switch n := v.(type) {
	case json.Number:
		f, _ := n.Float64()
		return f
	case float64:
		return n
	}
	return 0
}
