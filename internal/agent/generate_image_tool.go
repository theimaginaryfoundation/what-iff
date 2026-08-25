package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type generateImageToolArgs struct {
	Prompt         string  `json:"prompt"`
	Count          *int    `json:"count,omitempty"`
	Quality        *string `json:"quality,omitempty"`
	FilenamePrefix *string `json:"filename_prefix,omitempty"`
}

type generateImageToolImage struct {
	Name     string `json:"name"`
	FileType string `json:"file_type"`
}

type generateImageToolResult struct {
	Success bool                     `json:"success"`
	Prompt  string                   `json:"prompt,omitempty"`
	Quality string                   `json:"quality,omitempty"`
	Count   int                      `json:"count,omitempty"`
	Images  []generateImageToolImage `json:"images,omitempty"`
	Error   string                   `json:"error,omitempty"`
}

func marshalGenerateImageToolResult(result generateImageToolResult) (string, error) {
	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal generate_image result: %w", err)
	}
	return string(out), nil
}

func clampGenerateImageCount(count int) int {
	if count < 1 {
		return 1
	}
	if count > 4 {
		return 4
	}
	return count
}

func parseGenerateImageQuality(raw *string) (provider.ImageQuality, error) {
	if raw == nil {
		return provider.ImageQualityLow, nil
	}
	q := strings.ToLower(strings.TrimSpace(*raw))
	if q == "" {
		return provider.ImageQualityLow, nil
	}
	switch q {
	case string(provider.ImageQualityLow):
		return provider.ImageQualityLow, nil
	case string(provider.ImageQualityMedium):
		return provider.ImageQualityMedium, nil
	case string(provider.ImageQualityHigh):
		return provider.ImageQualityHigh, nil
	default:
		return provider.ImageQualityLow, fmt.Errorf("invalid quality %q (expected low|medium|high)", q)
	}
}

func (a *Agent) generateImageTool(ctx context.Context, chat *models.Chat, args []byte) (string, []*models.FileAttachment, error) {
	var toolArgs generateImageToolArgs
	if err := json.Unmarshal(args, &toolArgs); err != nil {
		out, merr := marshalGenerateImageToolResult(generateImageToolResult{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		})
		if merr != nil {
			a.logger.Error("failed to marshal generate_image invalid-args result", zap.Error(merr))
			return "", nil, merr
		}
		return out, nil, nil
	}

	prompt := strings.TrimSpace(toolArgs.Prompt)
	if prompt == "" {
		out, merr := marshalGenerateImageToolResult(generateImageToolResult{
			Success: false,
			Error:   "prompt is required",
		})
		if merr != nil {
			a.logger.Error("failed to marshal generate_image empty-prompt result", zap.Error(merr))
			return "", nil, merr
		}
		return out, nil, nil
	}

	count := 1
	if toolArgs.Count != nil {
		count = *toolArgs.Count
	}
	count = clampGenerateImageCount(count)

	quality, qErr := parseGenerateImageQuality(toolArgs.Quality)
	if qErr != nil {
		out, merr := marshalGenerateImageToolResult(generateImageToolResult{
			Success: false,
			Error:   qErr.Error(),
		})
		if merr != nil {
			a.logger.Error("failed to marshal generate_image invalid-quality result", zap.Error(merr))
			return "", nil, merr
		}
		return out, nil, nil
	}

	prefix := "image"
	if toolArgs.FilenamePrefix != nil {
		if trimmed := strings.TrimSpace(*toolArgs.FilenamePrefix); trimmed != "" {
			trimmed = strings.ReplaceAll(trimmed, " ", "_")
			trimmed = strings.ReplaceAll(trimmed, "/", "_")
			trimmed = strings.ReplaceAll(trimmed, "\\", "_")
			prefix = trimmed
		}
	}

	b64s := make([]string, count)
	errs := make([]error, count)

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			b64, err := a.OpenAIProvider.GenerateImagePNGBase64WithQuality(ctx, prompt, quality)
			if err != nil {
				errs[i] = err
				return
			}
			b64s[i] = b64
		}()
	}
	wg.Wait()

	successCount := 0
	failCount := 0
	var firstErr error
	for i := 0; i < count; i++ {
		if errs[i] != nil {
			failCount++
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		if strings.TrimSpace(b64s[i]) == "" {
			// Treat empty payload as failure (shouldn't happen, but be defensive).
			failCount++
			if firstErr == nil {
				firstErr = fmt.Errorf("empty image payload")
			}
			continue
		}
		successCount++
	}

	if successCount == 0 {
		if violation, ok := provider.ParseSafetyViolationError(firstErr); ok {
			eventInput := models.CreateSafetyViolationEventInput{
				OccurredAt:      time.Now(),
				Provider:        violation.Provider,
				ViolationType:   violation.ViolationType,
				ProviderCode:    violation.ProviderCode,
				ProviderMessage: violation.ProviderMessage,
				RawError:        violation.RawError,
				UserID:          chat.UserID,
				ChatID:          &chat.ID,
				ChatName:        chat.Name,
				ChatMessageText: prompt,
			}
			if _, err := a.ds.CreateSafetyViolationEvent(ctx, eventInput); err != nil {
				a.logger.Error("failed to persist image-tool safety violation event",
					zap.String("user_id", chat.UserID.String()),
					zap.String("chat_id", chat.ID.String()),
					zap.Error(err),
				)
			}
			out, merr := marshalGenerateImageToolResult(generateImageToolResult{
				Success: false,
				Error:   safetyViolationAssistantMessage,
			})
			if merr != nil {
				a.logger.Error("failed to marshal generate_image safety-violation result", zap.Error(merr))
				return "", nil, merr
			}
			return out, nil, nil
		}

		a.logger.Error("failed to generate any images via tool",
			zap.String("user_id", chat.UserID.String()),
			zap.String("chat_id", chat.ID.String()),
			zap.Int("count", count),
			zap.String("quality", string(quality)),
			zap.Error(firstErr),
		)
		// Operational failure: surface as a tool error so the agent loop treats it as a failed tool call.
		return "", nil, fmt.Errorf("failed to generate image: %w", firstErr)
	}

	// Record usage for each successfully generated image (fire-and-forget). The
	// recorder is nil in builds without a metering implementation (the open-source
	// build), so nothing is recorded there.
	if recordImageGenerationUsage != nil {
		for i := 0; i < successCount; i++ {
			recordImageGenerationUsage(ctx, a.ds, chat.UserID, string(quality), provider.ImageEngine, chat.ID.String(), i+1)
		}
	}

	attachments := make([]*models.FileAttachment, 0, successCount)
	imagesMeta := make([]generateImageToolImage, 0, successCount)
	seq := 0
	for i := 0; i < count; i++ {
		if errs[i] != nil || strings.TrimSpace(b64s[i]) == "" {
			continue
		}
		seq++
		name := fmt.Sprintf("%s_%s.png", prefix, uuid.NewString())
		if count > 1 {
			name = fmt.Sprintf("%s_%02d_%s.png", prefix, seq, uuid.NewString())
		}
		attachments = append(attachments, &models.FileAttachment{
			UserID:      chat.UserID,
			Name:        name,
			FileType:    "image/png",
			FileContent: b64s[i],
			// ChatMessageID intentionally omitted: it will be set when the assistant message is persisted.
		})
		imagesMeta = append(imagesMeta, generateImageToolImage{
			Name:     name,
			FileType: "image/png",
		})
	}

	toolErrText := ""
	if failCount > 0 && firstErr != nil {
		toolErrText = fmt.Sprintf("generated %d/%d images; some requests failed: %s", successCount, count, firstErr.Error())
		a.logger.Warn("partial image generation via tool",
			zap.String("user_id", chat.UserID.String()),
			zap.String("chat_id", chat.ID.String()),
			zap.Int("requested", count),
			zap.Int("generated", successCount),
			zap.String("quality", string(quality)),
			zap.Error(firstErr),
		)
	}

	out, err := marshalGenerateImageToolResult(generateImageToolResult{
		Success: true,
		Prompt:  prompt,
		Quality: string(quality),
		Count:   count,
		Images:  imagesMeta,
		Error:   toolErrText,
	})
	if err != nil {
		a.logger.Error("failed to marshal generate_image success result", zap.Error(err))
		return "", nil, err
	}
	return out, attachments, nil
}
