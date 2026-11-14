package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryWritePromptText(t *testing.T) {
	t.Parallel()
	out := memoryWritePromptText()
	require.True(t, strings.Contains(out, memoryExtractionPrompt))
	require.True(t, strings.Contains(out, memoryExtractionPostamble))
}

// The note lists stored memories to avoid repeating, and leaves the list out when there are none.
func TestMemoryDedupNote(t *testing.T) {
	t.Parallel()
	require.NotContains(t, memoryDedupNote(nil), "duplicative")
	require.NotContains(t, memoryDedupNote(&chatContext{}), "duplicative")
	note := memoryDedupNote(&chatContext{memories: []string{"likes tea", "lives in Leeds"}})
	require.Contains(t, note, "duplicative")
	require.Contains(t, note, "likes tea\n\nlives in Leeds")
}
