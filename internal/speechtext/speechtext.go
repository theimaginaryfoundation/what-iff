// Package speechtext turns a reply's markdown into the words to read aloud, and
// splits them into the segments a speech model synthesises (ADR 0x052 §6).
package speechtext

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Spoken is the text to read aloud from a reply: its final round, from offset (in
// bytes), prepared for the ear. Narration before tool calls is not read.
func Spoken(message string, finalRoundOffset int) string {
	if finalRoundOffset > 0 && finalRoundOffset <= len(message) {
		message = message[finalRoundOffset:]
	}
	return Prepare(message)
}

var (
	fence       = regexp.MustCompile("(?ms)^[ \t]*(```|~~~)[^\n]*\n.*?(^[ \t]*(```|~~~)[ \t]*$|\\z)")
	image       = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	link        = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	refLink     = regexp.MustCompile(`\[([^\]]+)\]\[[^\]]*\]`)
	bareURL     = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>()]+`)
	autolink    = regexp.MustCompile(`<(?:https?://|mailto:)[^>]+>`)
	html        = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	inlineCode  = regexp.MustCompile("`([^`\n]*)`")
	strong      = regexp.MustCompile(`(\*\*|__)(\S(?:.*?\S)?)(\*\*|__)`)
	strike      = regexp.MustCompile(`~~(\S(?:.*?\S)?)~~`)
	emphStar    = regexp.MustCompile(`(^|[\s(])\*(\S(?:[^*\n]*\S)?)\*`)
	emphUnder   = regexp.MustCompile(`(^|[\s(])_(\S(?:[^_\n]*\S)?)_`)
	heading     = regexp.MustCompile(`^#{1,6}\s+`)
	listMarker  = regexp.MustCompile(`^(?:[-*+]|\d{1,3}[.)])\s+(?:\[[ xX]\]\s+)?`)
	quoteMarker = regexp.MustCompile(`^(?:>\s?)+`)
	rule        = regexp.MustCompile(`^(?:[-*_]\s*){3,}$`)
	spaces      = regexp.MustCompile(`[ \t]+`)
	spaceBefore = regexp.MustCompile(`\s+([.,!?;:…])`)
)

// Prepare reduces markdown to its words: emphasis, headings and list markers go, a
// link is read as its text, and fenced code, tables, images, bare URLs, HTML and
// emoji are dropped. Paragraphs stay apart; a heading or list item that ends
// without punctuation gets a full stop, so the voice pauses there.
func Prepare(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = fence.ReplaceAllString(md, "")
	md = image.ReplaceAllString(md, "")
	md = link.ReplaceAllString(md, "$1")
	md = refLink.ReplaceAllString(md, "$1")
	md = autolink.ReplaceAllString(md, "")
	md = bareURL.ReplaceAllString(md, "")
	md = html.ReplaceAllString(md, "")

	var paragraphs []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, strings.Join(current, " "))
			current = nil
		}
	}
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || rule.MatchString(line) {
			flush()
			continue
		}
		if strings.HasPrefix(line, "|") {
			continue // a table row
		}
		line = quoteMarker.ReplaceAllString(line, "")
		pause := false
		if heading.MatchString(line) {
			line = heading.ReplaceAllString(line, "")
			pause = true
			flush()
		} else if listMarker.MatchString(line) {
			line = listMarker.ReplaceAllString(line, "")
			pause = true
			flush()
		}
		line = inlineText(line)
		if line == "" {
			continue
		}
		if pause {
			current = append(current, withStop(line))
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return strings.Join(paragraphs, "\n\n")
}

// inlineText strips inline markup and emoji from one line.
func inlineText(s string) string {
	s = inlineCode.ReplaceAllString(s, "$1")
	for range 2 { // nested emphasis
		s = strong.ReplaceAllString(s, "$2")
		s = strike.ReplaceAllString(s, "$1")
		s = emphStar.ReplaceAllString(s, "$1$2")
		s = emphUnder.ReplaceAllString(s, "$1$2")
	}
	s = strings.Map(func(r rune) rune {
		if isEmoji(r) {
			return -1
		}
		return r
	}, s)
	s = spaces.ReplaceAllString(s, " ")
	// What was dropped (an emoji, a link) can leave a space before punctuation.
	return strings.TrimSpace(spaceBefore.ReplaceAllString(s, "$1"))
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF, // pictographs, emoticons, transport, symbols
		r >= 0x2600 && r <= 0x27BF,            // miscellaneous symbols, dingbats
		r >= 0x2B00 && r <= 0x2BFF,            // arrows and stars used as emoji
		r == 0xFE0F, r == 0x200D, r == 0x20E3: // variation selector, joiner, keycap
		return true
	}
	return false
}

func withStop(s string) string {
	last, _ := utf8.DecodeLastRuneInString(s)
	if strings.ContainsRune(".!?…:;", last) {
		return s
	}
	return s + "."
}

// Segment splits prepared text into the pieces synthesised one at a time: the first
// sentence on its own, so speech starts quickly, then whole paragraphs packed up to
// maxChars. A paragraph longer than maxChars is split between sentences, and a
// sentence longer than that between words.
func Segment(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 1000
	}
	var units []string // paragraphs no longer than maxChars
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if utf8.RuneCountInString(p) <= maxChars {
			units = append(units, p)
			continue
		}
		units = append(units, pack(sentences(p), maxChars, " ")...)
	}
	if len(units) == 0 {
		return nil
	}
	// The first sentence alone.
	first := sentences(units[0])
	var out []string
	if len(first) > 1 {
		out = append(out, first[0])
		units[0] = strings.TrimSpace(strings.TrimPrefix(units[0], first[0]))
	}
	return append(out, pack(units, maxChars, "\n\n")...)
}

// pack joins pieces with sep into chunks of at most maxChars, splitting any piece
// that alone is longer between words.
func pack(pieces []string, maxChars int, sep string) []string {
	var out []string
	var cur strings.Builder
	push := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, p := range pieces {
		for _, part := range splitWords(p, maxChars) {
			if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+len(sep)+utf8.RuneCountInString(part) > maxChars {
				push()
			}
			if cur.Len() > 0 {
				cur.WriteString(sep)
			}
			cur.WriteString(part)
		}
	}
	push()
	return out
}

func splitWords(s string, maxChars int) []string {
	if utf8.RuneCountInString(s) <= maxChars {
		return []string{s}
	}
	var out []string
	var cur []string
	n := 0
	for _, w := range strings.Fields(s) {
		wl := utf8.RuneCountInString(w)
		if n > 0 && n+1+wl > maxChars {
			out = append(out, strings.Join(cur, " "))
			cur, n = nil, 0
		}
		cur = append(cur, w)
		if n > 0 {
			n++
		}
		n += wl
	}
	if len(cur) > 0 {
		out = append(out, strings.Join(cur, " "))
	}
	return out
}

// sentences splits a paragraph after sentence-ending punctuation followed by space,
// keeping the punctuation (and closing quotes or brackets) with its sentence.
func sentences(p string) []string {
	var out []string
	runes := []rune(p)
	start := 0
	for i := 0; i < len(runes); i++ {
		if !strings.ContainsRune(".!?…", runes[i]) {
			continue
		}
		j := i + 1
		for j < len(runes) && strings.ContainsRune(".!?…\"'”’)]", runes[j]) {
			j++
		}
		if j < len(runes) && unicode.IsSpace(runes[j]) && !abbreviation(runes[start:i+1]) {
			out = append(out, strings.TrimSpace(string(runes[start:j])))
			start = j
			i = j
		}
	}
	if rest := strings.TrimSpace(string(runes[start:])); rest != "" {
		out = append(out, rest)
	}
	return out
}

// abbreviation reports whether a sentence so far ends in a common abbreviation
// ("e.g.", "Dr.") rather than a sentence.
func abbreviation(s []rune) bool {
	text := strings.ToLower(string(s))
	for _, a := range []string{"e.g.", "i.e.", "etc.", "vs.", "mr.", "mrs.", "ms.", "dr.", "st.", "no.", "approx."} {
		if strings.HasSuffix(text, " "+a) || text == a {
			return true
		}
	}
	return false
}
