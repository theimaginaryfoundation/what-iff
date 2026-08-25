package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestBuildSubagentModelContext_MinimalSegments(t *testing.T) {
	ctx := buildSubagentModelContext("personality prompt", "scratch", "hello")
	require.NotNil(t, ctx)
	require.Len(t, ctx.Segments, 3)

	require.Equal(t, provider.SegmentKindSystemPrompt, ctx.Segments[0].Kind)
	require.Equal(t, provider.SegmentKindScratchpad, ctx.Segments[1].Kind)
	require.Equal(t, provider.SegmentKindUserMessage, ctx.Segments[2].Kind)
	require.Contains(t, ctx.Segments[0].Content, "personality prompt")
	require.Equal(t, "hello", ctx.Segments[2].Content)
}

func TestBuildSubagentModelContext_ExcludesOptionalContext(t *testing.T) {
	ctx := buildSubagentModelContext("personality prompt", "", "hello")
	require.NotNil(t, ctx)
	require.Len(t, ctx.Segments, 2)

	for _, seg := range ctx.Segments {
		require.NotEqual(t, provider.SegmentKindHistoryTurn, seg.Kind)
		require.NotEqual(t, provider.SegmentKindCheckpointSummary, seg.Kind)
		require.NotEqual(t, provider.SegmentKindMemoryContext, seg.Kind)
	}
}

func TestValidateNoArgs(t *testing.T) {
	require.NoError(t, validateNoArgs(nil))
	require.NoError(t, validateNoArgs([]byte("")))
	require.NoError(t, validateNoArgs([]byte("{}")))
	require.Error(t, validateNoArgs([]byte(`{"foo":"bar"}`)))
	require.Error(t, validateNoArgs([]byte("{")))
}

func TestRunSubagentTool_ValidatesMessage(t *testing.T) {
	a := &Agent{}
	chatCtx := &chatContext{
		model: "gpt-5-mini",
		chat:  &models.Chat{},
	}

	out, err := a.runSubagentTool(context.Background(), chatCtx, []byte(`{"message":"   "}`))
	require.NoError(t, err)

	var result runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "message is required")
}

func TestRunSubagentTool_UnknownModelReturnsError(t *testing.T) {
	// Agent with nil datastore: name lookup returns "not found" gracefully.
	a := &Agent{}
	chatCtx := &chatContext{
		model: "gpt-5-mini",
		chat:  &models.Chat{UserID: uuid.New()},
	}

	out, err := a.runSubagentTool(context.Background(), chatCtx, []byte(`{"message":"hello","model":"no-such-model"}`))
	require.NoError(t, err)

	var result runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "not found")
}

func TestCallSubagentModel_ClaudeProviderMissing(t *testing.T) {
	a := &Agent{}
	out, err := a.callSubagentModel(context.Background(), uuid.New(), "claude-sonnet-4-6", &provider.ModelContext{}, nil)
	require.Error(t, err)
	require.Empty(t, out)
	require.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
}
