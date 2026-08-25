package provider

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
	"github.com/stretchr/testify/require"
)

func TestBuildGeminiParams_ModelAndMessages(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "you are a helpful assistant", true)
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "hi there", false)
	ctx.Append(SegmentKindHistoryTurn, RoleAssistant, "hello!", false)
	ctx.AppendUserMessage(RoleUser, "how are you?", nil, false)

	p := ctx.BuildGeminiParams("gemini-3.5-flash")
	require.Equal(t, shared.ChatModel("gemini-3.5-flash"), p.Model)
	require.Equal(t, int64(DefaultMaxContentLength), p.MaxCompletionTokens.Value)
	// system + 2 history turns + 1 user message
	require.Len(t, p.Messages, 4)
	require.NotNil(t, p.Messages[0].OfSystem)
	require.NotNil(t, p.Messages[1].OfUser)
	require.NotNil(t, p.Messages[2].OfAssistant)
	require.NotNil(t, p.Messages[3].OfUser)
}

func TestBuildGeminiParams_RendersImageOnlyTurn(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendUserMessage(RoleUser, "", []UserMessageImage{{RawBytes: []byte{0x1}, MediaType: "image/png"}}, false)
	p := ctx.BuildGeminiParams("gemini-3.5")
	require.Len(t, p.Messages, 1)
	require.NotNil(t, p.Messages[0].OfUser)
	require.NotEmpty(t, p.Messages[0].OfUser.Content.OfArrayOfContentParts)
	require.NotNil(t, p.Messages[0].OfUser.Content.OfArrayOfContentParts[0].OfImageURL)
}

func TestGeminiFunctionTool_ShapesFunction(t *testing.T) {
	t.Parallel()
	tool := GeminiFunctionTool("do_thing", "does a thing", map[string]interface{}{
		"arg": map[string]interface{}{"type": "string"},
	}, []string{"arg"})
	require.NotNil(t, tool.OfFunction)
	require.Equal(t, "do_thing", tool.OfFunction.Function.Name)
	require.Equal(t, "does a thing", tool.OfFunction.Function.Description.Value)
	require.Equal(t, "object", tool.OfFunction.Function.Parameters["type"])
	require.NotNil(t, tool.OfFunction.Function.Parameters["properties"])
}

func TestGeminiProvider_ToGenerateResponse_Nil(t *testing.T) {
	t.Parallel()
	resp := (&GeminiProvider{}).ToGenerateResponse(nil)
	require.NotNil(t, resp)
	require.Empty(t, resp.Text)
}

func TestGeminiProvider_ToGenerateResponse_UnsetUsage(t *testing.T) {
	t.Parallel()
	resp := &openai.ChatCompletion{
		ID: "resp-1",
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{Content: "hello"},
		}},
	}
	got := (&GeminiProvider{}).ToGenerateResponse(resp)
	require.Equal(t, int64(0), got.InputTokens)
	require.Equal(t, int64(0), got.OutputTokens)
	require.Equal(t, "hello", got.Text)
}

func TestBuildGeminiParams_RendersToolResultAttachmentAndPortrait(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindToolResult, RoleDeveloper, "earlier find_context output", false)
	ctx.Append(SegmentKindAttachmentContext, RoleDeveloper, "attached: report.pdf", false)
	ctx.AppendExpressionPortrait([]UserMessageImage{{RawBytes: []byte{0x89, 0x50}, MediaType: "image/png"}})
	ctx.AppendUserMessage(RoleUser, "follow up", nil, false)

	p := ctx.BuildGeminiParams("gemini-3.5-flash")
	require.Len(t, p.Messages, 4)
	require.Equal(t, "earlier find_context output", p.Messages[0].OfUser.Content.OfString.Value)
	require.Equal(t, "attached: report.pdf", p.Messages[1].OfUser.Content.OfString.Value)
	require.Equal(t, ExpressionPortraitVisualReferenceCaption, p.Messages[2].OfUser.Content.OfArrayOfContentParts[0].OfText.Text)
	require.Equal(t, "follow up", p.Messages[3].OfUser.Content.OfString.Value)
}
