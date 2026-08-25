package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestHandleImageGenerateRitual_InvalidNilInputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "hi", false)
	chat := &models.Chat{ID: uuid.New()}
	userMsg := &models.ChatMessage{ChatID: chat.ID, Message: "draw a cat"}

	_, _, err := (&Agent{}).handleImageGenerateRitual(ctx, uid, nil, &chatContext{chat: chat, model: models.DefaultModelName}, mc)
	require.ErrorContains(t, err, "invalid nil inputs")

	_, _, err = (&Agent{}).handleImageGenerateRitual(ctx, uid, userMsg, nil, mc)
	require.ErrorContains(t, err, "invalid nil inputs")

	_, _, err = (&Agent{}).handleImageGenerateRitual(ctx, uid, userMsg, &chatContext{chat: nil, model: models.DefaultModelName}, mc)
	require.ErrorContains(t, err, "invalid nil inputs")

	_, _, err = (&Agent{}).handleImageGenerateRitual(ctx, uid, userMsg, &chatContext{chat: chat, model: models.DefaultModelName}, nil)
	require.ErrorContains(t, err, "invalid nil inputs")
}

func TestHandleImageGenerateRitual_OpenAI_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	chatID := uuid.New()
	userMsg := &models.ChatMessage{ChatID: chatID, Message: "a red balloon"}
	chatCtx := &chatContext{
		model: models.DefaultModelName,
		chat:  &models.Chat{ID: chatID},
	}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "context", false)

	var gotParams responses.ResponseNewParams
	var inferCalls, genCalls int

	a := &Agent{testHooks: agentTestHooks{
		ImageRitualOpenAIInfer: func(_ context.Context, params responses.ResponseNewParams) (string, *provider.GenerateResponse, error) {
			gotParams = params
			inferCalls++
			return "  trimmed prompt  ", &provider.GenerateResponse{ID: "resp_openai", OutputTokens: 42}, nil
		},
		ImageRitualGenerateImagePNG: func(_ context.Context, prompt string) (string, error) {
			genCalls++
			require.Equal(t, "trimmed prompt", prompt)
			return "base64pngdata", nil
		},
		ImageRitualCreateChatMessage: func(_ context.Context, _ uuid.UUID, cm models.ChatMessage) (*models.ChatMessage, error) {
			require.Equal(t, models.MessageOriginAssistant, cm.Origin)
			require.Equal(t, "Generated image.", cm.Message)
			require.Len(t, cm.ToolCalls, 1)
			require.Equal(t, ImageGenerationToolName, cm.ToolCalls[0].ToolName)
			require.Equal(t, userMsg.Message, cm.ToolCalls[0].ToolInput)
			require.Equal(t, "trimmed prompt", cm.ToolCalls[0].ToolOutput)
			rid := "resp_openai"
			require.NotNil(t, cm.ResponseID)
			require.Equal(t, rid, *cm.ResponseID)
			return &models.ChatMessage{ID: uuid.New(), ChatID: chatID, Message: cm.Message, Origin: cm.Origin, ToolCalls: cm.ToolCalls}, nil
		},
		ImageRitualCreateFileAttachment: func(_ context.Context, _ uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
			require.Equal(t, "image/png", fa.FileType)
			require.Empty(t, fa.FileContent)
			return &models.FileAttachment{ID: uuid.New(), Name: fa.Name, FileType: fa.FileType}, nil
		},
		ImageRitualPersistImage: func(_ context.Context, _ uuid.UUID, attachment *models.FileAttachment, imageBase64 string) error {
			require.NotEqual(t, uuid.Nil, attachment.ID)
			require.Equal(t, "base64pngdata", imageBase64)
			return nil
		},
	}}

	assistant, gen, err := a.handleImageGenerateRitual(ctx, uid, userMsg, chatCtx, mc)
	require.NoError(t, err)
	require.NotNil(t, assistant)
	require.Len(t, assistant.Attachments, 1, "returned message must carry the created attachment")
	require.Equal(t, int64(42), gen.OutputTokens)
	require.Equal(t, models.DefaultModelName, string(gotParams.Model))
	require.Equal(t, int64(imagePromptMaxOutputTokens), gotParams.MaxOutputTokens.Value)
	require.Equal(t, 1, inferCalls)
	require.Equal(t, 1, genCalls)

	// Developer instruction segment appended for prompt inference
	var found bool
	for _, s := range mc.Segments {
		if s.Kind == provider.SegmentKindDeveloperContext && s.Content == imagePromptInstructions {
			found = true
			break
		}
	}
	require.True(t, found, "expected image prompt instructions developer segment")
}

func TestHandleImageGenerateRitual_Claude_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	chatID := uuid.New()
	userMsg := &models.ChatMessage{ChatID: chatID, Message: "sunset"}
	chatCtx := &chatContext{
		model: "claude-sonnet-4-6",
		chat:  &models.Chat{ID: chatID},
	}
	mc := &provider.ModelContext{}

	a := &Agent{testHooks: agentTestHooks{
		ImageRitualClaudeInfer: func(_ context.Context, params anthropic.MessageNewParams) (string, *provider.GenerateResponse, error) {
			require.Equal(t, int64(imagePromptMaxOutputTokens), params.MaxTokens)
			return "claude prompt", &provider.GenerateResponse{ID: "resp_claude", OutputTokens: 7}, nil
		},
		ImageRitualGenerateImagePNG: func(_ context.Context, prompt string) (string, error) {
			require.Equal(t, "claude prompt", prompt)
			return "imgdata", nil
		},
		ImageRitualCreateChatMessage: func(_ context.Context, _ uuid.UUID, cm models.ChatMessage) (*models.ChatMessage, error) {
			return &models.ChatMessage{ID: uuid.New(), ChatID: chatID}, nil
		},
		ImageRitualCreateFileAttachment: func(_ context.Context, _ uuid.UUID, _ models.FileAttachment) (*models.FileAttachment, error) {
			return &models.FileAttachment{ID: uuid.New(), Name: "image.png", FileType: "image/png"}, nil
		},
		ImageRitualPersistImage: func(_ context.Context, _ uuid.UUID, _ *models.FileAttachment, _ string) error {
			return nil
		},
	}}

	_, gen, err := a.handleImageGenerateRitual(ctx, uid, userMsg, chatCtx, mc)
	require.NoError(t, err)
	require.Equal(t, "resp_claude", gen.ID)
}

func TestHandleImageGenerateRitual_Claude_NoProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	chatID := uuid.New()
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "x", false)

	a := &Agent{ClaudeProvider: nil}
	_, _, err := a.handleImageGenerateRitual(ctx, uid, &models.ChatMessage{ChatID: chatID}, &chatContext{
		model: "claude-sonnet-4-6",
		chat:  &models.Chat{ID: chatID},
	}, mc)
	require.ErrorContains(t, err, "ANTHROPIC_API_KEY is not configured")
}

func TestHandleImageGenerateRitual_EmptyPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	chatID := uuid.New()
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "x", false)

	a := &Agent{testHooks: agentTestHooks{
		ImageRitualOpenAIInfer: func(_ context.Context, _ responses.ResponseNewParams) (string, *provider.GenerateResponse, error) {
			return "   ", &provider.GenerateResponse{ID: "r1"}, nil
		},
	}}

	_, _, err := a.handleImageGenerateRitual(ctx, uid, &models.ChatMessage{ChatID: chatID}, &chatContext{
		model: models.DefaultModelName,
		chat:  &models.Chat{ID: chatID},
	}, mc)
	require.ErrorContains(t, err, "empty image prompt from inference")
}

func TestHandleImageGenerateRitual_GenerateImageError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	chatID := uuid.New()
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "x", false)

	a := &Agent{testHooks: agentTestHooks{
		ImageRitualOpenAIInfer: func(_ context.Context, _ responses.ResponseNewParams) (string, *provider.GenerateResponse, error) {
			return "ok", &provider.GenerateResponse{ID: "r1"}, nil
		},
		ImageRitualGenerateImagePNG: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("boom")
		},
	}}

	_, _, err := a.handleImageGenerateRitual(ctx, uid, &models.ChatMessage{ChatID: chatID}, &chatContext{
		model: models.DefaultModelName,
		chat:  &models.Chat{ID: chatID},
	}, mc)
	require.ErrorContains(t, err, "failed to generate image")
	require.ErrorContains(t, err, "boom")
}

func TestHandleImageGenerateRitual_MockMode_PersistsFixturePNG(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid := uuid.New()
	chatID := uuid.New()
	userMsg := &models.ChatMessage{ChatID: chatID, Message: "draw a fox"}
	mc := &provider.ModelContext{}
	mc.Append(provider.SegmentKindUserMessage, provider.RoleUser, "context", false)

	var savedContent string
	a := &Agent{mockLLM: true, testHooks: agentTestHooks{
		// No inference or generation hooks: mock mode must never reach them.
		ImageRitualOpenAIInfer: func(_ context.Context, _ responses.ResponseNewParams) (string, *provider.GenerateResponse, error) {
			t.Fatal("prompt inference must not run in mock mode")
			return "", nil, nil
		},
		ImageRitualGenerateImagePNG: func(_ context.Context, _ string) (string, error) {
			t.Fatal("image generation must not run in mock mode")
			return "", nil
		},
		ImageRitualCreateChatMessage: func(_ context.Context, _ uuid.UUID, cm models.ChatMessage) (*models.ChatMessage, error) {
			return &models.ChatMessage{ID: uuid.New(), ChatID: chatID, Message: cm.Message, Origin: cm.Origin, ToolCalls: cm.ToolCalls}, nil
		},
		ImageRitualCreateFileAttachment: func(_ context.Context, _ uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
			require.Equal(t, "image/png", fa.FileType)
			require.Empty(t, fa.FileContent)
			return &models.FileAttachment{ID: uuid.New(), Name: fa.Name, FileType: fa.FileType}, nil
		},
		// The image bytes reach storage through the persist step, not the
		// attachment payload.
		ImageRitualPersistImage: func(_ context.Context, _ uuid.UUID, attachment *models.FileAttachment, imageBase64 string) error {
			require.NotEqual(t, uuid.Nil, attachment.ID)
			savedContent = imageBase64
			return nil
		},
	}}

	assistant, gen, err := a.handleImageGenerateRitual(ctx, uid, userMsg, &chatContext{
		model: models.DefaultModelName,
		chat:  &models.Chat{ID: chatID},
	}, mc)
	require.NoError(t, err)
	require.NotNil(t, assistant)
	require.Len(t, assistant.Attachments, 1)
	require.NotNil(t, gen)
	require.Contains(t, gen.Text, "draw a fox")

	// The fixture must be a genuine PNG (signature check on decoded bytes).
	raw, err := base64.StdEncoding.DecodeString(savedContent)
	require.NoError(t, err)
	require.Greater(t, len(raw), 8)
	require.Equal(t, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, raw[:8])
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	require.NoError(t, err)
	require.Greater(t, cfg.Width, 0)
}
