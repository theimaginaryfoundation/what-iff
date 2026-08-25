package datastore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
)

func TestPersonalityUsageStatsFromChats_CountsAndLastUsedAt(t *testing.T) {
	t.Parallel()

	firstPersonalityID := uuid.New()
	secondPersonalityID := uuid.New()
	unusedPersonalityID := uuid.New()
	older := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	newer := older.Add(2 * time.Hour)

	stats := personalityUsageStatsFromChats(
		[]uuid.UUID{firstPersonalityID, secondPersonalityID, unusedPersonalityID},
		[]*ent.Chat{
			chatWithPersonality(firstPersonalityID, older, nil),
			chatWithPersonality(firstPersonalityID, older, &newer),
			chatWithPersonality(secondPersonalityID, older.Add(time.Hour), nil),
		},
	)

	require.Equal(t, 2, stats[firstPersonalityID].ChatCount)
	require.NotNil(t, stats[firstPersonalityID].LastUsedAt)
	require.True(t, newer.Equal(*stats[firstPersonalityID].LastUsedAt))

	require.Equal(t, 1, stats[secondPersonalityID].ChatCount)
	require.NotNil(t, stats[secondPersonalityID].LastUsedAt)
	require.True(t, older.Add(time.Hour).Equal(*stats[secondPersonalityID].LastUsedAt))

	require.Equal(t, 0, stats[unusedPersonalityID].ChatCount)
	require.Nil(t, stats[unusedPersonalityID].LastUsedAt)
}

func TestPersonalityUsageStatsFromChats_ReassignmentUpdatesCounts(t *testing.T) {
	t.Parallel()

	firstPersonalityID := uuid.New()
	secondPersonalityID := uuid.New()
	usedAt := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	personalityIDs := []uuid.UUID{firstPersonalityID, secondPersonalityID}

	before := personalityUsageStatsFromChats(personalityIDs, []*ent.Chat{
		chatWithPersonality(firstPersonalityID, usedAt, nil),
	})
	after := personalityUsageStatsFromChats(personalityIDs, []*ent.Chat{
		chatWithPersonality(secondPersonalityID, usedAt, nil),
	})

	require.Equal(t, 1, before[firstPersonalityID].ChatCount)
	require.Equal(t, 0, before[secondPersonalityID].ChatCount)
	require.Equal(t, 0, after[firstPersonalityID].ChatCount)
	require.Equal(t, 1, after[secondPersonalityID].ChatCount)
}

func TestPersonalityUsageStatsFromChats_IgnoresChatsWithoutLoadedPersonality(t *testing.T) {
	t.Parallel()

	personalityID := uuid.New()
	stats := personalityUsageStatsFromChats([]uuid.UUID{personalityID}, []*ent.Chat{
		{UpdatedAt: time.Now()},
	})

	require.Equal(t, 0, stats[personalityID].ChatCount)
	require.Nil(t, stats[personalityID].LastUsedAt)
}

func chatWithPersonality(personalityID uuid.UUID, updatedAt time.Time, lastMessageTime *time.Time) *ent.Chat {
	return &ent.Chat{
		UpdatedAt:       updatedAt,
		LastMessageTime: lastMessageTime,
		Edges: ent.ChatEdges{
			Personality: &ent.Personality{ID: personalityID},
		},
	}
}
