package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Echo-mode directives let a person (or an e2e test) drive the mock model through tool calls from
// the chat, without a real model: each line of the user message of the form
//
//	/mock-tool <tool name> <JSON arguments>
//
// is one round in which the mock calls that tool, and
//
//	/mock-wait <duration>
//
// waits before the next round (e.g. to send a follow-up mid-turn). After the directives the mock
// answers with the rest of the message and a summary of the tool results it got. Only echo mode
// reads directives, and echo mode only runs with LLM_BACKEND=mock.

// mockDirectiveMaxWait bounds /mock-wait.
const mockDirectiveMaxWait = 2 * time.Minute

type mockDirective struct {
	tool *ToolUse
	wait time.Duration
}

// parseMockDirectives splits an echo text into its directives (in order) and the remaining text.
func parseMockDirectives(text string) ([]mockDirective, string) {
	var directives []mockDirective
	var rest []string
	n := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "/mock-tool "):
			fields := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(trimmed, "/mock-tool ")), " ", 2)
			args := "{}"
			if len(fields) == 2 && json.Valid([]byte(strings.TrimSpace(fields[1]))) {
				args = strings.TrimSpace(fields[1])
			}
			n++
			directives = append(directives, mockDirective{tool: &ToolUse{
				ID:    fmt.Sprintf("mock_call_%d", n),
				Name:  fields[0],
				Input: json.RawMessage(args),
			}})
		case strings.HasPrefix(trimmed, "/mock-wait "):
			if d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(trimmed, "/mock-wait "))); err == nil && d > 0 {
				directives = append(directives, mockDirective{wait: min(d, mockDirectiveMaxWait)})
			}
		default:
			rest = append(rest, line)
		}
	}
	return directives, strings.TrimSpace(strings.Join(rest, "\n"))
}

// mockUserNotesSummary lists the user turns added mid-turn (steering), so a local click-through
// can see a follow-up reached the turn.
func mockUserNotesSummary(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nMessages from you while I worked:")
	for _, n := range notes {
		fmt.Fprintf(&b, "\n- %s", strings.ReplaceAll(n, "\n", " "))
	}
	return b.String()
}

// mockToolSummary is the mock's closing note on the tool results it was given.
func mockToolSummary(batches [][]ToolResult) string {
	var b strings.Builder
	for _, batch := range batches {
		for _, r := range batch {
			// Long outputs keep their head and tail: a follow-up the turn took in (steering) is
			// appended to the end of a result.
			out := r.Output
			if rs := []rune(out); len(rs) > 700 {
				out = string(rs[:200]) + " … " + string(rs[len(rs)-450:])
			}
			status := "ok"
			if r.IsErr {
				status = "error"
			}
			fmt.Fprintf(&b, "\n- %s (%s): %s", r.ID, status, out)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\nTool results:" + b.String()
}
