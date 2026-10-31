package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Unified-diff patching for agent_file_write's "patch" action (ADR 0x034). Hunks are located by
// their context and removed lines, not trusted line numbers: a hunk applies where its old lines
// match exactly, nearest the line its header names when there is more than one match. Models often
// get the counts and numbers in a header slightly wrong, so counts are ignored and a header without
// numbers ("@@ @@") is accepted. All hunks apply or none do.

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+\d+(?:,\d+)? @@`)

type diffHunk struct {
	oldStart  int  // 1-based line the header names (0 for an insertion at the top)
	hasNumber bool // false for a header without line numbers
	old       []string
	new       []string
	ops       []byte // ' ', '-' or '+' per hunk line, in order
}

// parseUnifiedDiff reads the hunks of a single-file unified diff. File headers ("---", "+++",
// "diff --git", "index") before the first hunk are skipped.
func parseUnifiedDiff(diff string) ([]diffHunk, error) {
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	// A trailing newline leaves an empty last element that is not a context line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	var hunks []diffHunk
	var cur *diffHunk
	// bareBlanks counts the bare blank lines that end cur so far. Models often put a blank line
	// between hunks; before an "@@" those are separators, not context, so they are dropped.
	bareBlanks := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if cur != nil && bareBlanks > 0 {
				cur.old = cur.old[:len(cur.old)-bareBlanks]
				cur.new = cur.new[:len(cur.new)-bareBlanks]
				cur.ops = cur.ops[:len(cur.ops)-bareBlanks]
			}
			bareBlanks = 0
			// Any other "@@" line ("@@ @@", "@@ section @@") is a hunk without a line number.
			h := diffHunk{}
			if m := hunkHeaderPattern.FindStringSubmatch(line); m != nil {
				h.oldStart, _ = strconv.Atoi(m[1])
				h.hasNumber = true
			}
			hunks = append(hunks, h)
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil {
			// Preamble before the first hunk: file headers and any commentary.
			continue
		}
		if line == "" {
			bareBlanks++
		} else {
			bareBlanks = 0
		}
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "diff --git"):
			return nil, fmt.Errorf("line %d: a patch may change only one file", i+1)
		case line == "":
			// A blank context line whose leading space was dropped (or a separator; see bareBlanks).
			cur.old = append(cur.old, "")
			cur.new = append(cur.new, "")
			cur.ops = append(cur.ops, ' ')
		case line[0] == ' ':
			cur.old = append(cur.old, line[1:])
			cur.new = append(cur.new, line[1:])
			cur.ops = append(cur.ops, ' ')
		case line[0] == '-':
			cur.old = append(cur.old, line[1:])
			cur.ops = append(cur.ops, '-')
		case line[0] == '+':
			cur.new = append(cur.new, line[1:])
			cur.ops = append(cur.ops, '+')
		case line[0] == '\\':
			// "\ No newline at end of file"
		default:
			return nil, fmt.Errorf("line %d: each hunk line starts with ' ', '-' or '+'", i+1)
		}
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks: a patch needs at least one \"@@ ... @@\" hunk")
	}
	for i, h := range hunks {
		if len(h.old) == 0 && len(h.new) == 0 {
			return nil, fmt.Errorf("hunk %d is empty", i+1)
		}
		if len(h.old) == 0 && !h.hasNumber {
			return nil, fmt.Errorf("hunk %d only adds lines and has no line number; include a few unchanged lines around it as context", i+1)
		}
	}
	return hunks, nil
}

// applyUnifiedDiff applies diff to content and returns the result.
func applyUnifiedDiff(content, diff string) (string, error) {
	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		return "", err
	}
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if content == "" {
		lines = nil
	}
	offset := 0 // lines added minus removed by the hunks applied so far
	from := 0   // hunks apply in order and never overlap
	for i, h := range hunks {
		expected := -1
		if h.hasNumber {
			expected = h.oldStart - 1 + offset
		}
		var pos int
		if len(h.old) == 0 {
			// Pure insertion: "@@ -N,0 +M,k @@" inserts after line N.
			pos = h.oldStart + offset
			if pos < from || pos > len(lines) {
				return "", fmt.Errorf("hunk %d inserts after line %d, but the file has %d lines", i+1, h.oldStart, len(lines)-offset)
			}
		} else {
			pos = findHunk(lines, h.old, from, expected)
			if pos == -2 {
				return "", fmt.Errorf("hunk %d matches more than one place in the file and has no line number; add more context lines or a line number", i+1)
			}
			if pos < 0 {
				return "", fmt.Errorf("hunk %d does not match the file: its unchanged and removed lines (starting %q) were not found; re-read the file and make the patch against its current text", i+1, firstNonEmpty(h.old))
			}
		}
		// Context lines keep the file's own text (it may differ in trailing whitespace).
		next := make([]string, 0, len(lines)-len(h.old)+len(h.new))
		next = append(next, lines[:pos]...)
		src, added := pos, 0
		for _, op := range h.ops {
			switch op {
			case ' ':
				next = append(next, lines[src])
				src++
			case '-':
				src++
			case '+':
				next = append(next, h.new[added])
			}
			if op != '-' {
				added++
			}
		}
		next = append(next, lines[pos+len(h.old):]...)
		lines = next
		from = pos + len(h.new)
		offset += len(h.new) - len(h.old)
	}
	out := strings.Join(lines, "\n")
	if trailingNewline || (content == "" && len(lines) > 0) {
		out += "\n"
	}
	return out, nil
}

// findHunk returns where old occurs in lines at or after from: the occurrence nearest expected, or
// with expected < 0 the only occurrence (-2 when there are several). -1 means none. A second pass
// ignores trailing whitespace, which models often drop or add.
func findHunk(lines, old []string, from, expected int) int {
	for _, eq := range []func(a, b string) bool{
		func(a, b string) bool { return a == b },
		func(a, b string) bool { return strings.TrimRight(a, " \t") == strings.TrimRight(b, " \t") },
	} {
		best, count := -1, 0
		for pos := from; pos+len(old) <= len(lines); pos++ {
			match := true
			for j := range old {
				if !eq(lines[pos+j], old[j]) {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			count++
			if best < 0 || (expected >= 0 && absInt(pos-expected) < absInt(best-expected)) {
				best = pos
			}
		}
		if count == 0 {
			continue
		}
		if expected < 0 && count > 1 {
			return -2
		}
		return best
	}
	return -1
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func firstNonEmpty(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}
