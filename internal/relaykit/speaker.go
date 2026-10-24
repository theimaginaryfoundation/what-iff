package relaykit

import (
	"slices"
	"strings"
	"unicode"
)

// SpeakerLabel is a display name as it may appear at the start of a relayed line
// ("alice (Discord, #general): text"). Names are chosen by whoever posts, so the characters
// that make up that label are removed: a name cannot pose as another speaker's label or end
// its own early. Names are capped at 64 runes; an empty result reads "someone".
func SpeakerLabel(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', ':', '[', ']', '"':
			return ' '
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 64 {
		name = string(r[:64])
	}
	if name == "" {
		return "someone"
	}
	return name
}

// Allowed applies a binding's allow and deny lists to an author id. Deny always wins; an
// empty allow list means anyone who is not denied.
func Allowed(authorID string, allow, deny []string) bool {
	if slices.Contains(deny, authorID) {
		return false
	}
	return len(allow) == 0 || slices.Contains(allow, authorID)
}
