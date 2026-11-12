// Package textanchor keeps ranges of a text on the same words as the text changes (ADR 0x045):
// the anchors of comments on text and code files, which have no place of their own for them.
//
// Old and new text are compared word by word, and each range is carried across as Word carries a
// comment's range through an edit: changes inside it stay inside it, a changed start moves to the
// first word after it that is still there, a changed end to the last one before it, and a range
// none of whose words are left is gone. Whitespace is never matched on its own, so it cannot pull
// a range onto text it was not on.
//
// Offsets are UTF-16 code units, as the browser and the Monaco editor count them; a leading byte
// order mark is not part of the text.
package textanchor

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Range is [Start, End) in UTF-16 code units.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// MaxEdits bounds the word diff: past this many inserted and deleted words a range is found by
// its words alone (nearest to where it was), which a rewrite of the whole file comes down to.
const MaxEdits = 1000

// maxSteps bounds the diff's work (words compared, diagonals tried): a large file with many edits
// is otherwise seconds of CPU on a request. Past it a range is found by its words alone, as past
// MaxEdits.
var maxSteps = 20_000_000 // a var so tests can lower it

// token is a word or a single other character of the text, at [start, end) in UTF-16 units. id
// names its value, the same for equal values across the texts compared (see vocabulary).
type token struct {
	start, end int
	id         int32
}

// vocabulary numbers the values of the tokens of the texts compared, so they are compared as
// numbers and each value is held once.
type vocabulary map[string]int32

func (v vocabulary) id(value []byte) int32 {
	if id, ok := v[string(value)]; ok {
		return id
	}
	id := int32(len(v))
	v[string(value)] = id
	return id
}

// Units is the text as UTF-16 code units, without a leading byte order mark.
func Units(text string) []uint16 {
	return utf16.Encode([]rune(strings.TrimPrefix(text, "\ufeff")))
}

// Slice is the text of r in units, as a string.
func Slice(units []uint16, r Range) string {
	if r.Start < 0 || r.End > len(units) || r.Start >= r.End {
		return ""
	}
	return string(utf16.Decode(units[r.Start:r.End]))
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func tokenize(units []uint16, words vocabulary) []token {
	runes := utf16.Decode(units)
	var tokens []token
	var value []byte
	at := 0
	for i := 0; i < len(runes); {
		r := runes[i]
		width := utf16.RuneLen(r)
		if width < 0 {
			width = 1
		}
		switch {
		case unicode.IsSpace(r):
			at += width
			i++
		case isWord(r):
			start := at
			value = value[:0]
			for i < len(runes) && isWord(runes[i]) {
				value = utf8.AppendRune(value, runes[i])
				w := utf16.RuneLen(runes[i])
				if w < 0 {
					w = 1
				}
				at += w
				i++
			}
			tokens = append(tokens, token{start: start, end: at, id: words.id(value)})
		default:
			value = utf8.AppendRune(value[:0], r)
			tokens = append(tokens, token{start: at, end: at + width, id: words.id(value)})
			at += width
			i++
		}
	}
	return tokens
}

// match is, for each old token, the new token it is kept as, or -1. ok is false when the texts
// differ by more than MaxEdits words.
func match(old, new []token) (out []int, ok bool) {
	out = make([]int, len(old))
	for i := range out {
		out[i] = -1
	}
	// The common head and tail need no search.
	head := 0
	for head < len(old) && head < len(new) && old[head].id == new[head].id {
		out[head] = head
		head++
	}
	tail := 0
	for tail < len(old)-head && tail < len(new)-head && old[len(old)-1-tail].id == new[len(new)-1-tail].id {
		out[len(old)-1-tail] = len(new) - 1 - tail
		tail++
	}
	a := old[head : len(old)-tail]
	b := new[head : len(new)-tail]
	pairs, ok := myers(a, b)
	for _, p := range pairs {
		out[head+p[0]] = head + p[1]
	}
	return out, ok
}

// myers is the longest common subsequence of a and b as index pairs (Myers' O(ND) diff); false
// past MaxEdits or maxSteps.
func myers(a, b []token) ([][2]int, bool) {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return nil, true
	}
	max := n + m
	if max > 2*MaxEdits {
		max = 2 * MaxEdits
	}
	offset := max + 1
	v := make([]int, 2*max+3)
	var trace [][]int
	steps := 0
	for d := 0; d <= max; d++ {
		// Only diagonals -(d+1)..d+1 are read back at this step, so only they are kept.
		snapshot := make([]int, 2*d+3)
		copy(snapshot, v[offset-d-1:offset+d+2])
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			if steps++; steps > maxSteps {
				return nil, false
			}
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x].id == b[y].id {
				x++
				y++
				steps++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m, d), true
			}
		}
	}
	return nil, false
}

func backtrack(trace [][]int, n, m, d int) [][2]int {
	var pairs [][2]int
	x, y := n, m
	for ; d > 0; d-- {
		k := x - y
		prev := trace[d]
		at := func(k int) int { return prev[k+d+1] }
		var pk int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := at(pk)
		py := px - pk
		for x > px && y > py && x > 0 && y > 0 {
			x--
			y--
			pairs = append(pairs, [2]int{x, y})
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		x--
		y--
		pairs = append(pairs, [2]int{x, y})
	}
	return pairs
}

// Remap carries each range from oldText to newText; nil where none of its words are left.
func Remap(oldText, newText string, ranges []Range) []*Range {
	oldUnits, newUnits := Units(oldText), Units(newText)
	out := make([]*Range, len(ranges))
	if string(utf16.Decode(oldUnits)) == string(utf16.Decode(newUnits)) {
		for i, r := range ranges {
			if r.Start < r.End && r.End <= len(newUnits) {
				kept := r
				out[i] = &kept
			}
		}
		return out
	}
	words := vocabulary{}
	oldTokens, newTokens := tokenize(oldUnits, words), tokenize(newUnits, words)
	matched, ok := match(oldTokens, newTokens)
	for i, r := range ranges {
		if ok {
			out[i] = carry(r, oldTokens, newTokens, matched)
		} else {
			out[i] = find(Slice(oldUnits, r), newUnits, r.Start)
		}
	}
	return out
}

// carry maps one range through the word match.
func carry(r Range, old, new []token, matched []int) *Range {
	first, last := -1, -1
	for i, t := range old {
		if t.end <= r.Start || t.start >= r.End {
			continue
		}
		if matched[i] < 0 {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return nil
	}
	start := new[matched[first]].start
	// A range that starts or ends inside a kept word keeps that part of the word.
	if delta := r.Start - old[first].start; delta > 0 {
		start += min(delta, new[matched[first]].end-new[matched[first]].start)
	}
	end := new[matched[last]].end
	if delta := old[last].end - r.End; delta > 0 {
		end -= min(delta, new[matched[last]].end-new[matched[last]].start)
	}
	if end <= start {
		return nil
	}
	return &Range{Start: start, End: end}
}

// find is the place of quote in units nearest near, for a text rewritten past the diff's reach.
func find(quote string, units []uint16, near int) *Range {
	if quote == "" {
		return nil
	}
	q := utf16.Encode([]rune(quote))
	best := -1
	for i := 0; i+len(q) <= len(units); i++ {
		if !equalAt(units, q, i) {
			continue
		}
		if best < 0 || abs(i-near) < abs(best-near) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &Range{Start: best, End: best + len(q)}
}

func equalAt(units, q []uint16, at int) bool {
	for j := range q {
		if units[at+j] != q[j] {
			return false
		}
	}
	return true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// FindAll is every place quote appears in units, in order.
func FindAll(units []uint16, quote string) []Range {
	if quote == "" {
		return nil
	}
	q := utf16.Encode([]rune(quote))
	var out []Range
	for i := 0; i+len(q) <= len(units); i++ {
		if equalAt(units, q, i) {
			out = append(out, Range{Start: i, End: i + len(q)})
		}
	}
	return out
}

// LineAt is the 1-based line of the UTF-16 offset at in units.
func LineAt(units []uint16, at int) int {
	line := 1
	for i := 0; i < at && i < len(units); i++ {
		if units[i] == '\n' {
			line++
		}
	}
	return line
}
