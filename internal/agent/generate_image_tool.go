package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type generateImageToolArgs struct {
	Prompt         string  `json:"prompt"`
	Count          *int    `json:"count,omitempty"`
	Quality        *string `json:"quality,omitempty"`
	AspectRatio    *string `json:"aspect_ratio,omitempty"`
	FilenamePrefix *string `json:"filename_prefix,omitempty"`
	Folder         *string `json:"folder,omitempty"`
	// ReferenceImages and MaskImage make the request an edit (ADR 0x045).
	ReferenceImages []generateImageReference `json:"reference_images,omitempty"`
	MaskImage       string                   `json:"mask_image,omitempty"`
}

type generateImageToolImage struct {
	Name     string `json:"name"`
	FileType string `json:"file_type"`
}

type generateImageToolResult struct {
	Success     bool   `json:"success"`
	Prompt      string `json:"prompt,omitempty"`
	Quality     string `json:"quality,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Folder      string `json:"folder,omitempty"`
	Count       int    `json:"count,omitempty"`
	// Provider is the vendor that made the images; ReferenceImages how many input images it had.
	Provider        string                   `json:"provider,omitempty"`
	ReferenceImages int                      `json:"reference_images,omitempty"`
	Images          []generateImageToolImage `json:"images,omitempty"`
	Error           string                   `json:"error,omitempty"`
}

func marshalGenerateImageToolResult(result generateImageToolResult) (string, error) {
	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal generate_image result: %w", err)
	}
	return string(out), nil
}

// generateImageFailure is a refused request: a result the model reads, not a failed tool call.
func (a *Agent) generateImageFailure(reason string) (string, []*models.FileAttachment, error) {
	out, err := marshalGenerateImageToolResult(generateImageToolResult{Success: false, Error: reason})
	if err != nil {
		a.logger.Error("failed to marshal generate_image refusal", zap.Error(err))
		return "", nil, err
	}
	return out, nil, nil
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

// parseGenerateImageFolder normalizes the optional gallery folder; nil or blank is the top level.
func parseGenerateImageFolder(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}
	folder, err := models.NormalizeFolder(*raw)
	if err != nil {
		return "", fmt.Errorf("invalid folder: %w", err)
	}
	if models.IsReservedFolder(folder) {
		// Checked before the image is generated; the datastore refuses it too.
		return "", models.ErrReservedFolder
	}
	return folder, nil
}

func parseGenerateImageAspectRatio(raw *string) (provider.ImageAspectRatio, error) {
	if raw == nil {
		return provider.ImageAspectRatioSquare, nil
	}
	ar := strings.ToLower(strings.TrimSpace(*raw))
	if ar == "" {
		return provider.ImageAspectRatioSquare, nil
	}
	switch ar {
	case string(provider.ImageAspectRatioSquare):
		return provider.ImageAspectRatioSquare, nil
	case string(provider.ImageAspectRatioLandscape):
		return provider.ImageAspectRatioLandscape, nil
	case string(provider.ImageAspectRatioPortrait):
		return provider.ImageAspectRatioPortrait, nil
	default:
		return provider.ImageAspectRatioSquare, fmt.Errorf("invalid aspect_ratio %q (expected square|landscape|portrait)", ar)
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

	editing := len(toolArgs.ReferenceImages) > 0 || strings.TrimSpace(toolArgs.MaskImage) != ""
	aspectRatio, arErr := parseGenerateImageAspectRatio(toolArgs.AspectRatio)
	if editing && (toolArgs.AspectRatio == nil || strings.TrimSpace(*toolArgs.AspectRatio) == "") {
		// An edit keeps its base image's shape unless asked otherwise.
		aspectRatio = ""
	}
	if arErr != nil {
		out, merr := marshalGenerateImageToolResult(generateImageToolResult{
			Success: false,
			Error:   arErr.Error(),
		})
		if merr != nil {
			a.logger.Error("failed to marshal generate_image invalid-aspect-ratio result", zap.Error(merr))
			return "", nil, merr
		}
		return out, nil, nil
	}

	folder, fErr := parseGenerateImageFolder(toolArgs.Folder)
	if fErr != nil {
		out, merr := marshalGenerateImageToolResult(generateImageToolResult{
			Success: false,
			Error:   fErr.Error(),
		})
		if merr != nil {
			a.logger.Error("failed to marshal generate_image invalid-folder result", zap.Error(merr))
			return "", nil, merr
		}
		return out, nil, nil
	}

	// Input images are found and checked, and the vendor asked whether it edits, before the quota
	// gate: a request that cannot run should not count against it.
	var editor provider.ImageEditor
	var inputs []provider.ImageInput
	var mask []byte
	if editing {
		var err error
		if editor, err = a.imageEditorForUser(ctx, chat.UserID); err != nil {
			if errors.Is(err, errNoImageGenerator) {
				return "", nil, err
			}
			return a.generateImageFailure(err.Error())
		}
		if inputs, mask, err = a.imageInputs(ctx, chat, toolArgs.ReferenceImages, toolArgs.MaskImage); err != nil {
			return a.generateImageFailure(err.Error())
		}
	}

	// Quota gate. The images are billed per image once they exist, but the gate runs
	// first so a user with no credits left does not get free generations. Like the
	// turn-level gate it is fuzzy; the meter's atomic accounting at Record time is
	// the precise enforcer. The tool is not told which tier the chat model is, and
	// the gate ignores it for non-chat actions.
	var qd metering.Decision
	if a.meter != nil {
		qd = a.meter.Check(ctx, chat.UserID, "", models.ActionTypeImageGeneration)
		if !qd.Allowed {
			a.logger.Warn("generate_image rejected by quota check", zap.String("user_id", chat.UserID.String()))
			a.recordQuotaRejection(ctx)
			out, merr := marshalGenerateImageToolResult(generateImageToolResult{
				Success: false,
				Error:   "not enough credits to generate images",
			})
			if merr != nil {
				a.logger.Error("failed to marshal generate_image quota result", zap.Error(merr))
				return "", nil, merr
			}
			return out, nil, nil
		}
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

	var generator provider.ImageGenerator = editor
	generate := func(ctx context.Context) (provider.GeneratedImage, error) {
		return editor.EditImage(ctx, provider.ImageEditRequest{Prompt: prompt, Quality: quality, AspectRatio: aspectRatio, Images: inputs, Mask: mask})
	}
	if !editing {
		var err error
		if generator, err = a.imageGeneratorForUser(ctx, chat.UserID); err != nil {
			return "", nil, err
		}
		generate = func(ctx context.Context) (provider.GeneratedImage, error) {
			return generator.GenerateImage(ctx, prompt, quality, aspectRatio)
		}
	}

	images := make([]provider.GeneratedImage, count)
	errs := make([]error, count)

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			img, err := generate(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			images[i] = img
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
		if strings.TrimSpace(images[i].Base64) == "" {
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
		if violation, ok := imageSafetyViolationFor(generator, firstErr); ok {
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

	// Record usage for each successfully generated image (fire-and-forget). With no
	// metering implementation linked (the open-source build) the meter is the no-op
	// one and nothing is tracked.
	path := "tool"
	if editing {
		path = "tool_edit"
	}
	if a.meter != nil {
		for i := 0; i < successCount; i++ {
			a.meter.Record(ctx, qd, metering.Usage{
				UserID:        chat.UserID,
				ActionType:    models.ActionTypeImageGeneration,
				ChatID:        chat.ID.String(),
				ImageQuality:  string(quality),
				ImageProvider: string(generator.Provider()),
				Source:        metering.TurnSourceFromContext(ctx),
				Metadata: map[string]interface{}{
					"path":        path,
					"engine":      generator.Model(),
					"provider":    string(generator.Provider()),
					"image_index": i + 1,
				},
			})
		}
	}

	attachments := make([]*models.FileAttachment, 0, successCount)
	imagesMeta := make([]generateImageToolImage, 0, successCount)
	seq := 0
	for i := 0; i < count; i++ {
		if errs[i] != nil || strings.TrimSpace(images[i].Base64) == "" {
			continue
		}
		seq++
		fileType, ext := generatedImageFileType(images[i].MIMEType)
		name := fmt.Sprintf("%s_%s%s", prefix, uuid.NewString(), ext)
		if count > 1 {
			name = fmt.Sprintf("%s_%02d_%s%s", prefix, seq, uuid.NewString(), ext)
		}
		attachments = append(attachments, &models.FileAttachment{
			UserID:      chat.UserID,
			Name:        name,
			FileType:    fileType,
			FileContent: images[i].Base64,
			Thumbnail:   a.generatedImageThumbnail(images[i].Base64),
			Folder:      folder,
			// ChatMessageID intentionally omitted: it will be set when the assistant message is persisted.
		})
		imagesMeta = append(imagesMeta, generateImageToolImage{
			Name:     name,
			FileType: fileType,
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
		Success:         true,
		Prompt:          prompt,
		Quality:         string(quality),
		AspectRatio:     resultAspectRatio(aspectRatio),
		Folder:          folder,
		Count:           count,
		Provider:        string(generator.Provider()),
		ReferenceImages: len(inputs),
		Images:          imagesMeta,
		Error:           toolErrText,
	})
	if err != nil {
		a.logger.Error("failed to marshal generate_image success result", zap.Error(err))
		return "", nil, err
	}
	return out, attachments, nil
}

// generatedImageThumbnail returns the gallery thumbnail of a generated image, made once here so
// the model is shown it for the rest of the turn and the save stores it (see
// models.FileAttachment.Thumbnail). It returns nil when the image cannot be decoded, which leaves
// both to their full-size fallbacks.
func (a *Agent) generatedImageThumbnail(b64 string) []byte {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err == nil {
		var thumb []byte
		if thumb, err = imageutil.GenerateThumbnail(raw, imageutil.DefaultThumbnailMaxPx); err == nil {
			return thumb
		}
	}
	a.logger.Warn("generate_image: could not make a thumbnail of a generated image", zap.Error(err))
	return nil
}

// imageSafetyViolationFor reports whether err is the generator's vendor refusing
// the prompt on content grounds, attributed to that vendor.
func imageSafetyViolationFor(generator provider.ImageGenerator, err error) (*provider.SafetyViolation, bool) {
	var typed *provider.SafetyViolationError
	if errors.As(err, &typed) && typed.Violation != nil {
		return typed.Violation, true
	}
	return provider.ParseSafetyViolationErrorFor(imageSafetyProvider(generator.Provider()), err)
}

// generatedImageFileTypes are the formats image vendors return, with the file
// extension each is saved under.
var generatedImageFileTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// generatedImageFileType normalises a generated image's MIME type and returns
// its extension, treating anything unrecognised as the PNG every vendor
// defaulted to before images carried a type.
func generatedImageFileType(mime string) (string, string) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if ext, ok := generatedImageFileTypes[mime]; ok {
		return mime, ext
	}
	return "image/png", ".png"
}

// resultAspectRatio reports the shape asked for; an edit that followed its base image says so.
func resultAspectRatio(ar provider.ImageAspectRatio) string {
	if ar == "" {
		return "same_as_base"
	}
	return string(ar)
}
