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
// bytes), prepared for the ear. Narration before tool calls is not read, unless the
// final round has nothing to read (a turn that ended on a tool): then all of it is.
func Spoken(message string, finalRoundOffset int) string {
	if finalRoundOffset > 0 && finalRoundOffset <= len(message) {
		if final := Prepare(message[finalRoundOffset:]); final != "" {
			return final
		}
	}
	return Prepare(message)
}

// Remaining is what is left of prepared after the segments already said, which are
// pieces of it in order (as Segment cuts them): ok is false when they aren't, because
// the reply or the rules that cut it have changed since.
func Remaining(prepared string, said []string) (string, bool) {
	pos := 0
	for _, seg := range said {
		rest := prepared[pos:]
		trimmed := strings.TrimLeftFunc(rest, unicode.IsSpace)
		if seg == "" || !strings.HasPrefix(trimmed, seg) {
			return "", false
		}
		pos += len(rest) - len(trimmed) + len(seg)
	}
	return strings.TrimSpace(prepared[pos:]), true
}

var (
	fence    = regexp.MustCompile("(?ms)^[ \t]*(```|~~~)[^\n]*\n.*?(^[ \t]*(```|~~~)[ \t]*$|\\z)")
	image    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	link     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	refLink  = regexp.MustCompile(`\[([^\]]+)\]\[[^\]]*\]`)
	bareURL  = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>()]*[^\s<>().,!?;:'"]`)
	autolink = regexp.MustCompile(`<(?:https?://|mailto:)[^>]+>`)
	// A tag with no attributes or name=value ones: "a<b then c>d" is prose, not a tag.
	html        = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9-]*(?:\s+[A-Za-z_:][-\w:.]*\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>]+))*\s*/?>`)
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
// that alone is longer between words. Pieces are joined as they stood in the text, so
// every chunk is a run of it: no separator after a full-width stop, none inside a word
// cut by length.
func pack(pieces []string, maxChars int, sep string) []string {
	var out []string
	var cur strings.Builder
	curLen := 0
	push := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curLen = 0
		}
	}
	for i, p := range pieces {
		gap := sep
		if i > 0 && sep == " " && endsFullWidth(pieces[i-1]) {
			gap = ""
		}
		for j, part := range splitWords(p, maxChars) {
			if j > 0 {
				gap = part.gap
			}
			n := utf8.RuneCountInString(part.text)
			if cur.Len() > 0 && curLen+utf8.RuneCountInString(gap)+n > maxChars {
				push()
			}
			if cur.Len() > 0 {
				cur.WriteString(gap)
				curLen += utf8.RuneCountInString(gap)
			}
			cur.WriteString(part.text)
			curLen += n
		}
	}
	push()
	return out
}

func endsFullWidth(s string) bool {
	last, _ := utf8.DecodeLastRuneInString(strings.TrimRight(s, "\"'”’)]」』）"))
	return strings.ContainsRune("。！？", last)
}

// chunk is part of a sentence, and what stood before it: a space, or nothing when a
// word was cut by length.
type chunk struct{ text, gap string }

// splitWords cuts s into chunks of at most maxChars, between words; a "word" longer
// than that (text written without spaces, as Chinese and Japanese are) is cut by length.
func splitWords(s string, maxChars int) []chunk {
	if utf8.RuneCountInString(s) <= maxChars {
		return []chunk{{text: s}}
	}
	var words []chunk
	for _, w := range strings.Fields(s) {
		gap := " "
		for r := []rune(w); len(r) > maxChars; r = []rune(w) {
			words = append(words, chunk{text: string(r[:maxChars]), gap: gap})
			w, gap = string(r[maxChars:]), ""
		}
		words = append(words, chunk{text: w, gap: gap})
	}
	var out []chunk
	var cur strings.Builder
	curGap, n := "", 0
	for i, w := range words {
		wl := utf8.RuneCountInString(w.text)
		if n > 0 && n+len(w.gap)+wl > maxChars {
			out = append(out, chunk{text: cur.String(), gap: curGap})
			cur.Reset()
			n = 0
		}
		if n == 0 {
			curGap = w.gap
			if i == 0 {
				curGap = ""
			}
		} else {
			cur.WriteString(w.gap)
			n += len(w.gap)
		}
		cur.WriteString(w.text)
		n += wl
	}
	if cur.Len() > 0 {
		out = append(out, chunk{text: cur.String(), gap: curGap})
	}
	return out
}

// sentences splits a paragraph after sentence-ending punctuation followed by space,
// or after a full-width stop (。！？), which takes none, keeping the punctuation (and
// closing quotes or brackets) with its sentence.
func sentences(p string) []string {
	var out []string
	runes := []rune(p)
	start := 0
	for i := 0; i < len(runes); i++ {
		fullWidth := strings.ContainsRune("。！？", runes[i])
		if !fullWidth && !strings.ContainsRune(".!?…", runes[i]) {
			continue
		}
		j := i + 1
		for j < len(runes) && strings.ContainsRune(".!?…。！？\"'”’)]」』）", runes[j]) {
			j++
		}
		if j < len(runes) && (fullWidth || unicode.IsSpace(runes[j])) && !abbreviation(runes[start:i+1]) {
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

// NextSpeakable is the next piece of streaming text that can be read aloud now (ADR 0x052
// §8): only whole sentences, since the rest may still change. The first piece of a round
// is one sentence, so speech starts quickly; later pieces run to the end of a paragraph,
// or to about a third of maxChars within a long one. consumed is how many bytes of text
// the piece covers, with the space after it. A sentence counts as complete once space
// follows it.
func NextSpeakable(text string, first bool, maxChars int) (piece string, consumed int) {
	if maxChars <= 0 {
		maxChars = 1000
	}
	end := 0      // where the pieces taken so far end, spaces after them included
	pieceEnd := 0 // where the text of the pieces taken so far ends
	runes := 0
	for {
		rest := text[end:]
		i := sentenceEnd(rest)
		if i < 0 {
			break
		}
		sentence := strings.TrimSpace(rest[:i])
		n := utf8.RuneCountInString(sentence)
		if runes > 0 && runes+1+n > maxChars {
			break
		}
		gap := rest[i:]
		ws := len(gap) - len(strings.TrimLeftFunc(gap, unicode.IsSpace))
		pieceEnd = end + i
		end += i + ws
		runes += n + 1
		if first || strings.Contains(gap[:ws], "\n\n") || runes >= maxChars/3 {
			break
		}
	}
	if end == 0 {
		return "", 0
	}
	return strings.TrimSpace(text[:pieceEnd]), end
}

// sentenceEnd is the byte index just past the first complete sentence in s (its closing
// punctuation and quotes), or -1 when no sentence is complete yet: one is once space
// follows it.
func sentenceEnd(s string) int {
	for i, r := range s {
		fullWidth := strings.ContainsRune("。！？", r)
		if !fullWidth && !strings.ContainsRune(".!?…", r) {
			continue
		}
		j := i + utf8.RuneLen(r)
		for j < len(s) {
			c, size := utf8.DecodeRuneInString(s[j:])
			if !strings.ContainsRune(".!?…。！？\"'”’)]」』）", c) {
				break
			}
			j += size
		}
		if j < len(s) {
			c, _ := utf8.DecodeRuneInString(s[j:])
			if (fullWidth || unicode.IsSpace(c)) && !abbreviation([]rune(strings.TrimSpace(s[:j]))) {
				return j
			}
		}
	}
	// A paragraph break ends a heading or list item without punctuation too.
	if k := strings.Index(s, "\n\n"); k > 0 && strings.TrimSpace(s[:k]) != "" {
		return k
	}
	return -1
}

// Settled is the part of streaming markdown whose words can't change as more of it
// arrives (ADR 0x052 §8). Prepare reads an inline construct differently once it closes
// ("**Note." is read with its asterisks, "**Note.** More" without), so the line being
// written is cut before any emphasis, code span, strike-through, link or tag still open
// on it, and before a code fence it opens. Finished lines are settled.
func Settled(md string) string {
	lineStart := strings.LastIndexByte(md, '\n') + 1
	line := md[lineStart:]
	if t := strings.TrimLeft(line, " \t"); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
		return md[:lineStart]
	}
	cut := len(line)
	for _, i := range []int{openPair(line, "**"), openPair(line, "__"), openPair(line, "~~"), openCode(line),
		openEmphasis(line, '*'), openEmphasis(line, '_'), openLink(line), openTag(line)} {
		if i >= 0 && i < cut {
			cut = i
		}
	}
	return md[:lineStart+cut]
}

// openPair is where a double marker (**, __, ~~) is left open on the line, or -1.
func openPair(line, marker string) int {
	open := -1
	for i := 0; i+len(marker) <= len(line); {
		if line[i:i+len(marker)] != marker {
			i++
			continue
		}
		if open < 0 {
			open = i
		} else {
			open = -1
		}
		i += len(marker)
	}
	return open
}

// openCode is where a code span is left open on the line, or -1.
func openCode(line string) int {
	open := -1
	for i := 0; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		if open < 0 {
			open = i
		} else {
			open = -1
		}
	}
	return open
}

// openEmphasis is where single-marker emphasis (*word*, _word_) is left open on the line,
// or -1: an opener follows the line's start, a space or "(" and comes before a non-space.
func openEmphasis(line string, m byte) int {
	open := -1
	for i := 0; i < len(line); i++ {
		if line[i] != m {
			continue
		}
		if (i+1 < len(line) && line[i+1] == m) || (i > 0 && line[i-1] == m) {
			continue // part of a double marker
		}
		before := byte(' ')
		if i > 0 {
			before = line[i-1]
		}
		switch {
		case open < 0 && (before == ' ' || before == '\t' || before == '(') && (i+1 == len(line) || line[i+1] != ' '):
			open = i
		case open >= 0 && before != ' ' && before != '\t':
			open = -1
		}
	}
	return open
}

// openLink is where a link or image whose target hasn't arrived yet starts, or -1.
func openLink(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != '[' {
			continue
		}
		start := i
		if i > 0 && line[i-1] == '!' {
			start = i - 1
		}
		j := strings.IndexByte(line[i:], ']')
		if j < 0 || i+j+1 == len(line) {
			return start // the text, or what follows it, is still coming
		}
		j += i
		if line[j+1] == '(' && !strings.Contains(line[j+1:], ")") {
			return start
		}
		if line[j+1] == '[' && !strings.Contains(line[j+2:], "]") {
			return start
		}
	}
	return -1
}

// openTag is where an HTML tag is still being written, or -1.
func openTag(line string) int {
	i := strings.LastIndexByte(line, '<')
	if i < 0 || strings.IndexByte(line[i:], '>') >= 0 {
		return -1
	}
	if i+1 == len(line) {
		return i // what follows decides
	}
	if c := line[i+1]; c == '/' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') {
		return i
	}
	return -1
}

// Resync finds where prepared reaches the words already read aloud, when the text read
// was prepared differently (markup since closed): the words, letters and digits only,
// must be the same, and pos is just past them in prepared. ok is false when they aren't.
func Resync(prepared, said string) (pos int, ok bool) {
	want := []rune(wordRunes(said))
	if len(want) == 0 {
		return 0, true
	}
	k := 0
	for i, r := range prepared {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		if r != want[k] {
			return 0, false
		}
		k++
		if k == len(want) {
			pos = i + utf8.RuneLen(r)
			// The sentence's own closing punctuation goes with it.
			for pos < len(prepared) {
				c, size := utf8.DecodeRuneInString(prepared[pos:])
				if unicode.IsSpace(c) || unicode.IsLetter(c) || unicode.IsDigit(c) {
					break
				}
				pos += size
			}
			return pos, true
		}
	}
	return 0, false
}

func wordRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}
