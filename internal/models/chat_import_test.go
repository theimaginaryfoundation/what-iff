package models

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestImportProgress_MarshalsImportedIDs(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	b, err := json.Marshal(ImportProgress{
		Phase:       "complete",
		Imported:    1,
		ImportedIDs: []uuid.UUID{id},
	})
	require.NoError(t, err)
	require.Contains(t, string(b), `"imported_ids"`)
	require.Contains(t, string(b), id.String())
}

// TestNormalizeImportedTitle covers the two ways an imported conversation used to disappear:
// a title over the Chat.name byte limit failed Ent validation outright, and a blank-but-not-empty
// title slipped past the "" fallback check and landed as a thread with no visible name.
func TestNormalizeImportedTitle(t *testing.T) {
	t.Parallel()

	const fallback = "Imported chat 2024-01-15 10:30"

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"keeps an ordinary title", "Weekend plans", "Weekend plans"},
		{"trims surrounding whitespace", "  Weekend plans\t\n", "Weekend plans"},
		{"empty falls back", "", fallback},
		{"spaces and tabs fall back", " \t\n ", fallback},
		{"non-breaking space falls back", "  ", fallback},
		{"zero-width space falls back", "​", fallback},
		{"keeps a title exactly at the limit", strings.Repeat("a", MaxChatTitleBytes), strings.Repeat("a", MaxChatTitleBytes)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, NormalizeImportedTitle(tc.title, fallback))
		})
	}
}

func TestNormalizeImportedTitle_FitsTheColumnAndKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	const fallback = "Imported chat 2024-01-15 10:30"

	tests := []struct {
		name  string
		title string
	}{
		{"long ASCII", strings.Repeat("a", MaxChatTitleBytes+50)},
		{"long CJK", strings.Repeat("漢", 200)},            // 3 bytes per rune
		{"long emoji", strings.Repeat("\U0001f600", 100)}, // 4 bytes per rune
		{"mixed widths", strings.Repeat("a漢\U0001f600", 60)},
		{"trailing space before the cut", strings.Repeat("a", MaxChatTitleBytes-5) + strings.Repeat(" b", 20)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeImportedTitle(tc.title, fallback)

			require.LessOrEqual(t, len(got), MaxChatTitleBytes,
				"must fit the Chat.name MaxLen(200) byte validator")
			require.True(t, utf8.ValidString(got), "must not cut in the middle of a rune")
			require.NotEmpty(t, got, "must not violate the NotEmpty validator")
			require.True(t, strings.HasSuffix(got, "…"), "a shortened title should say so")
		})
	}
}
