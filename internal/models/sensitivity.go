package models

import "strings"

// MemorySensitivity is how delicate a memory or entity is. The levels are ordered, least to most
// restricted: public < personal < sensitive.
//
//   - public: safe to use on public surfaces (for example a Discord thread). Never assigned
//     automatically except by capping to a public-limited chat.
//   - personal: the default for every existing and new memory.
//   - sensitive: health, finances, intimate or otherwise delicate material.
//
// A chat carries a limit (Chat.MemorySensitivityLimit). It may read only the memories and
// entities whose sensitivity is at or below that limit. See docs/ARCHITECTURE_SUMMARY.md.
type MemorySensitivity string

const (
	MemorySensitivityPublic    MemorySensitivity = "public"
	MemorySensitivityPersonal  MemorySensitivity = "personal"
	MemorySensitivitySensitive MemorySensitivity = "sensitive"
)

// DefaultMemorySensitivity is what every existing and new memory or entity gets.
const DefaultMemorySensitivity = MemorySensitivityPersonal

// DefaultMemorySensitivityLimit is a chat's default limit: no restriction.
const DefaultMemorySensitivityLimit = MemorySensitivitySensitive

// AllMemorySensitivities lists the levels from least to most restricted.
var AllMemorySensitivities = []MemorySensitivity{
	MemorySensitivityPublic, MemorySensitivityPersonal, MemorySensitivitySensitive,
}

// Valid reports whether s is one of the three known levels.
func (s MemorySensitivity) Valid() bool {
	return s == MemorySensitivityPublic || s == MemorySensitivityPersonal || s == MemorySensitivitySensitive
}

// Rank orders the levels: public 0, personal 1, sensitive 2. An unknown or empty value ranks as
// the default (personal), the safe reading for legacy rows.
func (s MemorySensitivity) Rank() int {
	switch s {
	case MemorySensitivityPublic:
		return 0
	case MemorySensitivitySensitive:
		return 2
	default:
		return 1
	}
}

// OrDefault returns s, or DefaultMemorySensitivity when s is empty or unknown.
func (s MemorySensitivity) OrDefault() MemorySensitivity {
	if s.Valid() {
		return s
	}
	return DefaultMemorySensitivity
}

// AllowedUnder reports whether something at level s may be read by a chat whose limit is limit.
// An empty or unknown limit means unrestricted (the chat default).
func (s MemorySensitivity) AllowedUnder(limit MemorySensitivity) bool {
	return s.OrDefault().Rank() <= limit.LimitOrDefault().Rank()
}

// LimitOrDefault normalizes a chat limit: empty or unknown is DefaultMemorySensitivityLimit
// (unrestricted), never the personal default, so a missing limit cannot silently restrict.
func (s MemorySensitivity) LimitOrDefault() MemorySensitivity {
	if s.Valid() {
		return s
	}
	return DefaultMemorySensitivityLimit
}

// Restricted reports whether a chat with this limit is a sandbox, that is, its limit is below
// sensitive.
func (s MemorySensitivity) Restricted() bool {
	return s.LimitOrDefault() != MemorySensitivitySensitive
}

// MostRestricted returns the more restricted of two levels (merging folds a group into its most
// restricted member). Empty values count as personal.
func MostRestricted(a, b MemorySensitivity) MemorySensitivity {
	a, b = a.OrDefault(), b.OrDefault()
	if b.Rank() > a.Rank() {
		return b
	}
	return a
}

// CapToLimit returns the level a memory or entity created from a chat should get: want (or the
// default personal when want is empty or unknown) lowered to the chat's limit when the chat is
// restricted. It never raises a level above want.
func CapToLimit(want, limit MemorySensitivity) MemorySensitivity {
	want = want.OrDefault()
	limit = limit.LimitOrDefault()
	if want.Rank() > limit.Rank() {
		return limit
	}
	return want
}

// SensitivitiesUpTo lists the levels a chat with this limit may read, as strings for SQL IN
// predicates.
func SensitivitiesUpTo(limit MemorySensitivity) []string {
	max := limit.LimitOrDefault().Rank()
	out := make([]string, 0, 3)
	for _, s := range AllMemorySensitivities {
		if s.Rank() <= max {
			out = append(out, string(s))
		}
	}
	return out
}

// ParseMemorySensitivity parses an API value (case-insensitive, trimmed). ok is false for an
// empty or unknown value.
func ParseMemorySensitivity(raw string) (MemorySensitivity, bool) {
	s := MemorySensitivity(strings.ToLower(strings.TrimSpace(raw)))
	return s, s.Valid()
}
