package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
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

func TestBuildCheckpointSummaryRequest_ThreadedMode(t *testing.T) {
	t.Parallel()

	rid := "resp_abc"
	params, err := checkpointSummaryParams(uuid.New(), "", checkpointSummarySource{
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

func TestBuildCheckpointSummaryRequest_ExplicitMode(t *testing.T) {
	t.Parallel()

	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindHistoryTurn, provider.RoleUser, "hi", true)
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "current user", false)

	params, err := checkpointSummaryParams(uuid.New(), "prior summary", checkpointSummarySource{
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

// TestBuildCheckpointSummaryRequest_StripsOpenAIUnsafeImages reproduces the checkpoint
// summary crash: a Claude-chat turn carried an image with a media type OpenAI does not
// support, which returned a 400 from the summarizer. The built params must never surface an
// unsupported media type, and a kept image always renders as a data URL.
func TestBuildCheckpointSummaryRequest_StripsOpenAIUnsafeImages(t *testing.T) {
	t.Parallel()

	mc := &provider.ModelContext{}
	// Supported type with raw bytes: kept, rendered as a data URL.
	mc.AppendHistoryTurn(provider.RoleUser, "look at this", []provider.UserMessageImage{
		{MediaType: "image/jpeg", RawBytes: []byte{1, 2, 3}},
	}, false)
	// Unsupported type from another vendor's turn: must be dropped, its text kept.
	mc.AppendHistoryTurn(provider.RoleUser, "and this heic", []provider.UserMessageImage{
		{MediaType: "image/heic", RawBytes: []byte{4, 5}},
	}, false)

	params, err := checkpointSummaryParams(uuid.New(), "", checkpointSummarySource{ModelContext: mc})
	require.NoError(t, err)

	sawImage := false
	for _, item := range params.Input.OfInputItemList {
		if item.OfMessage == nil {
			continue
		}
		for _, part := range item.OfMessage.Content.OfInputItemContentList {
			if part.OfInputImage == nil {
				continue
			}
			sawImage = true
			require.False(t, part.OfInputImage.FileID.Valid(), "summarizer image must be inline, not a file reference")
			require.True(t, part.OfInputImage.ImageURL.Valid(), "kept image should render as a data URL")
			require.True(t, strings.HasPrefix(part.OfInputImage.ImageURL.Value, "data:image/jpeg;base64,"),
				"data URL should carry a normalized, supported media type")
		}
	}
	require.True(t, sawImage, "the supported jpeg image should survive sanitization")
}

func TestBuildCheckpointSummaryRequest_RequiresSource(t *testing.T) {
	t.Parallel()

	_, err := checkpointSummaryParams(uuid.New(), "", checkpointSummarySource{})
	require.Error(t, err)
}

// summarizeConversationForCheckpoint tolerates a nil chat context, and fails on a missing
// source before making any call.
func TestSummarizeConversationForCheckpoint_NoSourceReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	_, err := a.summarizeConversationForCheckpoint(context.Background(), uuid.New(), nil, checkpointSummarySource{})
	require.ErrorContains(t, err, "requires PreviousResponseID or ModelContext")
}

// checkpointSummaryParams renders the summary request the way the memory role's model
// receives it, so these tests pin the wire shape.
func checkpointSummaryParams(userID uuid.UUID, existing string, src checkpointSummarySource) (responses.ResponseNewParams, error) {
	req, err := buildCheckpointSummaryRequest(userID, existing, src)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	return provider.UtilityResponsesParams(archivalOpenAIModel, req), nil
}
