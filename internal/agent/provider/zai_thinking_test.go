package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// GLM/z.ai models think by default and cannot disable it, so without a bounded
// budget reasoning consumes the whole output cap. ApplyZAIThinkingBudget bounds the
// reasoning and raises the cap so reasoning + answer both fit.
func TestApplyZAIThinkingBudget(t *testing.T) {
	t.Parallel()

	params := (&ModelContext{}).BuildClaudeParams("glm-5.2")
	// Baseline: no thinking, default cap.
	require.Equal(t, int64(DefaultMaxContentLength), params.MaxTokens)
	require.Nil(t, params.Thinking.OfEnabled)

	ApplyZAIThinkingBudget(&params)

	require.Equal(t, int64(ZAIMaxOutputTokens), params.MaxTokens)
	require.Equal(t, int64(12288), params.MaxTokens)
	require.NotNil(t, params.Thinking.OfEnabled)
	require.Equal(t, int64(ZAIThinkingBudgetTokens), params.Thinking.OfEnabled.BudgetTokens)
	require.Equal(t, int64(4096), params.Thinking.OfEnabled.BudgetTokens)

	// Anthropic requires max_tokens > budget_tokens, and the leftover answer budget
	// should match what every other vendor gets (DefaultMaxContentLength).
	require.Greater(t, params.MaxTokens, params.Thinking.OfEnabled.BudgetTokens)
	require.Equal(t, int64(DefaultMaxContentLength), params.MaxTokens-params.Thinking.OfEnabled.BudgetTokens)
}

func TestApplyZAIThinkingBudget_NilIsSafe(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() { ApplyZAIThinkingBudget(nil) })
}
