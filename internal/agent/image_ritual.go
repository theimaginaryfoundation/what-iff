package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

const imagePromptMaxOutputTokens = 300

const imagePromptInstructions = `Write ONE concise, high-quality prompt for an image generation model based on the user's latest request and the conversation context.

Rules:
- Output ONLY the prompt text. No quotes, no markdown, no preamble.
- Do not mention policies or refusals.
- If the user wants text in the image, include the exact text to render.
- Prefer concrete visual details (subject, setting, composition, lighting, lens/style).
- If the request is ambiguous, make a reasonable default assumption and proceed.`

// handleImageGenerateRitual executes the system-ritual image generation flow:
// 1) infer an image prompt via the active chat model (OpenAI Responses or Claude Messages; no tools)
// 2) generate image bytes via Images API (gpt-image-1)
// 3) save an assistant message + S3-backed FileAttachment(image/png) via saveImageRitualResult
//
// In mock mode (a.mockLLM) steps 1-2 are skipped entirely — no provider egress —
// and a fixture PNG goes straight into step 3, so the save pipeline is still
// genuinely exercised.
//
// modelContext must be the same object built for the chat turn (history, memories, user text).
// It returns the assistant message plus a normalised GenerateResponse for job/token bookkeeping.
func (a *Agent) handleImageGenerateRitual(ctx context.Context, userID uuid.UUID, userMessage *models.ChatMessage, chatCtx *chatContext, modelContext *provider.ModelContext) (*models.ChatMessage, *provider.GenerateResponse, error) {
	if userMessage == nil || chatCtx == nil || chatCtx.chat == nil || modelContext == nil {
		return nil, nil, fmt.Errorf("invalid nil inputs for image ritual")
	}

	modelContext.Append(provider.SegmentKindDeveloperContext, provider.RoleDeveloper, imagePromptInstructions, false)

	var (
		prompt string
		result *provider.GenerateResponse
	)

	// Mock/local mode: skip prompt inference and image generation entirely (no
	// provider egress), but persist the fixture through the same
	// CreateChatMessage/CreateFileAttachment path below so the save pipeline is
	// genuinely exercised.
	if a.nonVendorLLM() {
		prompt = "Mock image prompt: " + strings.TrimSpace(userMessage.Message)
		result = &provider.GenerateResponse{
			ID:        "mock-image-" + uuid.NewString(),
			CreatedAt: time.Now().Unix(),
			Text:      prompt,
		}
		return a.saveImageRitualResult(ctx, userID, userMessage, prompt, mockRitualPNGBase64(), result)
	}

	switch {
	case models.UsesOpenAIChatCompletionsAPI(chatCtx.modelProvider, chatCtx.model):
		return nil, nil, fmt.Errorf("image ritual prompt inference is not yet supported for %s models", chatCtx.modelProvider)
	case models.UsesAnthropicMessagesAPI(chatCtx.modelProvider, chatCtx.model):
		// Selects a.ClaudeProvider (native Anthropic) or a.ZAIProvider (GLM); both
		// speak the Messages API so the prompt-inference call is identical.
		claudeProvider, _, provErr := a.claudeProviderForModel(chatCtx)
		if provErr != nil && a.testHooks.ImageRitualClaudeInfer == nil {
			return nil, nil, provErr
		}
		params := modelContext.BuildClaudeParams(chatCtx.model)
		params.MaxTokens = imagePromptMaxOutputTokens
		if a.testHooks.ImageRitualClaudeInfer != nil {
			var err error
			prompt, result, err = a.testHooks.ImageRitualClaudeInfer(ctx, params)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to infer image prompt (Claude): %w", err)
			}
		} else {
			msg, err := claudeProvider.Call(telemetry.WithCallPath(ctx, telemetry.CallPathImageRitual), params)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to infer image prompt (Claude): %w", err)
			}
			if msg == nil {
				return nil, nil, fmt.Errorf("nil response from Claude image prompt inference")
			}
			prompt = strings.TrimSpace(provider.ExtractClaudeText(msg))
			result = claudeProvider.ToGenerateResponse(msg)
		}
	default:
		params := modelContext.BuildOpenAIResponseParams(provider.OpenAIResponseParamsOptions{Model: chatCtx.model, MaxOutputTokens: imagePromptMaxOutputTokens})

		if a.testHooks.ImageRitualOpenAIInfer != nil {
			var err error
			prompt, result, err = a.testHooks.ImageRitualOpenAIInfer(ctx, params)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to infer image prompt (OpenAI): %w", err)
			}
		} else {
			if a.OpenAIProvider == nil {
				return nil, nil, fmt.Errorf("OpenAI provider is not configured for image ritual")
			}
			resp, err := a.OpenAIProvider.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathImageRitual), params)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to infer image prompt (OpenAI): %w", err)
			}
			if resp == nil {
				return nil, nil, fmt.Errorf("nil response from image prompt inference")
			}
			prompt = strings.TrimSpace(provider.ProcessResponseOutput(resp))
			result = provider.OpenAIToGenerateResponse(resp)
		}
	}

	if result == nil {
		return nil, nil, fmt.Errorf("nil generate result from image prompt inference")
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, nil, fmt.Errorf("empty image prompt from inference")
	}

	var imageB64 string
	var err error
	if a.testHooks.ImageRitualGenerateImagePNG != nil {
		imageB64, err = a.testHooks.ImageRitualGenerateImagePNG(ctx, prompt)
	} else {
		if a.OpenAIProvider == nil {
			return nil, nil, fmt.Errorf("OpenAI provider is not configured for image generation")
		}
		imageB64, err = a.OpenAIProvider.GenerateImagePNGBase64(ctx, prompt)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate image: %w", err)
	}

	return a.saveImageRitualResult(ctx, userID, userMessage, prompt, imageB64, result)
}

// saveImageRitualResult persists the image-ritual assistant message and its PNG
// attachment. Shared by the real and mock ritual paths so both exercise the
// same save pipeline.
func (a *Agent) saveImageRitualResult(ctx context.Context, userID uuid.UUID, userMessage *models.ChatMessage, prompt, imageB64 string, result *provider.GenerateResponse) (*models.ChatMessage, *provider.GenerateResponse, error) {
	toolCalls := []*models.ToolCall{
		{
			ToolName:   ImageGenerationToolName,
			ToolInput:  userMessage.Message,
			ToolOutput: prompt,
		},
	}

	// Save assistant message (so the UI renders the image attachment in the normal message stream).
	rid := result.ID
	assistantPayload := models.ChatMessage{
		ChatID:     userMessage.ChatID,
		Message:    "Generated image.",
		Origin:     models.MessageOriginAssistant,
		ResponseID: &rid,
		Tokens:     result.OutputTokens,
		ToolCalls:  toolCalls,
	}
	var assistantMsg *models.ChatMessage
	var err error
	if a.testHooks.ImageRitualCreateChatMessage != nil {
		assistantMsg, err = a.testHooks.ImageRitualCreateChatMessage(ctx, userID, assistantPayload)
	} else {
		assistantMsg, err = a.ds.CreateChatMessage(ctx, userID, assistantPayload)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create assistant message for image ritual: %w", err)
	}

	// Save image attachment.
	imgName := fmt.Sprintf("image_%s.png", uuid.NewString())
	attachPayload := models.FileAttachment{
		UserID:        userID,
		Name:          imgName,
		FileType:      "image/png",
		ChatMessageID: &assistantMsg.ID,
	}
	var attachment *models.FileAttachment
	if a.testHooks.ImageRitualCreateFileAttachment != nil {
		attachment, err = a.testHooks.ImageRitualCreateFileAttachment(ctx, userID, attachPayload)
	} else {
		attachment, err = a.ds.CreateFileAttachment(ctx, userID, attachPayload)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create image attachment: %w", err)
	}
	if attachment == nil {
		return nil, nil, fmt.Errorf("created image attachment is nil")
	}
	if a.testHooks.ImageRitualPersistImage != nil {
		err = a.testHooks.ImageRitualPersistImage(ctx, userID, attachment, imageB64)
	} else {
		err = a.persistImageRitualAttachment(ctx, userID, attachment, imageB64)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to persist image attachment to S3: %w", err)
	}

	// Attach the created record to the returned message. The attachment is
	// created after the message, so without this the caller (and any consumer
	// of the returned message) sees an empty Attachments slice even though the
	// attachment persisted.
	assistantMsg.Attachments = append(assistantMsg.Attachments, attachment)

	return assistantMsg, result, nil
}

func (a *Agent) persistImageRitualAttachment(ctx context.Context, userID uuid.UUID, attachment *models.FileAttachment, imageB64 string) error {
	if a.fileStore == nil {
		return fmt.Errorf("file store is not configured")
	}
	rawBytes, err := base64.StdEncoding.DecodeString(imageB64)
	if err != nil {
		return fmt.Errorf("decode generated image: %w", err)
	}
	if len(rawBytes) == 0 {
		return fmt.Errorf("decode generated image: empty payload")
	}

	imageKey := storage.FileKeyForImage(userID, attachment.ID, attachment.Name)
	if err := a.fileStore.UploadFile(ctx, imageKey, rawBytes, attachment.FileType); err != nil {
		return fmt.Errorf("upload image: %w", err)
	}
	if err := a.ds.SetFileAttachmentS3Key(ctx, userID, attachment.ID, imageKey); err != nil {
		return fmt.Errorf("set image s3 key: %w", err)
	}

	thumb, err := imageutil.GenerateThumbnail(rawBytes, imageutil.DefaultThumbnailMaxPx)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("failed to generate image ritual thumbnail", zap.String("attachment_id", attachment.ID.String()), zap.Error(err))
		}
		return nil // The full-resolution object is canonical; thumbnails are best effort.
	}
	if err := a.fileStore.UploadFile(ctx, storage.FileKeyForImageThumbnail(userID, attachment.ID), thumb, "image/jpeg"); err != nil {
		if a.logger != nil {
			a.logger.Warn("failed to upload image ritual thumbnail", zap.String("attachment_id", attachment.ID.String()), zap.Error(err))
		}
		return nil
	}
	return nil
}
