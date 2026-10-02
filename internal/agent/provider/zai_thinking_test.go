package provider

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/require"
)

// GLM/z.ai models always think and ignore a thinking budget, so the only lever is
// output_config.effort plus a raised output cap.
func TestApplyZAIReasoningEffort(t *testing.T) {
	t.Parallel()

	params := (&ModelContext{}).BuildClaudeParams("glm-5.2")
	// Baseline: no effort, default cap.
	require.Equal(t, int64(DefaultMaxContentLength), params.MaxTokens)
	require.Empty(t, params.OutputConfig.Effort)

	ApplyZAIReasoningEffort(&params, ZAIReasoningEffort)

	require.Equal(t, int64(ReasoningMaxOutputTokens), params.MaxTokens)
	require.Equal(t, anthropic.OutputConfigEffort("high"), params.OutputConfig.Effort)
	// No thinking param: z.ai ignores budget_tokens, so we no longer send one.
	require.Nil(t, params.Thinking.OfEnabled)

	raw, err := json.Marshal(params)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"output_config":{"effort":"high"}`)
	require.NotContains(t, string(raw), `"thinking"`)
}

func TestApplyZAIReasoningEffort_NilIsSafe(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() { ApplyZAIReasoningEffort(nil, ZAIReasoningEffort) })
}
