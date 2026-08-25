package provider

import (
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
)

func TestAppendOpenAIHistoryTurn_AssistantImagesUseUserRoleForVision(t *testing.T) {
	t.Parallel()

	var out []responses.ResponseInputItemUnionParam
	seg := ModelContextSegment{
		Kind:    SegmentKindHistoryTurn,
		Role:    RoleAssistant,
		Content: "I made an image",
		UserImages: []UserMessageImage{
			{FileID: "file-img", MediaType: "image/png"},
		},
	}
	appendOpenAIHistoryTurn(&out, seg)
	require.Len(t, out, 2)
	require.Equal(t, responses.EasyInputMessageRole(RoleAssistant), out[0].OfMessage.Role)
	require.Equal(t, responses.EasyInputMessageRole(RoleUser), out[1].OfMessage.Role)
	require.NotNil(t, out[1].OfMessage.Content.OfInputItemContentList[0].OfInputText)
}

func firstClaudeText(m anthropic.MessageParam) string {
	if len(m.Content) == 0 || m.Content[0].OfText == nil {
		return ""
	}
	return m.Content[0].OfText.Text
}

func TestModelContext_StripUserMessageImages(t *testing.T) {
	t.Parallel()

	var ctx ModelContext
	ctx.AppendUserMessage(RoleUser, "hello", []UserMessageImage{{FileID: "f1"}}, false)
	ctx.AppendUserMessage(RoleDeveloper, "", []UserMessageImage{{RawBytes: []byte{1}}}, false)

	ctx.StripUserMessageImages()

	require.Len(t, ctx.Segments, 1)
	require.Equal(t, "hello", ctx.Segments[0].Content)
	require.Empty(t, ctx.Segments[0].UserImages)
}

func TestModelContext_StripUserMessageImages_DropsExpressionPortrait(t *testing.T) {
	t.Parallel()

	var ctx ModelContext
	ctx.AppendExpressionPortrait([]UserMessageImage{{RawBytes: []byte{1, 2}, MediaType: "image/png"}})
	ctx.Append(SegmentKindDeveloperContext, RoleDeveloper, "continuity text", false)

	ctx.StripUserMessageImages()

	require.Len(t, ctx.Segments, 1)
	require.Equal(t, SegmentKindDeveloperContext, ctx.Segments[0].Kind)
}

func TestModelContext_PrepareForTextOnlyChatCompletions(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.AppendExpressionPortrait([]UserMessageImage{{RawBytes: []byte{1, 2}, MediaType: "image/png"}})
	ctx.AppendUserMessage(RoleUser, "", []UserMessageImage{{RawBytes: []byte{0x89, 0x50}, MediaType: "image/png"}}, false)
	ctx.AppendUserMessage(RoleUser, "hello with image", []UserMessageImage{{RawBytes: []byte{0x01}}}, false)

	ctx.PrepareForTextOnlyChatCompletions()

	require.Len(t, ctx.Segments, 2)
	require.Equal(t, TextOnlyChatCompletionsImageFallback, ctx.Segments[0].Content)
	require.Empty(t, ctx.Segments[0].UserImages)
	require.Equal(t, "hello with image", ctx.Segments[1].Content)
	require.Empty(t, ctx.Segments[1].UserImages)

	p := ctx.BuildOpenAIChatCompletionParams("mimo-v2.5-pro")
	for _, msg := range p.Messages {
		if msg.OfUser == nil {
			continue
		}
		require.Empty(t, msg.OfUser.Content.OfArrayOfContentParts)
	}

	pDeepSeek := ctx.BuildOpenAIChatCompletionParams("deepseek-chat")
	for _, msg := range pDeepSeek.Messages {
		if msg.OfUser == nil {
			continue
		}
		require.Empty(t, msg.OfUser.Content.OfArrayOfContentParts)
	}
}

func TestModelContext_Clone(t *testing.T) {
	t.Parallel()

	original := &ModelContext{}
	original.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	original.AppendUserMessage(RoleUser, "hi", []UserMessageImage{
		{FileID: "f1", MediaType: "image/png", RawBytes: []byte{0x01}},
	}, false)

	clone := original.Clone()

	// Clone starts as a structural copy.
	require.Len(t, clone.Segments, 2)
	require.Equal(t, original.Segments[0].Content, clone.Segments[0].Content)

	// Appending to clone must not affect original.
	clone.Append(SegmentKindScratchpad, RoleDeveloper, "scratch", false)
	require.Len(t, original.Segments, 2, "original must be unaffected by clone append")
	require.Len(t, clone.Segments, 3)

	// Mutating cloned UserImages must not affect original.
	clone.Segments[1].UserImages[0].FileID = "mutated"
	require.Equal(t, "f1", original.Segments[1].UserImages[0].FileID, "original UserImages must be independent")

	// Clone of nil is nil.
	var nilCtx *ModelContext
	require.Nil(t, nilCtx.Clone())
}

func TestModelContext_AppendAndSystemPrompt(t *testing.T) {
	t.Parallel()

	var ctx ModelContext
	ctx.Append(SegmentKindScratchpad, RoleDeveloper, "scratch", true)
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "ignored", true)

	require.Len(t, ctx.Segments, 3)
	require.Equal(t, "sys", ctx.SystemPrompt())
}

func TestModelContext_EstimatedTokensBySegment(t *testing.T) {
	t.Parallel()

	counter := NewTokenCounter()
	var ctx ModelContext
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "hello", true)
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "a", true)
	ctx.Append(SegmentKindHistoryTurn, RoleAssistant, "b", true)

	got := ctx.EstimatedTokensBySegment(counter)
	require.Greater(t, got[SegmentKindSystemPrompt], 0)
	require.Greater(t, got[SegmentKindHistoryTurn], 0)
	// Two history segments aggregate under one kind.
	u1, _ := counter.CountTokens("a")
	u2, _ := counter.CountTokens("b")
	require.Equal(t, u1+u2, got[SegmentKindHistoryTurn])
}

func TestModelContext_InsertBeforeLastUserMessage(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "hist-u", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "final-user", false)
	ctx.InsertBeforeLastUserMessage(SegmentKindDeveloperContext, RoleDeveloper, "inline-dev-context", false)

	require.Len(t, ctx.Segments, 3)
	require.Equal(t, SegmentKindHistoryTurn, ctx.Segments[0].Kind)
	require.Equal(t, SegmentKindDeveloperContext, ctx.Segments[1].Kind)
	require.Equal(t, "inline-dev-context", ctx.Segments[1].Content)
	require.Equal(t, SegmentKindUserMessage, ctx.Segments[2].Kind)
	require.Equal(t, "final-user", ctx.Segments[2].Content)
}

func TestModelContext_BuildOpenAIResponseParams(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "hi", false)

	p := ctx.BuildOpenAIResponseParams(OpenAIResponseParamsOptions{
		Model:           "gpt-5.1",
		SafetyUserID:    "user-1",
		MaxOutputTokens: DefaultMaxContentLength,
		Instructions:    "sys",
	})
	require.Equal(t, "gpt-5.1", p.Model)
	require.NotNil(t, p.Instructions)
	require.Len(t, p.Input.OfInputItemList, 1)
}

func TestModelContext_RenderOpenAIInputItems(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindScratchpad, RoleDeveloper, "scratch", true)
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "u1", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "u-current", false)

	items := RenderOpenAIInputItems(ctx)
	require.Len(t, items, 3)
	require.Equal(t, "developer", string(items[0].OfMessage.Role))
	require.Equal(t, "user", string(items[1].OfMessage.Role))
	require.Equal(t, "user", string(items[2].OfMessage.Role))
}

func TestModelContext_RenderClaudeContext(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindScratchpad, RoleDeveloper, "scratch", true)
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "u1", true)
	ctx.Append(SegmentKindMemoryContext, RoleDeveloper, "m1", false)
	ctx.Append(SegmentKindUserMessage, RoleUser, "u-current", false)

	system, messages := renderClaudeContext(ctx)
	require.Len(t, system, 2)
	require.Len(t, messages, 4)
}

func TestRenderClaudeContext_CacheControlOnLastCacheableSegment(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "u-old", true)
	ctx.Append(SegmentKindHistoryTurn, RoleAssistant, "a-old", true)
	ctx.Append(SegmentKindMemoryContext, RoleDeveloper, "mem", false)
	ctx.Append(SegmentKindUserMessage, RoleUser, "hi", false)

	system, messages := renderClaudeContext(ctx)
	require.Len(t, system, 1)
	require.Empty(t, system[0].CacheControl.TTL)
	require.Len(t, messages, 5)
	require.Empty(t, messages[0].Content[0].OfText.CacheControl.TTL)
	require.Equal(t, anthropic.CacheControlEphemeralTTLTTL5m, messages[1].Content[0].OfText.CacheControl.TTL)
	require.Empty(t, messages[2].Content[0].OfText.CacheControl.TTL)
	require.Empty(t, messages[3].Content[0].OfText.CacheControl.TTL)
	require.Empty(t, messages[4].Content[0].OfText.CacheControl.TTL)
}

func TestRenderClaudeContext_CacheControlOnSystemWhenNoHistory(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "sys", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "hi", false)

	system, messages := renderClaudeContext(ctx)
	require.Len(t, system, 1)
	require.Equal(t, anthropic.CacheControlEphemeralTTLTTL5m, system[0].CacheControl.TTL)
	require.Len(t, messages, 1)
	require.Empty(t, messages[0].Content[0].OfText.CacheControl.TTL)
}

func TestModelContext_RenderClaudeContext_DeveloperBeforeUser(t *testing.T) {
	t.Parallel()

	ctx := &ModelContext{}
	ctx.Append(SegmentKindHistoryTurn, RoleUser, "u1", true)
	ctx.Append(SegmentKindUserMessage, RoleUser, "current", false)
	ctx.InsertBeforeLastUserMessage(SegmentKindDeveloperContext, RoleDeveloper, "extra developer context", false)

	system, messages := renderClaudeContext(ctx)
	require.Empty(t, system)
	require.Len(t, messages, 3)
	require.Equal(t, "u1", firstClaudeText(messages[0]))
	require.NotNil(t, messages[0].Content[0].OfText)
	require.Equal(t, anthropic.CacheControlEphemeralTTLTTL5m, messages[0].Content[0].OfText.CacheControl.TTL,
		"cache anchor on last contiguous cacheable segment (history before dynamic developer segment)")
	require.Contains(t, firstClaudeText(messages[1]), "extra developer context")
	require.NotNil(t, messages[1].Content[0].OfText)
	require.Empty(t, messages[1].Content[0].OfText.CacheControl.TTL)
	require.Equal(t, "current", firstClaudeText(messages[2]))
	require.NotNil(t, messages[2].Content[0].OfText)
	require.Empty(t, messages[2].Content[0].OfText.CacheControl.TTL)
}

func TestRenderClaudeContext_NilContext(t *testing.T) {
	t.Parallel()
	system, messages := renderClaudeContext(nil)
	require.Nil(t, system)
	require.Nil(t, messages)
}

func TestRenderClaudeContext_AttachmentContextRendersAsUserMessage(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindAttachmentContext, RoleDeveloper, "attachment-body", false)
	system, messages := renderClaudeContext(ctx)
	require.Empty(t, system)
	require.Len(t, messages, 1)
	require.Equal(t, "attachment-body", firstClaudeText(messages[0]))
}

func TestRenderClaudeContext_ToolResultRendersAsUserMessage(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindToolResult, RoleDeveloper, "earlier-tool-output", false)
	system, messages := renderClaudeContext(ctx)
	require.Empty(t, system)
	require.Len(t, messages, 1)
	require.Equal(t, "earlier-tool-output", firstClaudeText(messages[0]))
}

func TestRenderClaudeContext_AssistantHistoryTurn(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindHistoryTurn, RoleAssistant, "prior assistant", true)
	system, messages := renderClaudeContext(ctx)
	require.Empty(t, system)
	require.Len(t, messages, 1)
	require.Equal(t, "prior assistant", firstClaudeText(messages[0]))
}

func TestRenderClaudeContext_AssistantHistoryTurnWithImagesSplitToUser(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendHistoryTurn(RoleAssistant, "I made an image", []UserMessageImage{
		{RawBytes: []byte{0x89, 0x50}, MediaType: "image/png"},
	}, true)
	system, messages := renderClaudeContext(ctx)
	require.Empty(t, system)
	require.Len(t, messages, 2)
	require.Equal(t, "I made an image", firstClaudeText(messages[0]))
	require.Equal(t, HistoryAssistantImageCaption, messages[1].Content[0].OfText.Text)
	require.NotNil(t, messages[1].Content[1].OfImage)
}

func TestRenderClaudeContext_CheckpointSummaryPrefix(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindCheckpointSummary, RoleDeveloper, "ck", true)
	system, messages := renderClaudeContext(ctx)
	require.Empty(t, messages)
	require.Len(t, system, 1)
	require.Contains(t, system[0].Text, "Conversation checkpoint summary:")
	require.Contains(t, system[0].Text, "ck")
}

func TestBuildClaudeParams_ModelAndDefaultMaxTokens(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.Append(SegmentKindUserMessage, RoleUser, "hi", false)
	p := ctx.BuildClaudeParams("claude-sonnet-4-20250514")
	require.Equal(t, anthropic.Model("claude-sonnet-4-20250514"), p.Model)
	require.Equal(t, int64(DefaultMaxContentLength), p.MaxTokens)
	require.Len(t, p.Messages, 1)
}

func TestRenderClaudeContext_ExpressionPortraitContinuityOrder(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendExpressionPortrait([]UserMessageImage{{RawBytes: []byte{0x89, 0x50}, MediaType: "image/png"}})
	ctx.Append(SegmentKindDeveloperContext, RoleDeveloper,
		`The previous *assistant* message was classified with the expression: "happy".`+ExpressionPortraitContinuityPointerNote,
		false,
	)
	ctx.AppendUserMessage(RoleUser, "current user question", nil, false)

	_, messages := renderClaudeContext(ctx)
	require.Len(t, messages, 3)

	require.Equal(t, ExpressionPortraitVisualReferenceCaption, messages[0].Content[0].OfText.Text)
	require.NotNil(t, messages[0].Content[1].OfImage)

	devText := firstClaudeText(messages[1])
	require.Contains(t, devText, "happy")
	require.Contains(t, devText, "preceding user message")

	require.Equal(t, "current user question", firstClaudeText(messages[2]))
}

func TestRenderClaudeContext_ExpressionPortrait(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendExpressionPortrait([]UserMessageImage{{RawBytes: []byte{0x89, 0x50}, MediaType: "image/png"}})

	_, messages := renderClaudeContext(ctx)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 2)
	require.Equal(t, ExpressionPortraitVisualReferenceCaption, messages[0].Content[0].OfText.Text)
	require.NotNil(t, messages[0].Content[1].OfImage)
}

func TestRenderClaudeContext_UserMessageWithHydratedImages(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendUserMessage(RoleUser, "what is this", []UserMessageImage{
		{FileID: "f1", MediaType: "image/png", RawBytes: []byte{0x89, 0x50}},
	}, false)

	_, messages := renderClaudeContext(ctx)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 2)
	require.NotNil(t, messages[0].Content[0].OfImage)
	require.NotNil(t, messages[0].Content[1].OfText)
	require.Equal(t, "what is this", messages[0].Content[1].OfText.Text)
}

func TestRenderClaudeContext_UserMessageImagesWithoutRawBytes_TextOnly(t *testing.T) {
	t.Parallel()
	ctx := &ModelContext{}
	ctx.AppendUserMessage(RoleUser, "caption this", []UserMessageImage{
		{FileID: "openai-file-id", MediaType: "image/png"},
	}, false)

	_, messages := renderClaudeContext(ctx)
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Content, 1)
	require.NotNil(t, messages[0].Content[0].OfText)
	require.Equal(t, "caption this", messages[0].Content[0].OfText.Text)
}
