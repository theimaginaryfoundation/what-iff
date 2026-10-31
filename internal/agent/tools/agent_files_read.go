package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	// agentFileReadMaxLines and agentFileReadMaxChars bound one read; a longer file is read in
	// several calls with start_line.
	agentFileReadMaxLines = 2000
	agentFileReadMaxChars = 24000
	// agentFileOutlineMax caps the headings listed when a search finds none.
	agentFileOutlineMax = 100
)

// AgentFileReadDescription is the agent-facing description of agent_file_read.
var AgentFileReadDescription = fmt.Sprintf(`Read a text file front to back, or pull one section out of it by heading. Works on your own files and on the user's text files (Markdown, plain text, JSON, CSV, code and similar); for images and PDFs use find_context.

action:
- "read": the file's text from start_line (default 1), up to %d lines or about %d characters. When there is more, the result gives next_start_line; call again with it to continue.
- "search": the section under the Markdown heading that best matches heading (exact title first, then close matches), down to the next heading of the same or higher level, with its line numbers. Leave heading empty to get the file's outline.

file is a file id, or a path such as "notes/journal.md". Find files with list (kind="files"); origin="agent" lists files agents made. The result says whether you can change the file (editable).`, agentFileReadMaxLines, agentFileReadMaxChars)

// AgentFileReadToolSpec is the shared function-tool shape for agent_file_read.
var AgentFileReadToolSpec = FunctionToolSpec{
	Name:        agentFileReadToolName,
	Description: AgentFileReadDescription,
	Properties: map[string]interface{}{
		"action": map[string]interface{}{
			"type": "string",
			"enum": []string{"read", "search"},
		},
		"file": map[string]interface{}{
			"type":        "string",
			"description": "A file id, or a path such as 'notes/journal.md'.",
		},
		"start_line": map[string]interface{}{
			"type":        "integer",
			"description": "First line to read (read). Default 1.",
			"minimum":     1,
		},
		"heading": map[string]interface{}{
			"type":        "string",
			"description": "Heading to find (search), e.g. 'Tuesday' or 'Open questions'. Empty for the outline.",
		},
	},
	Required: []string{"action", "file"},
}

type agentFileReadArgs struct {
	Action    string `json:"action"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	Heading   string `json:"heading"`
}

type agentFileHeading struct {
	Title string `json:"title"`
	Level int    `json:"level"`
	Line  int    `json:"line"`
}

type agentFileReadResult struct {
	File          *agentFileRef      `json:"file,omitempty"`
	StartLine     int                `json:"start_line,omitempty"`
	EndLine       int                `json:"end_line,omitempty"`
	TotalLines    int                `json:"total_lines,omitempty"`
	NextStartLine int                `json:"next_start_line,omitempty"`
	Section       *agentFileHeading  `json:"section,omitempty"`
	Content       *string            `json:"content,omitempty"`
	OtherMatches  []agentFileHeading `json:"other_matches,omitempty"`
	Outline       []agentFileHeading `json:"outline,omitempty"`
	Note          string             `json:"note,omitempty"`
	Error         string             `json:"error,omitempty"`
}

// Read executes an agent_file_read call.
func (t *AgentFileTool) Read(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	fail := func(msg string) (string, error) {
		return marshalToolResult(agentFileReadResult{Error: msg}, agentFileReadToolName)
	}
	if chat.IsSandboxed() {
		return fail(sandboxedNote("Agent files"))
	}
	var a agentFileReadArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return fail(fmt.Sprintf("invalid arguments: %v", err))
	}
	if a.Action != "read" && a.Action != "search" {
		return fail(`action must be "read" or "search"`)
	}
	res, err := t.read(ctx, chat, a)
	if msg, ok := AsFileRefusal(err); ok {
		return fail(msg)
	}
	if err != nil {
		t.logger.Error("agent_file_read failed", zap.String("user_id", chat.UserID.String()), zap.Error(err))
		return fail("could not read the file; try again")
	}
	return marshalToolResult(res, agentFileReadToolName)
}

func (t *AgentFileTool) read(ctx context.Context, chat *models.Chat, a agentFileReadArgs) (agentFileReadResult, error) {
	att, err := t.resolve(ctx, chat, a.File)
	if err != nil {
		return agentFileReadResult{}, err
	}
	text, err := t.readText(ctx, att)
	if err != nil {
		return agentFileReadResult{}, err
	}
	ref := t.ref(chat, att, text)
	lines := splitLines(text)
	res := agentFileReadResult{File: &ref, TotalLines: len(lines)}
	if len(lines) == 0 {
		res.Note = "The file is empty."
		return res, nil
	}
	if a.Action == "read" {
		start := max(a.StartLine, 1)
		if start > len(lines) {
			return agentFileReadResult{}, agentFileErrorf("start_line %d is past the end of the file (%d lines)", start, len(lines))
		}
		end := lineWindowEnd(lines, start-1, len(lines))
		content := strings.Join(lines[start-1:end], "\n")
		res.StartLine, res.EndLine, res.Content = start, end, &content
		if end < len(lines) {
			res.NextStartLine = end + 1
		}
		return res, nil
	}

	headings := markdownHeadings(lines)
	query := strings.TrimSpace(a.Heading)
	matches := matchHeadings(headings, query)
	if query == "" || len(matches) == 0 {
		if query != "" {
			res.Note = fmt.Sprintf("No heading matches %q.", query)
		}
		if len(headings) == 0 {
			res.Note = strings.TrimSpace(res.Note + " The file has no Markdown headings; read it with action \"read\".")
			return res, nil
		}
		res.Outline = headings
		if len(res.Outline) > agentFileOutlineMax {
			res.Outline = res.Outline[:agentFileOutlineMax]
			res.Note = strings.TrimSpace(res.Note + fmt.Sprintf(" Showing the first %d of %d headings.", agentFileOutlineMax, len(headings)))
		}
		return res, nil
	}
	best := matches[0]
	sectionEnd := len(lines) // exclusive, 0-based
	for _, h := range headings {
		if h.Line > best.Line && h.Level <= best.Level {
			sectionEnd = h.Line - 1
			break
		}
	}
	end := lineWindowEnd(lines, best.Line-1, sectionEnd)
	content := strings.Join(lines[best.Line-1:end], "\n")
	res.Section, res.StartLine, res.EndLine, res.Content = &best, best.Line, end, &content
	if end < sectionEnd {
		res.NextStartLine = end + 1
		res.Note = "The section is longer than one read; continue with action \"read\" from next_start_line."
	}
	if len(matches) > 1 {
		res.OtherMatches = matches[1:min(len(matches), 10)]
	}
	return res, nil
}

// splitLines splits text into lines without a trailing empty line for a final newline.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
}

// lineWindowEnd returns the exclusive end of the window from 0-based start, stopping at limit, at
// agentFileReadMaxLines, or before the line that would pass agentFileReadMaxChars (always at least
// one line, truncating nothing).
func lineWindowEnd(lines []string, start, limit int) int {
	chars := 0
	end := start
	for end < limit && end-start < agentFileReadMaxLines {
		chars += len(lines[end]) + 1
		if chars > agentFileReadMaxChars && end > start {
			break
		}
		end++
	}
	return end
}

var (
	markdownHeadingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	markdownFencePattern   = regexp.MustCompile("^[ \t]{0,3}(```|~~~)")
)

// markdownHeadings returns the ATX headings of lines with 1-based line numbers, skipping fenced
// code blocks.
func markdownHeadings(lines []string) []agentFileHeading {
	var out []agentFileHeading
	fence := ""
	for i, line := range lines {
		if m := markdownFencePattern.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				fence = m[1]
			case fence == m[1]:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := markdownHeadingPattern.FindStringSubmatch(line); m != nil {
			out = append(out, agentFileHeading{Title: strings.TrimSpace(m[2]), Level: len(m[1]), Line: i + 1})
		}
	}
	return out
}

// matchHeadings ranks the headings that match query: an exact title (ignoring case and spacing),
// then a title containing the query or contained in it, then titles sharing at least half their
// words with it. Ties keep file order.
func matchHeadings(headings []agentFileHeading, query string) []agentFileHeading {
	q := normalizeHeading(query)
	if q == "" {
		return nil
	}
	qWords := strings.Fields(q)
	type scored struct {
		h     agentFileHeading
		score float64
	}
	var hits []scored
	for _, h := range headings {
		title := normalizeHeading(h.Title)
		var score float64
		switch {
		case title == q:
			score = 3
		case strings.Contains(title, q) || strings.Contains(q, title):
			score = 2
		default:
			if overlap := wordOverlap(qWords, strings.Fields(title)); overlap >= 0.5 {
				score = 1 + overlap
			}
		}
		if score > 0 {
			hits = append(hits, scored{h, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]agentFileHeading, len(hits))
	for i, s := range hits {
		out[i] = s.h
	}
	return out
}

var headingPunctuation = regexp.MustCompile(`[^\p{L}\p{N}\s]+`)

func normalizeHeading(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(headingPunctuation.ReplaceAllString(s, " "))), " ")
}

// wordOverlap is the share of the distinct words in a or b that appear in both (Jaccard).
func wordOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]int{}
	for _, w := range a {
		set[w] |= 1
	}
	for _, w := range b {
		set[w] |= 2
	}
	both := 0
	for _, v := range set {
		if v == 3 {
			both++
		}
	}
	return float64(both) / float64(len(set))
}
