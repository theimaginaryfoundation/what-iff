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
