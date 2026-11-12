package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Regex to match sentences containing sandbox:/mnt/data/* links
// This matches markdown links like [text](sandbox:/mnt/data/file.ext) within sentences
var sandboxLinkRegex = regexp.MustCompile(`\[[^\]]*\]\(sandbox:/mnt/data/[^)]+\)`)

// Split text into sentences by looking for sentence-ending punctuation followed by whitespace or end of string
var sentenceRegex = regexp.MustCompile(`([.!?]+)\s+`)

func (a *OpenAIProvider) SaveMessageAttachments(ctx context.Context, userID, chatMessageID uuid.UUID, resp *responses.Response) error {
	for _, output := range resp.Output {
		switch output.Type {
		case "image_generation_call":
			a.saveImageAttachment(ctx, userID, chatMessageID, output.AsImageGenerationCall())
		}
	}

	return nil
}

func (a *OpenAIProvider) saveImageAttachment(ctx context.Context, userID, chatMessageID uuid.UUID, image responses.ResponseOutputItemImageGenerationCall) {
	a.zapLog().Info("image generation call response", zap.Any("extra_fields", image.JSON.ExtraFields))

	imageName := fmt.Sprintf("image_%s.png", image.ID)
	created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		UserID:        userID,
		Name:          imageName,
		FileType:      "image/png",
		ChatMessageID: &chatMessageID,
	})
	if err != nil {
		a.zapLog().Error("failed to create file attachment", zap.Error(err))
		return
	}

	// Decode the base64 image result and upload to S3 so the gallery can serve it.
	rawBytes, decodeErr := base64.StdEncoding.DecodeString(image.Result)
	if decodeErr != nil || len(rawBytes) == 0 {
		a.zapLog().Error("failed to decode image result from OpenAI", zap.Error(decodeErr))
		return
	}

	if a.fileStore == nil {
		a.zapLog().Error("fileStore is nil; generated image cannot be persisted to S3 and will be lost",
			zap.String("attachment_id", created.ID.String()))
		return
	}

	// Stored compact, like every generated image (ADR 0x047 §9).
	rawBytes = imageutil.CompactPNGForStorage(rawBytes)
	imgKey := storage.FileKeyForImage(userID, created.ID, imageName)
	if err := a.fileStore.UploadFile(ctx, imgKey, rawBytes, "image/png"); err != nil {
		a.zapLog().Error("failed to upload generated image to S3; image will not be available in the gallery",
			zap.String("attachment_id", created.ID.String()),
			zap.String("s3_key", imgKey),
			zap.Error(err))
		return
	}

	if err := a.ds.SetFileAttachmentS3Key(ctx, userID, created.ID, imgKey); err != nil {
		a.zapLog().Warn("failed to persist attachment s3_key after image save",
			zap.String("attachment_id", created.ID.String()),
			zap.String("s3_key", imgKey),
			zap.Error(err))
	}

	// Thumbnail is best-effort; failure never blocks the attachment record.
	thumb, thumbErr := imageutil.GenerateThumbnail(rawBytes, imageutil.DefaultThumbnailMaxPx)
	if thumbErr != nil {
		a.zapLog().Warn("failed to generate thumbnail for generated image",
			zap.String("attachment_id", created.ID.String()),
			zap.Error(thumbErr))
	} else {
		thumbKey := storage.FileKeyForImageThumbnail(userID, created.ID)
		if err := a.fileStore.UploadFile(ctx, thumbKey, thumb, "image/jpeg"); err != nil {
			a.zapLog().Warn("failed to upload thumbnail to S3",
				zap.String("attachment_id", created.ID.String()),
				zap.Error(err))
		}
	}
}

// StripOpenAIFileLinks removes sentences containing OpenAI sandbox file download links
// from the provided text. This prevents users from seeing broken sandbox:/mnt/data/* links
// since we create our own FileAttachments for user access.
// The function preserves newlines and markdown formatting.
func StripOpenAIFileLinks(text string) string {
	if text == "" {
		return text
	}

	// Find all sentence boundaries
	matches := sentenceRegex.FindAllStringIndex(text, -1)

	if len(matches) == 0 {
		// No sentence separators found, treat entire text as one sentence
		if sandboxLinkRegex.MatchString(text) {
			return ""
		}
		return text
	}

	var result strings.Builder
	lastEnd := 0

	for _, match := range matches {
		// Extract sentence with its surrounding context (preserve original spacing)
		sentenceStart := lastEnd
		sentenceEnd := match[0]
		puncEnd := match[1]

		// Get the sentence content (before punctuation)
		sentenceContent := text[sentenceStart:sentenceEnd]

		// Check if this sentence contains a sandbox link
		if !sandboxLinkRegex.MatchString(sentenceContent) {
			// Keep the sentence with its original spacing and punctuation
			result.WriteString(text[sentenceStart:puncEnd])
		}

		lastEnd = puncEnd
	}

	// Handle the last sentence (after the last punctuation)
	if lastEnd < len(text) {
		lastSentence := text[lastEnd:]
		if !sandboxLinkRegex.MatchString(lastSentence) {
			result.WriteString(lastSentence)
		}
	}

	output := result.String()

	// Normalize multiple spaces to single spaces, but preserve newlines for markdown
	// This regex matches 2+ spaces or tabs (but not newlines)
	output = regexp.MustCompile(`[ \t]+`).ReplaceAllString(output, " ")

	return strings.TrimSpace(output)
}
