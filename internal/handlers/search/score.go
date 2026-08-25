package search

import (
	"strings"
	"unicode"
)

// Scoring tiers used by score(). Higher is more relevant. The numbers carry no
// intrinsic meaning beyond ordering — they just need stable gaps so a
// downstream tiebreak (recency) cannot cross tiers.
const (
	ScoreNoMatch       = 0
	ScoreSubstring     = 40
	ScoreWordBoundary  = 60
	ScorePrefix        = 80
	ScoreExact         = 100
	snippetMaxRuneSize = 140
)

// score returns a relevance score in [ScoreNoMatch, ScoreExact] for the given
// haystack/needle pair. Comparisons are case-insensitive; an empty needle
// always returns ScoreNoMatch (callers should never search the empty string).
func score(haystack, needle string) int {
	h := strings.TrimSpace(strings.ToLower(haystack))
	n := strings.TrimSpace(strings.ToLower(needle))
	if h == "" || n == "" {
		return ScoreNoMatch
	}

	if h == n {
		return ScoreExact
	}
	if strings.HasPrefix(h, n) {
		return ScorePrefix
	}
	if hasWordBoundaryMatch(h, n) {
		return ScoreWordBoundary
	}
	if strings.Contains(h, n) {
		return ScoreSubstring
	}
	return ScoreNoMatch
}

// scoreFields takes the maximum score across multiple candidate strings so a
// label hit can outrank a description-only hit on the same record.
func scoreFields(needle string, fields ...string) int {
	best := ScoreNoMatch
	for _, f := range fields {
		if s := score(f, needle); s > best {
			best = s
		}
	}
	return best
}

// hasWordBoundaryMatch reports whether `needle` appears in `haystack` at the
// start of a word (i.e. the preceding rune is not a letter or digit). Used to
// rank "atlas roadmap" higher for query "road" than "carrot road" only would
// in a substring tier — yes, the same words match, but a multi-token label
// matches mid-word more often, so the boundary signal is mostly useful on
// long descriptive labels and prompt bodies.
func hasWordBoundaryMatch(haystack, needle string) bool {
	if !strings.Contains(haystack, needle) {
		return false
	}
	idx := 0
	for {
		hit := strings.Index(haystack[idx:], needle)
		if hit < 0 {
			return false
		}
		absolute := idx + hit
		if absolute == 0 {
			return true
		}
		prev := rune(haystack[absolute-1])
		if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) {
			return true
		}
		idx = absolute + 1
		if idx >= len(haystack) {
			return false
		}
	}
}

// trimSnippet returns up to snippetMaxRuneSize runes of the given text with
// surrounding whitespace collapsed and an ellipsis appended when truncated.
// Used to render chat-message and memory-content previews without leaking
// multi-paragraph payloads into the palette.
func trimSnippet(text string) string {
	cleaned := strings.Join(strings.Fields(text), " ")
	if cleaned == "" {
		return ""
	}
	runes := []rune(cleaned)
	if len(runes) <= snippetMaxRuneSize {
		return cleaned
	}
	return string(runes[:snippetMaxRuneSize]) + "…"
}
