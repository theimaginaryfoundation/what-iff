package pptx

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
)

// paragraphs is a text body's text, a string per paragraph, a line break as "\n".
func paragraphs(txBody *node) []string {
	var out []string
	for _, p := range txBody.kids(nsA, "p") {
		var b strings.Builder
		p.walk(func(n *node) {
			switch {
			case n.is(nsA, "t"):
				b.WriteString(n.Text)
			case n.is(nsA, "br"):
				b.WriteString("\n")
			}
		})
		out = append(out, b.String())
	}
	return out
}

// plainText is a text body's text as comments count it: a line per paragraph.
func plainText(txBody *node) string {
	return strings.Join(paragraphs(txBody), "\n")
}

// textContext is the ac:context PowerPoint checks a text anchor against (MS-ODRAWXML
// CT_TextCharRangeContext): the length and hash of the shape's text with each paragraph ended by
// "\r" and each line break a "\v".
func textContext(txBody *node) string {
	var b strings.Builder
	for _, p := range paragraphs(txBody) {
		b.WriteString(strings.ReplaceAll(p, "\n", "\v"))
		b.WriteString("\r")
	}
	text := b.String()
	return fmt.Sprintf(`<ac:context len="%d" hash="%d"/>`, utf16Len(text), officeArtHash(text))
}

// officeArtHash is MS-ODRAWXML 1.3.9.1: hash = hash*33 + character, over UTF-16 code units, in 32 bits.
func officeArtHash(text string) uint32 {
	var hash uint32
	for _, u := range utf16.Encode([]rune(text)) {
		hash = hash*33 + uint32(u)
	}
	return hash
}

// utf16Len is the length of text as the browser counts it, in UTF-16 code units.
func utf16Len(text string) int {
	return len(utf16.Encode([]rune(text)))
}

// utf16Slice is text[start:start+length] in UTF-16 code units.
func utf16Slice(text string, start, length int) string {
	units := utf16.Encode([]rune(text))
	if start > len(units) {
		return ""
	}
	end := min(start+length, len(units))
	return string(utf16.Decode(units[start:end]))
}

// commentBody is a comment's text as the browser writes it: a paragraph per line.
func commentBody(prefixA, prefixP188, text string) string {
	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text), "\n")
	a := func(local string) string { return qualify(prefixA, local) }
	var b strings.Builder
	b.WriteString("<" + qualify(prefixP188, "txBody") + "><" + a("bodyPr") + "/><" + a("lstStyle") + "/>")
	for _, line := range lines {
		if line == "" {
			b.WriteString("<" + a("p") + "><" + a("endParaRPr") + ` lang="en-US" dirty="0"/></` + a("p") + ">")
			continue
		}
		b.WriteString("<" + a("p") + "><" + a("r") + "><" + a("rPr") + ` lang="en-US" dirty="0"/><` + a("t") + ">" + escapeXML(line) + "</" + a("t") + "></" + a("r") + "></" + a("p") + ">")
	}
	b.WriteString("</" + qualify(prefixP188, "txBody") + ">")
	return b.String()
}

func qualify(prefix, local string) string {
	if prefix == "" {
		return local
	}
	return prefix + ":" + local
}

// bodyText is a comment's text as the browser reads it: a line per paragraph.
func bodyText(txBody *node) string {
	return plainText(txBody)
}

// newGUID is a new comment, author or shape id as PowerPoint writes them: a braced upper-case GUID.
func newGUID() string {
	id, err := uuid.NewRandom()
	if err != nil {
		var b [16]byte
		_, _ = rand.Read(b[:])
		id, _ = uuid.FromBytes(b[:])
	}
	return "{" + strings.ToUpper(id.String()) + "}"
}

// isGUID is whether an id is a braced GUID, as the browser checks a comment id.
func isGUID(id string) bool {
	if len(id) != 38 || id[0] != '{' || id[37] != '}' {
		return false
	}
	_, err := uuid.Parse(id[1:37])
	return err == nil && strings.Count(id, "-") == 4
}

// isoTime is a time as the browser writes it (JavaScript's toISOString).
func isoTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// parseTime reads a comment's time; one with no zone (as PowerPoint writes them) is taken as UTC.
func parseTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Unix(0, 0).UTC()
}

// initials are a name's initials as the browser makes them: up to three, or "?".
func initials(name string) string {
	var b strings.Builder
	n := 0
	for _, w := range strings.Fields(name) {
		if n == 3 {
			break
		}
		r, _ := utf8.DecodeRuneInString(w)
		b.WriteRune(unicode.ToUpper(r))
		n++
	}
	if b.Len() == 0 {
		return "?"
	}
	return b.String()
}

// legacyGUID is the stable id the browser gives a legacy comment.
func legacyGUID(authorID, idx string) string {
	hex := func(v string, width int) string {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || n < 0 {
			n = 0
		}
		s := strings.ToUpper(strconv.FormatUint(uint64(n), 16))
		if len(s) < width {
			s = strings.Repeat("0", width-len(s)) + s
		}
		return s[len(s)-width:]
	}
	return "{00000000-0000-4000-8000-" + hex(authorID, 4) + hex(idx, 8) + "}"
}
