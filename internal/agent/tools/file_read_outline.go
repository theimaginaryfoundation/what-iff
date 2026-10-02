package tools

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// buildOutline returns a file's structure with 1-based line numbers so the agent can plan a
// ranged read instead of paging through the whole file. It is deterministic and cheap: Markdown
// headings, top-level JSON or YAML keys, CSV/TSV columns, or (for anything else) the first
// non-empty lines.
func buildOutline(fa *models.FileAttachment, lines []string) ([]outlineEntry, string) {
	switch outlineKind(fa) {
	case "markdown":
		if entries := markdownOutline(lines); len(entries) > 0 {
			return capOutline(entries)
		}
	case "json":
		if entries, note, ok := jsonOutline(lines); ok {
			return entries, note
		}
	case "yaml":
		if entries := yamlOutline(lines); len(entries) > 0 {
			return capOutline(entries)
		}
	case "csv":
		if entries, note, ok := delimitedOutline(lines, ','); ok {
			return entries, note
		}
	case "tsv":
		if entries, note, ok := delimitedOutline(lines, '\t'); ok {
			return entries, note
		}
	}
	return firstLinesOutline(lines)
}

func outlineKind(fa *models.FileAttachment) string {
	ext := strings.ToLower(filepath.Ext(fa.Name))
	ct := strings.ToLower(fa.FileType)
	switch {
	case ext == ".md" || ext == ".markdown" || strings.Contains(ct, "markdown"):
		return "markdown"
	case ext == ".json" || strings.Contains(ct, "json"):
		return "json"
	case ext == ".yaml" || ext == ".yml" || strings.Contains(ct, "yaml"):
		return "yaml"
	case ext == ".csv" || strings.Contains(ct, "csv"):
		return "csv"
	case ext == ".tsv" || strings.Contains(ct, "tab-separated"):
		return "tsv"
	}
	return ""
}

func capOutline(entries []outlineEntry) ([]outlineEntry, string) {
	if len(entries) > outlineMaxEntries {
		return entries[:outlineMaxEntries], fmt.Sprintf("Showing the first %d of %d entries.", outlineMaxEntries, len(entries))
	}
	return entries, ""
}

// markdownOutline lists ATX headings ("# Title"), ignoring lines inside fenced code blocks.
func markdownOutline(lines []string) []outlineEntry {
	var out []outlineEntry
	inFence := false
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || len(line)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
			continue
		}
		text := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(trimmed[level:]), "#"))
		if text == "" {
			continue
		}
		out = append(out, outlineEntry{Line: i + 1, Level: level, Text: text})
	}
	return out
}

// jsonOutline lists a JSON object's top-level keys with the line each starts on and its value's
// kind, or summarizes a top-level array. Invalid JSON falls through to the generic outline.
func jsonOutline(lines []string) ([]outlineEntry, string, bool) {
	data := []byte(strings.Join(lines, "\n"))
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, "", false
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, "", false
		}
		text := fmt.Sprintf("array of %d items", len(items))
		if len(items) > 0 {
			text += ", first item is " + jsonKind(items[0])
		}
		return []outlineEntry{{Line: 1, Text: text}}, "", true
	}
	if trimmed[0] != '{' {
		return nil, "", false
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, "", false
	}
	lineStarts := lineStartOffsets(data)
	var out []outlineEntry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, "", false
		}
		key, ok := tok.(string)
		if !ok {
			return nil, "", false
		}
		line := lineForOffset(lineStarts, dec.InputOffset())
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, "", false
		}
		if len(out) < outlineMaxEntries {
			out = append(out, outlineEntry{Line: line, Level: 1, Text: key + ": " + jsonKind(value)})
		}
	}
	return out, "", true
}

func jsonKind(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "empty"
	}
	switch raw[0] {
	case '{':
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			return fmt.Sprintf("object (%d keys)", len(m))
		}
		return "object"
	case '[':
		var a []json.RawMessage
		if json.Unmarshal(raw, &a) == nil {
			return fmt.Sprintf("array (%d items)", len(a))
		}
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}

func lineStartOffsets(data []byte) []int64 {
	starts := []int64{0}
	for i, b := range data {
		if b == '\n' {
			starts = append(starts, int64(i+1))
		}
	}
	return starts
}

// lineForOffset maps a byte offset to its 1-based line number.
func lineForOffset(starts []int64, offset int64) int {
	lo, hi := 0, len(starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if starts[mid] <= offset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1
}

// yamlOutline lists top-level mapping keys: unindented "key:" lines outside block scalars.
func yamlOutline(lines []string) []outlineEntry {
	var out []outlineEntry
	for i, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "...") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		if idx+1 < len(line) && line[idx+1] != ' ' && line[idx+1] != '\t' {
			continue // "http://…" style values, not a key
		}
		out = append(out, outlineEntry{Line: i + 1, Level: 1, Text: strings.TrimSpace(line[:idx])})
	}
	return out
}

// delimitedOutline reports a CSV/TSV header's columns and the data row count.
func delimitedOutline(lines []string, sep rune) ([]outlineEntry, string, bool) {
	headerLine := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			headerLine = i
			break
		}
	}
	if headerLine < 0 {
		return nil, "", false
	}
	r := csv.NewReader(strings.NewReader(lines[headerLine]))
	r.Comma = sep
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil || len(header) == 0 {
		return nil, "", false
	}
	rows := 0
	for _, l := range lines[headerLine+1:] {
		if strings.TrimSpace(l) != "" {
			rows++
		}
	}
	cols := header
	if len(cols) > outlineMaxEntries {
		cols = cols[:outlineMaxEntries]
	}
	entries := []outlineEntry{{
		Line: headerLine + 1,
		Text: fmt.Sprintf("header, %d columns: %s", len(header), strings.Join(cols, ", ")),
	}}
	note := fmt.Sprintf("About %d data rows (one per non-empty line; quoted values that span lines are counted per line).", rows)
	return entries, note, true
}

func firstLinesOutline(lines []string) ([]outlineEntry, string) {
	var out []outlineEntry
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, outlineEntry{Line: i + 1, Text: clipLine(strings.TrimSpace(l))})
		if len(out) == outlineFallbackEntries {
			break
		}
	}
	return out, "No recognised structure, so these are the first non-empty lines. Use grep_files to locate a section."
}
