package memoryutil

import (
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestNormalizeContentForDedupe(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trims and lowercases", "  Gori Likes Foxes  ", "gori likes foxes"},
		{"collapses internal whitespace", "gori\tlikes\n  foxes", "gori likes foxes"},
		{"blank becomes empty", "   \n\t ", ""},
		{"already normalized is unchanged", "gori likes foxes", "gori likes foxes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeContentForDedupe(tc.in); got != tc.want {
				t.Fatalf("NormalizeContentForDedupe(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeExtractedMemories(t *testing.T) {
	in := []models.ExtractedMemory{
		{Content: "  keep me ", Scope: "User", Confidence: models.MemoryConfidenceHigh},
		{Content: "   ", Scope: "User"},
		{Content: "bad scope", Scope: "Galaxy"},
		{Content: "no confidence", Scope: "Chat"},
		{Content: "over cap", Scope: "Chat"},
	}

	out := NormalizeExtractedMemories(in, 3)
	if len(out) != 3 {
		t.Fatalf("expected 3 memories after cap, got %d", len(out))
	}
	if out[0].Content != "keep me" || out[0].Confidence != models.MemoryConfidenceHigh {
		t.Errorf("first memory not trimmed/preserved: %+v", out[0])
	}
	if out[1].Scope != "Chat" {
		t.Errorf("invalid scope should coerce to Chat, got %q", out[1].Scope)
	}
	if out[2].Confidence != models.MemoryConfidenceMedium {
		t.Errorf("missing confidence should default to medium, got %q", out[2].Confidence)
	}

	if got := NormalizeExtractedMemories(nil, 0); len(got) != 0 {
		t.Errorf("nil input should yield empty slice, got %d entries", len(got))
	}
}

func TestExtractedSensitivity_FailsClosedAndNeverPublic(t *testing.T) {
	for raw, want := range map[models.MemorySensitivity]models.MemorySensitivity{
		"":               models.MemorySensitivityPersonal,
		"personal":       models.MemorySensitivityPersonal,
		"PERSONAL":       models.MemorySensitivityPersonal,
		"sensitive":      models.MemorySensitivitySensitive,
		" Sensitive ":    models.MemorySensitivitySensitive,
		"public":         models.MemorySensitivityPersonal,  // extraction never assigns public
		"highly-private": models.MemorySensitivitySensitive, // not a level: fail closed
		"secret":         models.MemorySensitivitySensitive,
	} {
		if got := ExtractedSensitivity(raw); got != want {
			t.Errorf("ExtractedSensitivity(%q) = %q, want %q", raw, got, want)
		}
	}
	out := NormalizeExtractedMemories([]models.ExtractedMemory{{Content: "x", Scope: "User", Sensitivity: "nonsense"}}, 0)
	if len(out) != 1 || out[0].Sensitivity != models.MemorySensitivitySensitive {
		t.Errorf("NormalizeExtractedMemories kept %+v, want sensitive", out)
	}
}
