package memoryutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripMemoryContextMetadata(t *testing.T) {
	raw := "Prefers dark mode [stored_at=2026-01-01T00:00:00Z confidence=high age_days=3]"
	require.Equal(t, "Prefers dark mode", StripMemoryContextMetadata(raw))
	require.Equal(t, "Plain fact", StripMemoryContextMetadata("Plain fact"))
}
