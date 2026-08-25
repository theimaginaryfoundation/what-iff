package provider

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func TestRenderOpenAIInputItems_UserMessageWithImages(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendUserMessage(RoleUser, "question", []UserMessageImage{{FileID: "file-xyz", MediaType: "image/jpeg"}}, false)

	items := RenderOpenAIInputItems(ctx)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].OfMessage)
	parts := items[0].OfMessage.Content.OfInputItemContentList
	require.Len(t, parts, 2)
	require.NotNil(t, parts[0].OfInputText)
	require.Equal(t, "question", parts[0].OfInputText.Text)
	require.NotNil(t, parts[1].OfInputImage)
	require.True(t, parts[1].OfInputImage.FileID.Valid())
	require.Equal(t, "file-xyz", parts[1].OfInputImage.FileID.Value)
}

func TestRenderOpenAIInputItems_ExpressionPortraitContinuityOrder(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendExpressionPortrait([]UserMessageImage{{FileID: "portrait-file", MediaType: "image/jpeg"}})
	ctx.Append(SegmentKindDeveloperContext, RoleDeveloper,
		`The previous *assistant* message was classified with the expression: "happy" (usage hint: warm).`+ExpressionPortraitContinuityPointerNote,
		false,
	)
	ctx.AppendUserMessage(RoleUser, "current user question", nil, false)

	items := RenderOpenAIInputItems(ctx)
	require.Len(t, items, 3)

	portraitParts := items[0].OfMessage.Content.OfInputItemContentList
	require.Len(t, portraitParts, 2)
	require.Equal(t, ExpressionPortraitVisualReferenceCaption, portraitParts[0].OfInputText.Text)
	require.Equal(t, "portrait-file", portraitParts[1].OfInputImage.FileID.Value)

	require.Contains(t, items[1].OfMessage.Content.OfString.Value, "preceding user message")
	require.Equal(t, "current user question", items[2].OfMessage.Content.OfString.Value)
}

func TestAppendExpressionPortrait(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendExpressionPortrait([]UserMessageImage{{RawBytes: []byte{1, 2}, MediaType: "image/png"}})
	require.Len(t, ctx.Segments, 1)
	require.Equal(t, SegmentKindExpressionPortrait, ctx.Segments[0].Kind)
	require.Equal(t, ExpressionPortraitVisualReferenceCaption, ctx.Segments[0].Content)
	require.Len(t, ctx.Segments[0].UserImages, 1)
}

func TestRenderOpenAIInputItems_SkipsSystemPrompt_PreservesSegmentOrder(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "system-text-not-in-items", true)
	ctx.Append(SegmentKindCheckpointSummary, RoleDeveloper, "ckpt", true)
	ctx.Append(SegmentKindScratchpad, RoleDeveloper, "spad", true)
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "hu", true)
	ctx.Append(SegmentKindHistoryTurn, RoleAssistant, "ha", true)
	ctx.Append(SegmentKindMemoryContext, RoleDeveloper, "membody", true)
	ctx.Append(SegmentKindAttachmentContext, RoleDeveloper, "attbody", true)
	ctx.Append(SegmentKindDeveloperContext, RoleDeveloper, "devctx", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "final", false)

	items := RenderOpenAIInputItems(ctx)
	require.Len(t, items, 8)

	require.Contains(t, itemText(t, items[0]), "checkpoint")
	require.Contains(t, itemText(t, items[0]), "ckpt")

	require.Contains(t, itemText(t, items[1]), "Scratchpad")
	require.Contains(t, itemText(t, items[1]), "spad")

	require.Equal(t, "hu", itemText(t, items[2]))
	require.Equal(t, "ha", itemText(t, items[3]))

	require.Contains(t, itemText(t, items[4]), "memories")
	require.Contains(t, itemText(t, items[4]), "membody")

	require.Contains(t, itemText(t, items[5]), "attbody")
	require.Equal(t, "devctx", itemText(t, items[6]))
	require.Equal(t, "final", itemText(t, items[7]))
}

// itemText extracts assistant/user message text from a ResponseInputItemUnionParam (message variant).
func itemText(t *testing.T, item responses.ResponseInputItemUnionParam) string {
	t.Helper()
	require.NotNil(t, item.OfMessage)
	require.True(t, item.OfMessage.Content.OfString.Valid())
	return item.OfMessage.Content.OfString.Value
}

func TestBuildOpenAIResponseParams_DefaultMaxWhenZero(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "hi", false)

	p := ctx.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           "gpt-5.1",
		MaxOutputTokens: 0,
	})
	require.Equal(t, int64(DefaultMaxContentLength), p.MaxOutputTokens.Value)
}

func TestBuildOpenAIResponseParams_InstructionsFromSystemPromptWhenOptsEmpty(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "only-from-context", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "u", false)

	p := ctx.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           "gpt-5.1",
		MaxOutputTokens: 256,
		Instructions:    "",
	})
	require.Equal(t, "only-from-context", p.Instructions.Value)
}

func TestBuildOpenAIResponseParams_OptsInstructionsOverrideSystemPrompt(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "from-segments", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "u", false)

	p := ctx.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           "gpt-5.1",
		MaxOutputTokens: 128,
		Instructions:    "explicit-instructions",
	})
	require.Equal(t, "explicit-instructions", p.Instructions.Value)
}

func TestBuildOpenAIResponseParams_SafetyUserIDParallelAndTools(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindUserMessage, RoleUser, "hi", false)

	p := ctx.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:             "gpt-5.1",
		SafetyUserID:      "user-safety-1",
		MaxOutputTokens:   512,
		ParallelToolCalls: true,
		Tools:             []responses.ToolUnionParam{{}},
		Instructions:      "instr",
	})
	require.Equal(t, "user-safety-1", p.SafetyIdentifier.Value)
	require.True(t, p.ParallelToolCalls.Value)
	require.Len(t, p.Tools, 1)
}

func TestModelContext_CacheablePrefixContiguous(t *testing.T) {
	t.Parallel()
	var m ModelContext
	m.Append(SegmentKindSystemPrompt, RoleDeveloper, "a", true)
	m.Append(SegmentKindScratchpad, RoleDeveloper, "b", true)
	m.Append(SegmentKindUserMessage, RoleUser, "c", false)
	m.Append(SegmentKindDeveloperContext, RoleDeveloper, "d", true)

	firstNon := -1
	for i, s := range m.Segments {
		if !s.Cacheable {
			firstNon = i
			break
		}
	}
	require.Equal(t, 2, firstNon)
	for i := 0; i < firstNon; i++ {
		require.True(t, m.Segments[i].Cacheable, "index %d", i)
	}
}

func TestBuildOpenAIResponseParams_ModelAndInputItemList(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "hello", false)

	p := ctx.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           "gpt-5.1",
		MaxOutputTokens: 100,
	})
	require.Equal(t, "gpt-5.1", string(p.Model))
	require.NotNil(t, p.Input.OfInputItemList)
	require.GreaterOrEqual(t, len(p.Input.OfInputItemList), 1)
}
