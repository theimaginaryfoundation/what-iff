package chat

import (
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestNeedsRehydration(t *testing.T) {
	t.Parallel()
	str := func(s string) *string { return &s }
	flag := func(b bool) *bool { return &b }

	cases := []struct {
		name string
		chat models.Chat
		want bool
	}{
		{"imported, unarchived, not yet summarized", models.Chat{Source: str("openai"), Archived: flag(false)}, true},
		{"previous attempt failed", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStateFailed}, true},
		{"native thread", models.Chat{Archived: flag(false)}, false},
		{"empty source", models.Chat{Source: str(""), Archived: flag(false)}, false},
		// An archived thread opens read-only; summarizing waits until it is restored and opened.
		{"archived", models.Chat{Source: str("openai"), Archived: flag(true)}, false},
		{"already pending", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStatePending}, false},
		{"already processing", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStateProcessing}, false},
		{"already ready", models.Chat{Source: str("openai"), Archived: flag(false), RehydrationState: models.RehydrationStateReady}, false},
		{"already has a checkpoint", models.Chat{Source: str("openai"), Archived: flag(false), CheckpointSummary: "summary"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := needsRehydration(&tc.chat); got != tc.want {
				t.Fatalf("needsRehydration = %v, want %v", got, tc.want)
			}
		})
	}
}
