package relaykit

import (
	"strings"
	"unicode/utf8"
)

// closeFence is appended to a part that ends inside a code block.
const closeFence = "\n```"

// SplitText turns a reply into one or more messages of at most limit runes each,
// for a platform with a message length limit (Discord, Slack). Markdown tables,
// which neither renders, become code blocks first. Paragraphs (and whole code blocks) are packed together while
// they fit; a paragraph that is too long on its own is split on lines, and a line
// that is too long on its own is cut at a space. A code block that has to be
// split is closed at the end of one part and reopened, with its language, at the
// start of the next, so every part renders on its own.
func SplitText(text string, limit int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(TablesToCodeBlocks(text))
	if text == "" {
		return nil
	}
	var parts []string
	cur := ""
	for _, block := range splitBlocks(text) {
		joined := block
		if cur != "" {
			joined = cur + "\n\n" + block
		}
		if runeLen(joined) <= limit {
			cur = joined
			continue
		}
		if cur != "" {
			parts = append(parts, cur)
			cur = ""
		}
		if runeLen(block) <= limit {
			cur = block
			continue
		}
		pieces := splitLongBlock(block, limit)
		parts = append(parts, pieces[:len(pieces)-1]...)
		cur = pieces[len(pieces)-1]
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	return parts
}

// splitBlocks separates text into paragraphs on blank lines, keeping each code
// block whole (blank lines inside a block do not split it).
func splitBlocks(text string) []string {
	var blocks []string
	var cur []string
	inCode := false
	for _, line := range strings.Split(text, "\n") {
		if IsFenceLine(line) {
			inCode = !inCode
		} else if !inCode && strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

// splitLongBlock splits one over-long block on lines, cutting single over-long
// lines, and carries an open code fence across parts. It always returns at least
// one part.
func splitLongBlock(block string, limit int) []string {
	var parts []string
	var cur strings.Builder
	fence := ""    // the opening fence line of the block we are inside, or ""
	reopened := "" // what a part starts with after a flush (the fence, reopened)

	reserve := func() int {
		if fence != "" {
			return runeLen(closeFence)
		}
		return 0
	}
	fits := func(s string) bool {
		return runeLen(cur.String())+runeLen(s)+reserve() <= limit
	}
	flush := func() {
		body := strings.TrimRight(cur.String(), "\n")
		if body != "" && body != strings.TrimRight(reopened, "\n") {
			if fence != "" {
				body += closeFence
			}
			parts = append(parts, body)
		}
		cur.Reset()
		reopened = ""
		if fence != "" {
			reopened = fence + "\n"
			cur.WriteString(reopened)
		}
	}

	for _, line := range strings.Split(block, "\n") {
		piece := line + "\n"
		if !fits(piece) {
			flush()
		}
		// piece != "": a reopened fence and the closing reserve always leave room now (fenceOpening
		// keeps the reopened line short), but never spin on an empty piece if they did not.
		for piece != "" && !fits(piece) {
			room := limit - runeLen(cur.String()) - reserve()
			head, tail := cutRunes(piece, room)
			cur.WriteString(head)
			flush()
			piece = tail
		}
		cur.WriteString(piece)
		if IsFenceLine(line) {
			if fence == "" {
				fence = fenceOpening(line)
			} else {
				fence = ""
			}
		}
	}
	if body := strings.TrimRight(cur.String(), "\n"); body != "" {
		parts = append(parts, body)
	}
	if len(parts) == 0 {
		parts = []string{""}
	}
	return parts
}

// IsFenceLine reports whether line opens or closes a code block.
func IsFenceLine(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "```") }

// maxFenceInfoRunes bounds the language tag a split code block is reopened with.
const maxFenceInfoRunes = 32

// fenceOpening is what a code block split across parts is reopened with: its backticks and,
// when it is a plain language tag, the tag. Not the whole opening line: a line about as long
// as the limit, repeated at the start of every part, would leave no room for the content, and
// the split would never finish.
func fenceOpening(line string) string {
	line = strings.TrimSpace(line)
	ticks := len(line) - len(strings.TrimLeft(line, "`"))
	info := strings.TrimSpace(line[ticks:])
	if info == "" || strings.ContainsAny(info, " \t`") || runeLen(info) > maxFenceInfoRunes {
		return line[:ticks]
	}
	return line[:ticks] + info
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// cutRunes splits s so the head has at most n runes, preferring the last space
// in the second half of that window.
func cutRunes(s string, n int) (head, tail string) {
	if n <= 0 {
		n = 1
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s, ""
	}
	cut := n
	for i := n; i > n/2; i-- {
		if runes[i-1] == ' ' {
			cut = i
			break
		}
	}
	return string(runes[:cut]), string(runes[cut:])
}

// TablesToCodeBlocks wraps runs of Markdown table rows (lines starting and ending
// with "|", with a separator row) in a code block, outside existing code blocks.
// Discord and Slack render neither tables nor their pipes, but both show code
// blocks in a fixed-width font.
func TablesToCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inCode := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if IsFenceLine(line) {
			inCode = !inCode
			out = append(out, line)
			continue
		}
		if !inCode && isTableRow(line) && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			j := i
			for j < len(lines) && isTableRow(lines[j]) {
				j++
			}
			out = append(out, "```")
			out = append(out, lines[i:j]...)
			out = append(out, "```")
			i = j - 1
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= 2 && strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|")
}

func isTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if !isTableRow(t) {
		return false
	}
	return strings.Trim(t, "|-: ") == ""
}
