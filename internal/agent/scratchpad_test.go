package agent

import (
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func TestShouldSummarizeScratchpad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		scratchpad string
		want       bool
	}{
		{name: "empty", scratchpad: "", want: false},
		{name: "small", scratchpad: "hello", want: false},
		// Use lots of short, space-delimited tokens to reliably exceed the token budget across tokenizers.
		{name: "over_limit", scratchpad: strings.Repeat("a ", provider.ScratchpadMaxContentLength+500), want: true},
	}

	oaiClient := openai.NewClient(option.WithAPIKey("test"))
	a := &Agent{
		OpenAIProvider: provider.NewOpenAIProvider(nil, &oaiClient, nil, telemetry.LoggerOnly(zap.NewNop())),
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, a.shouldSummarizeScratchpad(tc.scratchpad))
		})
	}
}

func TestBuildScratchpadSummarizationInput(t *testing.T) {
	t.Parallel()

	in := "abc\n123"
	out := buildScratchpadSummarizationInput(in)
	require.True(t, strings.Contains(out, "SCRATCHPAD:"), out)
	require.True(t, strings.Contains(out, in), out)
}

func TestBuildUpdateScratchpadPromptUsesPersonalityOverride(t *testing.T) {
	t.Parallel()

	custom := "CUSTOM_SCRATCHPAD_UPDATE_INSTRUCTION"
	p := &models.Personality{
		Scratchpad:             "existing",
		ScratchpadUpdatePrompt: custom,
	}
	out := buildUpdateScratchpadPrompt(p)
	require.Contains(t, out, custom)
	require.Contains(t, out, scratchpadUpdatePostamble)
}
