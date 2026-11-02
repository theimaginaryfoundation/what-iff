package datastore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestNormalizeImportedMessageTimesPreservesOrder(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 2, 123456000, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: base.Add(-time.Second)},
	}

	normalized := normalizeImportedMessageTimes(messages)

	require.Equal(t, base, normalized[0].SentAt)
	require.Equal(t, base.Add(time.Microsecond), normalized[1].SentAt)
	require.Equal(t, base.Add(2*time.Microsecond), normalized[2].SentAt)
	require.Equal(t, base, messages[1].SentAt)
	require.Equal(t, base.Add(-time.Second), messages[2].SentAt)
}

func TestNormalizeImportedMessageTimesEnforcesDatastorePrecisionStep(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 2, 123456000, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base.Add(500 * time.Nanosecond)},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: base.Add(750 * time.Nanosecond)},
	}

	normalized := normalizeImportedMessageTimes(messages)

	require.Equal(t, base, normalized[0].SentAt)
	require.Equal(t, base.Add(time.Microsecond), normalized[1].SentAt)
	require.Equal(t, base.Add(2*time.Microsecond), normalized[2].SentAt)
}

func TestNormalizeImportedMessageTimesLeavesPrecisionSafeIncreasingTimesUnchanged(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 0, 0, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base.Add(2 * time.Second)},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: base.Add(5 * time.Second)},
	}

	require.Equal(t, messages, normalizeImportedMessageTimes(messages))
}

func TestNormalizeImportedMessageTimesIsIdempotent(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 2, 123456000, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: base.Add(-time.Second)},
		{Message: "fourth", Origin: models.MessageOriginAssistant, SentAt: base.Add(500 * time.Nanosecond)},
	}

	once := normalizeImportedMessageTimes(messages)
	twice := normalizeImportedMessageTimes(once)

	require.Equal(t, once, twice)
}

func TestPlausibleImportedTimesReplacesBadTimesBeforeNormalizing(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	created := time.Date(2025, 8, 3, 3, 0, 0, 0, time.UTC)
	future := time.Unix(now.UnixMilli(), 0) // milliseconds read as seconds: the year 50000+
	checkpoint := time.Unix(0, 0)
	conv := models.ImportConversation{
		CreatedAt:        created,
		LastCheckpointAt: &checkpoint,
		Messages: []models.ChatMessage{
			{Message: "first", SentAt: created.Add(time.Minute)},
			{Message: "broken", SentAt: future},
			{Message: "third", SentAt: created.Add(3 * time.Minute)},
			{Message: "epoch", SentAt: time.Unix(0, 0)},
		},
	}

	got := plausibleImportedTimes(conv, now)
	normalized := normalizeImportedMessageTimes(got.Messages)

	require.Equal(t, created, got.CreatedAt)
	require.Nil(t, got.LastCheckpointAt, "an implausible checkpoint time is dropped")
	// A bad time takes the one before it, then normalizing spaces the tie apart, so neither the
	// broken message nor anything after it lands in the year 50000.
	require.Equal(t, created.Add(time.Minute), normalized[0].SentAt)
	require.Equal(t, created.Add(time.Minute+time.Microsecond), normalized[1].SentAt)
	require.Equal(t, created.Add(3*time.Minute), normalized[2].SentAt)
	require.Equal(t, created.Add(3*time.Minute+time.Microsecond), normalized[3].SentAt)
	require.Equal(t, future, conv.Messages[1].SentAt, "the caller's messages are not modified")
}

func TestPlausibleImportedTimesFallsBackForTheChatTime(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	firstGood := time.Date(2025, 8, 3, 3, 0, 0, 0, time.UTC)

	got := plausibleImportedTimes(models.ImportConversation{
		Messages: []models.ChatMessage{{SentAt: time.Time{}}, {SentAt: firstGood}},
	}, now)
	require.Equal(t, firstGood, got.CreatedAt, "a bad chat time takes the first good message time")
	require.Equal(t, firstGood, got.Messages[0].SentAt)

	got = plausibleImportedTimes(models.ImportConversation{CreatedAt: time.Unix(0, 0)}, now)
	require.Equal(t, now, got.CreatedAt, "with no good message time it is now")
}
