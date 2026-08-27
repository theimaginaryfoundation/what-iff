package exporter

import (
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
