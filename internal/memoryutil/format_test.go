package memoryutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func floatPtr(f float64) *float64 { return &f }

func TestFormatMemoryForContext_BaseAndOptionalSignals(t *testing.T) {
	created := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	// Plain memory at the DEFAULT confidence: base metadata only. A default confidence carries no
	// signal, so it is omitted (like reconfirmed/relevance) rather than emitting a noisy 0.60.
	base := FormatMemoryForContext(&models.Memory{
		Content:    "Prefers dark mode",
		CreatedAt:  created,
		Confidence: models.DefaultMemoryConfidence,
	})
	require.Contains(t, base, "Prefers dark mode")
	require.Contains(t, base, "stored_at=2024-06-01T12:00:00Z")
	require.Contains(t, base, "age_days=")
	require.NotContains(t, base, "confidence", "default confidence is omitted as noise")
	require.NotContains(t, base, "reconfirmed")
	require.NotContains(t, base, "relevance")

	// Non-default confidence IS surfaced — it's actionable (high = trust, low = be wary).
	require.Contains(t, FormatMemoryForContext(&models.Memory{Content: "x", CreatedAt: created, Confidence: 0.9}), "confidence=0.90")
	require.Contains(t, FormatMemoryForContext(&models.Memory{Content: "y", CreatedAt: created, Confidence: 0.3}), "confidence=0.30")

	// Retrieved + corroborated memory: relevance and reconfirmation both surface, and the whole
	// metadata block still strips cleanly (so every load path round-trips through the same codec).
	rich := FormatMemoryForContext(&models.Memory{
		Content:       "Post the daily digest to #status-updates as a root message",
		CreatedAt:     created,
		Confidence:    0.9,
		ChainMetadata: &models.MemoryChainMetadata{DuplicateCount: 5},
		Relevance:     floatPtr(0.873),
	})
	require.Contains(t, rich, "reconfirmed=5x")
	require.Contains(t, rich, "relevance=0.87")
	require.Equal(t, "Post the daily digest to #status-updates as a root message", StripMemoryContextMetadata(rich))
}
