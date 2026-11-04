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

	normalized := normalizeImportedMessageTimes(messages, base)

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

	normalized := normalizeImportedMessageTimes(messages, base)

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

	require.Equal(t, messages, normalizeImportedMessageTimes(messages, base))
}

func TestNormalizeImportedMessageTimesIsIdempotent(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 2, 123456000, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: base.Add(-time.Second)},
		{Message: "fourth", Origin: models.MessageOriginAssistant, SentAt: base.Add(500 * time.Nanosecond)},
	}

	once := normalizeImportedMessageTimes(messages, base)
	twice := normalizeImportedMessageTimes(once, base)

	require.Equal(t, once, twice)
}

func TestNormalizeImportedMessageTimesReplacesUnencodableTimes(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 0, 0, time.UTC)
	farFuture := time.Date(56000, 1, 1, 0, 0, 0, 0, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: farFuture},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base.Add(time.Second)},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: farFuture},
	}

	normalized := normalizeImportedMessageTimes(messages, base)

	require.Equal(t, base, normalized[0].SentAt, "a bad first time takes the fallback")
	require.Equal(t, base.Add(time.Second), normalized[1].SentAt)
	require.Equal(t, base.Add(time.Second+time.Microsecond), normalized[2].SentAt, "a bad later time follows its predecessor")
}
