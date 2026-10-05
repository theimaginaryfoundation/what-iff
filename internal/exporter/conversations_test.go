package exporter

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestConversationsRoundTripWhatiffContinuationState(t *testing.T) {
	chatID, personalityID := uuid.New(), uuid.New()
	checkpointAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	data, err := BuildConversationsJSON([]ConversationInput{{
		ID:                         chatID,
		Title:                      "Portable thread",
		CreatedAt:                  checkpointAt,
		PersonalityID:              &personalityID,
		CheckpointSummary:          "The user is implementing account portability.",
		CheckpointUserMessageCount: 7,
		LastCheckpointAt:           &checkpointAt,
		DisabledTools:              []string{"web_search"},
		Tags:                       []string{"portable"},
		IsFavorite:                 true,
		IsAutoMood:                 false,
		Messages: []MessageInput{{
			Origin: models.MessageOriginUser,
			Text:   "Continue this thread.",
			SentAt: checkpointAt,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseConversations(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed conversations = %d, want 1", len(parsed))
	}
	got := parsed[0]
	if got.WhatiffPersonalityID == nil || *got.WhatiffPersonalityID != personalityID {
		t.Fatalf("personality ID = %v, want %v", got.WhatiffPersonalityID, personalityID)
	}
	if got.WhatiffCheckpointSummary == "" || got.WhatiffCheckpointUserMessageCnt != 7 || got.WhatiffLastCheckpointAt == nil {
		t.Fatalf("checkpoint state was not preserved: %#v", got)
	}
	if len(got.WhatiffDisabledTools) != 1 || got.WhatiffDisabledTools[0] != "web_search" || !got.WhatiffIsFavorite || got.WhatiffIsAutoMood {
		t.Fatalf("continuation state was not preserved: %#v", got)
	}
}

func TestConversationsRoundTripSandboxed(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	msg := []MessageInput{{Origin: models.MessageOriginUser, Text: "hi", SentAt: now}}
	data, err := BuildConversationsJSON([]ConversationInput{
		{ID: uuid.New(), Title: "sandboxed thread", CreatedAt: now, Messages: msg, Sandboxed: true},
		{ID: uuid.New(), Title: "ordinary thread", CreatedAt: now.Add(time.Second), Messages: msg},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseConversations(data)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range parsed {
		got[c.Name] = c.WhatiffSandboxed
	}
	// A sandboxed chat round-trips; an ordinary one (and an export from before the flag existed)
	// parses to false, which is not sandboxed on import.
	want := map[string]bool{"sandboxed thread": true, "ordinary thread": false}
	for name, sandboxed := range want {
		if got[name] != sandboxed {
			t.Errorf("%s: sandboxed = %v, want %v", name, got[name], sandboxed)
		}
	}
	if strings.Contains(string(data), "whatiff_sandboxed\":false") {
		t.Errorf("the default flag should not be written to the bundle")
	}
}
