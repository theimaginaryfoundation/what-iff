package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

func TestBuildCheckpointConversationSummaryInput(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"Create a compact summary of the conversation so far that will allow the assistant to continue seamlessly.",
		buildCheckpointConversationSummaryInput(""),
	)

	out := buildCheckpointConversationSummaryInput("abc")
	require.True(t, strings.Contains(out, "EXISTING SUMMARY"), out)
	require.True(t, strings.Contains(out, "abc"), out)
}

func TestCheckpointArchivalContext_AppendsAssistantTurn(t *testing.T) {
	t.Parallel()

	base := &provider.ModelContext{}
	base.Append(provider.SegmentKindHistoryTurn, provider.RoleUser, "hello", true)
	base.Append(provider.SegmentKindUserMessage, provider.RoleUser, "follow up", false)

	got := checkpointArchivalContext(base, "assistant reply")
	require.NotNil(t, got)
	require.Len(t, got.Segments, 3)
	require.Equal(t, provider.SegmentKindHistoryTurn, got.Segments[2].Kind)
	require.Equal(t, provider.RoleAssistant, got.Segments[2].Role)
	require.Equal(t, "assistant reply", got.Segments[2].Content)

	// Base must remain unchanged.
	require.Len(t, base.Segments, 2)

	empty := checkpointArchivalContext(base, "   ")
	require.Len(t, empty.Segments, 2)

	nilBase := checkpointArchivalContext(nil, "assistant reply")
	require.NotNil(t, nilBase)
	require.Empty(t, nilBase.Segments)
}

func TestBuildCheckpointSummaryParams_ThreadedMode(t *testing.T) {
	t.Parallel()

	rid := "resp_abc"
	params, err := buildCheckpointSummaryParams(uuid.New(), "", checkpointSummarySource{
		PreviousResponseID: &rid,
	})
	require.NoError(t, err)
	require.Equal(t, archivalOpenAIModel, string(params.Model))
	require.NotNil(t, params.PreviousResponseID)
	require.Equal(t, rid, params.PreviousResponseID.Value)
	require.NotNil(t, params.Instructions)
	require.Equal(t, checkpointConversationSummaryInstructions, params.Instructions.Value)
	require.NotNil(t, params.Input.OfString)
	require.Equal(t, buildCheckpointConversationSummaryInput(""), params.Input.OfString.Value)
	require.Nil(t, params.Input.OfInputItemList)
}

func TestBuildCheckpointSummaryParams_ExplicitMode(t *testing.T) {
	t.Parallel()

	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindHistoryTurn, provider.RoleUser, "hi", true)
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "current user", false)

	params, err := buildCheckpointSummaryParams(uuid.New(), "prior summary", checkpointSummarySource{
		ModelContext:   mc,
		AssistantReply: "current assistant",
	})
	require.NoError(t, err)
	require.Empty(t, params.PreviousResponseID.Value)
	require.NotNil(t, params.Instructions)
	require.Equal(t, checkpointConversationSummaryInstructions, params.Instructions.Value)

	items := params.Input.OfInputItemList
	require.Len(t, items, 4) // history user, current user, assistant reply, update prompt
}

func TestBuildCheckpointSummaryParams_RequiresSource(t *testing.T) {
	t.Parallel()

	_, err := buildCheckpointSummaryParams(uuid.New(), "", checkpointSummarySource{})
	require.Error(t, err)
}
