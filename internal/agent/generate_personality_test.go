package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// GeneratePersonality's cheapest branch is the deliberate mock/local-mode
// denial, which fires before any OpenAI call is built.
func TestGeneratePersonality_MockModeReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{mockLLM: true}
	result, err := a.GeneratePersonality(context.Background(), map[string]string{"q": "a"})
	require.Nil(t, result)
	require.Error(t, err)
	require.ErrorContains(t, err, "personality generation is disabled")
}
