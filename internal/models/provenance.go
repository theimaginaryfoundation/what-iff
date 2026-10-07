package models

import (
	"strings"
	"unicode"
)

// MemoryProvenance says where a memory came from.
//
//   - user: the account owner's own conversations, or the memory manager. The default for every
//     existing and new memory.
//   - external: learned from people outside the account, in a Discord relay thread (any chat
//     bound to a Discord channel, sandboxed or not). Such memories are
//     unverified: whoever could tag the bot may have said them. They are rendered to the model as
//     unverified and attributed to their speaker (FormatMemoryForContext), and the memory manager
//     can filter by them.
//
// Provenance does not gate reads (the chat sandbox does that). It records trust.
type MemoryProvenance string

const (
	MemoryProvenanceUser     MemoryProvenance = "user"
	MemoryProvenanceExternal MemoryProvenance = "external"
)

// DefaultMemoryProvenance is what every existing and new memory gets.
const DefaultMemoryProvenance = MemoryProvenanceUser

// Valid reports whether p is one of the known values.
func (p MemoryProvenance) Valid() bool {
	return p == MemoryProvenanceUser || p == MemoryProvenanceExternal
}

// OrDefault returns p when it is known, user when it is empty (legacy rows and old exports), and
// external when it is non-empty but unknown, so garbage never reads as more trusted than it may be.
func (p MemoryProvenance) OrDefault() MemoryProvenance {
	switch {
	case p.Valid():
		return p
	case p == "":
		return DefaultMemoryProvenance
	default:
		return MemoryProvenanceExternal
	}
}

// IsExternal reports whether p (normalized) is external.
func (p MemoryProvenance) IsExternal() bool { return p.OrDefault() == MemoryProvenanceExternal }

// ParseMemoryProvenance parses an API value (case-insensitive, trimmed). ok is false for an empty
// or unknown value.
func ParseMemoryProvenance(raw string) (MemoryProvenance, bool) {
	p := MemoryProvenance(strings.ToLower(strings.TrimSpace(raw)))
	return p, p.Valid()
}

// MemoryOrigin is the provenance and speaker a memory is written with.
type MemoryOrigin struct {
	Provenance MemoryProvenance
	// Speaker is the display name of the external person the memory came from; empty when
	// unknown, and always empty for a user memory.
	Speaker string
}

// External reports whether the origin is external.
func (o MemoryOrigin) External() bool { return o.Provenance.IsExternal() }

// SpeakerPtr is the speaker to store: nil for a user memory or an unknown speaker.
func (o MemoryOrigin) SpeakerPtr() *string {
	if !o.External() {
		return nil
	}
	s := CleanSpeakerName(o.Speaker)
	if s == "" {
		return nil
	}
	return &s
}

// OriginFrom builds the origin stored on a memory row.
func OriginFrom(p MemoryProvenance, speaker *string) MemoryOrigin {
	o := MemoryOrigin{Provenance: p.OrDefault()}
	if speaker != nil && o.External() {
		o.Speaker = *speaker
	}
	return o
}

// MergeOrigins combines the origins of memories folded into one: the result is external when any
// member is external, and keeps a speaker only when every member names the same one (a user
// member, which has no speaker, disagrees with any named speaker). With no members it is the user
// default.
func MergeOrigins(members ...MemoryOrigin) MemoryOrigin {
	if len(members) == 0 {
		return MemoryOrigin{Provenance: DefaultMemoryProvenance}
	}
	out := MemoryOrigin{Provenance: MemoryProvenanceUser}
	speaker := ""
	agree := true
	for i, m := range members {
		if m.External() {
			out.Provenance = MemoryProvenanceExternal
		}
		s := ""
		if m.External() {
			s = CleanSpeakerName(m.Speaker)
		}
		if i == 0 {
			speaker = s
		} else if !strings.EqualFold(s, speaker) {
			agree = false
		}
	}
	if out.External() && agree {
		out.Speaker = speaker
	}
	return out
}

// maxSpeakerRunes bounds a stored speaker name (Discord display names are at most 32).
const maxSpeakerRunes = 64

// CleanSpeakerName makes an untrusted display name safe to store and to render inside a memory's
// metadata block: control characters and the bracket and quote characters that delimit that block
// are dropped, whitespace is collapsed, and the result is cut to a bounded length. Names that only
// mean "nobody in particular" ("owner", "unknown", "someone") read as empty.
func CleanSpeakerName(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == '[' || r == ']' || r == '"' || r == '`':
			continue
		case unicode.IsControl(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(s); len(r) > maxSpeakerRunes {
		s = strings.TrimSpace(string(r[:maxSpeakerRunes]))
	}
	switch strings.ToLower(s) {
	case "owner", "unknown", "someone", "none", "n/a":
		return ""
	}
	return s
}
