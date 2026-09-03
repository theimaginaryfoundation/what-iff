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

func TestNormalizeImportedMessageTimesLeavesIncreasingTimesUnchanged(t *testing.T) {
	base := time.Date(2025, 8, 3, 3, 0, 0, 0, time.UTC)
	messages := []models.ChatMessage{
		{Message: "first", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "second", Origin: models.MessageOriginAssistant, SentAt: base.Add(2 * time.Second)},
		{Message: "third", Origin: models.MessageOriginUser, SentAt: base.Add(5 * time.Second)},
	}

	require.Equal(t, messages, normalizeImportedMessageTimes(messages))
}
