package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

const personalityPortraitImageInstructions = `Render as a single square portrait image: one character, head and shoulders, centered composition, readable face, consistent lighting and style with the description. No text, no grid, no collage, no multiple panels.`

// GeneratePersonalityPortraitImage infers a likeness paragraph from the system prompt and generates one portrait image.
// The attachment is user-owned (no personality_id) for use as a wizard cover before accept.
// Image bytes are stored in S3 only (not file_content) to avoid large DB rows.
//
// imageStyle is an optional style hint (e.g. "anime", "watercolor"). Pass "auto" or "" for the default style.
// Returns an error immediately when imageStyle is models.ImageStyleNone — callers should guard before enqueueing.
func (a *Agent) GeneratePersonalityPortraitImage(ctx context.Context, userID uuid.UUID, systemPrompt, imageStyle string) (*models.FileAttachment, error) {
	if imageStyle == models.ImageStyleNone {
		return nil, fmt.Errorf("portrait generation skipped: image style is %q", models.ImageStyleNone)
	}
	if a == nil || a.ds == nil || a.OpenAIProvider == nil {
		return nil, fmt.Errorf("personality portrait: agent not configured")
	}
	if a.fileStore == nil {
		return nil, fmt.Errorf("personality portrait: file store not configured")
	}

	ctx = telemetry.WithCallPath(ctx, telemetry.CallPathPersonalityPortrait)

	likeness, err := a.inferExpressionGridLikeness(ctx, strings.TrimSpace(systemPrompt), nil, "")
	if err != nil {
		return nil, err
	}
	if likeness == "" {
		likeness = "A distinctive character portrait consistent with the personality."
		a.logger.Warn("personality portrait: empty likeness segment; using fallback prose")
	}

	styleInstructions := personalityPortraitImageInstructions
	if imageStyle != "" && imageStyle != "auto" && imageStyle != "none" {
		styleInstructions += "\n\nArt style: " + imageStyle
	}

	fullPrompt := strings.TrimSpace(likeness) + "\n\n" + styleInstructions
	if len(fullPrompt) > 16000 {
		fullPrompt = fullPrompt[:16000]
	}

	b64PNG, err := a.OpenAIProvider.GenerateImagePNGBase64WithQuality(ctx, fullPrompt, provider.ImageQualityMedium)
	if err != nil {
		return nil, fmt.Errorf("generate portrait image: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(b64PNG)
	if err != nil {
		return nil, fmt.Errorf("decode generated portrait: %w", err)
	}

	name := "personality-portrait.png"
	created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     name,
		FileType: "image/png",
	})
	if err != nil {
		return nil, fmt.Errorf("create portrait attachment: %w", err)
	}
	if created == nil {
		return nil, fmt.Errorf("create portrait attachment: nil model")
	}

	s3Key := storage.FileKeyForImage(userID, created.ID, name)
	if err := a.fileStore.UploadFile(ctx, s3Key, raw, "image/png"); err != nil {
		_ = a.ds.DeleteFileAttachment(ctx, userID, created.ID)
		return nil, fmt.Errorf("upload portrait: %w", err)
	}
	if err := a.ds.SetFileAttachmentS3Key(ctx, userID, created.ID, s3Key); err != nil {
		if delErr := a.fileStore.DeleteFile(ctx, s3Key); delErr != nil {
			a.logger.Warn("personality portrait: failed to delete s3 after key persist failure", zap.Error(delErr))
		}
		_ = a.ds.DeleteFileAttachment(ctx, userID, created.ID)
		return nil, fmt.Errorf("persist portrait s3 key: %w", err)
	}

	if thumb, err := imageutil.GenerateThumbnail(raw, imageutil.DefaultThumbnailMaxPx); err == nil && len(thumb) > 0 {
		thumbKey := storage.FileKeyForImageThumbnail(userID, created.ID)
		if err := a.fileStore.UploadFile(ctx, thumbKey, thumb, "image/jpeg"); err != nil {
			a.logger.Warn("personality portrait: thumbnail upload failed", zap.Error(err))
		}
	}

	return created, nil
}
