package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

// handlerutils.UploadFileAttachment takes a narrow FileAttachmentUploader
// interface rather than importing this package, because handlerutils → agent
// would close an import cycle through middleware. Nothing declares that this
// type satisfies it, so a signature change here would surface as three
// unrelated-looking errors at the handler call sites instead of one here.
//
// This assertion is the missing link. The dependency direction is still
// provider → handlerutils, which is the safe way round: handlerutils imports
// models, storage, utils and agent/filechunker, none of which reach provider.
var _ handlerutils.FileAttachmentUploader = (*OpenAIProvider)(nil)

// Regex to match sentences containing sandbox:/mnt/data/* links
// This matches markdown links like [text](sandbox:/mnt/data/file.ext) within sentences
var sandboxLinkRegex = regexp.MustCompile(`\[[^\]]*\]\(sandbox:/mnt/data/[^)]+\)`)

// Split text into sentences by looking for sentence-ending punctuation followed by whitespace or end of string
var sentenceRegex = regexp.MustCompile(`([.!?]+)\s+`)

func (a *OpenAIProvider) UploadFileAttachment(ctx context.Context, userID uuid.UUID, attrs map[string]string, file io.Reader, fileName string, fileTypeInfo utils.FileTypeInfo) (string, error) {
	inputFile := openai.File(file, normalizeUploadFileNameExtension(fileName), fileTypeInfo.ContentType)

	storedFile, err := a.oaiClient.Files.New(ctx, openai.FileNewParams{
		File:    inputFile,
		Purpose: openai.FilePurposeUserData,
	})
	if err != nil {
		a.zapLog().Error("failed to upload file attachment", zap.Error(err))
		return "", err
	}

	return storedFile.ID, nil
}

// normalizeUploadFileNameExtension lowercases a filename's extension so files land in the
// OpenAI Files API with an extension its Responses API accepts. OpenAI validates image
// extensions case-sensitively (an uploaded "photo.JPG" is later rejected as an unsupported
// format even though ".jpg" is fine). The base name is left untouched.
func normalizeUploadFileNameExtension(fileName string) string {
	ext := path.Ext(fileName)
	if ext == "" {
		return fileName
	}
	lower := strings.ToLower(ext)
	if lower == ext {
		return fileName
	}
	return fileName[:len(fileName)-len(ext)] + lower
}

func (a *OpenAIProvider) DeleteFileAttachment(ctx context.Context, fileID string) error {
	_, err := a.oaiClient.Files.Delete(ctx, fileID)
	if err != nil {
		a.zapLog().Error("failed to delete file attachment from OpenAI", zap.Error(err))
		return err
	}

	return nil
}

func (a *OpenAIProvider) SaveMessageAttachments(ctx context.Context, userID, chatMessageID uuid.UUID, resp *responses.Response) error {
	for _, output := range resp.Output {
		switch output.Type {
		case "image_generation_call":
			a.saveImageAttachment(ctx, userID, chatMessageID, output.AsImageGenerationCall())
		case "message", "code_interpreter_call":
			a.saveInterpreterAttachments(ctx, userID, chatMessageID, output.Content)
		}
	}

	return nil
}

func (a *OpenAIProvider) saveImageAttachment(ctx context.Context, userID, chatMessageID uuid.UUID, image responses.ResponseOutputItemImageGenerationCall) {
	a.zapLog().Info("image generation call response", zap.Any("extra_fields", image.JSON.ExtraFields))

	imageName := fmt.Sprintf("image_%s.png", image.ID)
	created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		UserID:        userID,
		FileID:        &image.ID,
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

func (a *OpenAIProvider) saveInterpreterAttachments(ctx context.Context, userID, chatMessageID uuid.UUID, content []responses.ResponseOutputMessageContentUnion) {
	for _, contentEntry := range content {
		a.processAnnotations(ctx, userID, chatMessageID, contentEntry.Annotations)
	}
}

func (a *OpenAIProvider) processAnnotations(ctx context.Context, userID, chatMessageID uuid.UUID, annotations []responses.ResponseOutputTextAnnotationUnion) {
	for _, annotation := range annotations {
		switch annotation.Type {
		case "container_file_citation":
			a.saveInterpreterAttachment(ctx, userID, chatMessageID, annotation.AsContainerFileCitation())
		}
	}

}

func (a *OpenAIProvider) saveInterpreterAttachment(ctx context.Context, userID, chatMessageID uuid.UUID, annotation responses.ResponseOutputTextAnnotationContainerFileCitation) {
	resp, err := a.oaiClient.Containers.Files.Content.Get(ctx, annotation.ContainerID, annotation.FileID)
	if err != nil {
		a.zapLog().Error("failed to get container file", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.zapLog().Error("failed to get container file", zap.String("status", resp.Status))
		return
	}

	fileTypeInfo, err := utils.GetFileType(annotation.Filename)
	if err != nil {
		a.zapLog().Error("failed to get file type", zap.Error(err))
		return
	}

	// Read the file bytes directly (no base64 encoding needed).
	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		a.zapLog().Error("failed to read container file", zap.Error(err))
		return
	}

	created, err := a.ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		UserID:        userID,
		FileID:        &annotation.FileID,
		Name:          annotation.Filename,
		FileType:      fileTypeInfo.ContentType,
		ChatMessageID: &chatMessageID,
	})
	if err != nil {
		a.zapLog().Error("failed to create file attachment", zap.Error(err))
		return
	}

	// Non-image attachments from the interpreter are only accessible via OpenAI's
	// Files API (file_id). They are not stored in S3 or the DB file_content column.
	// If fileStore is nil, this is expected for test environments; non-images don't
	// require it. Images, however, require S3 to be served by the gallery.
	if strings.HasPrefix(fileTypeInfo.ContentType, models.ImageMIMEPrefix) {
		if a.fileStore == nil {
			a.zapLog().Error("fileStore is nil; interpreter image cannot be persisted to S3 and will be lost",
				zap.String("attachment_id", created.ID.String()))
			return
		}

		imgKey := storage.FileKeyForImage(userID, created.ID, annotation.Filename)
		if err := a.fileStore.UploadFile(ctx, imgKey, rawBytes, fileTypeInfo.ContentType); err != nil {
			a.zapLog().Error("failed to upload interpreter image to S3; image will not be available in the gallery",
				zap.String("attachment_id", created.ID.String()),
				zap.String("s3_key", imgKey),
				zap.Error(err))
			return
		}

		if err := a.ds.SetFileAttachmentS3Key(ctx, userID, created.ID, imgKey); err != nil {
			a.zapLog().Warn("failed to persist s3_key for interpreter attachment",
				zap.String("attachment_id", created.ID.String()),
				zap.Error(err))
		}

		// Thumbnail is best-effort.
		thumb, thumbErr := imageutil.GenerateThumbnail(rawBytes, imageutil.DefaultThumbnailMaxPx)
		if thumbErr != nil {
			a.zapLog().Warn("failed to generate thumbnail for interpreter image",
				zap.String("attachment_id", created.ID.String()),
				zap.Error(thumbErr))
		} else {
			thumbKey := storage.FileKeyForImageThumbnail(userID, created.ID)
			if err := a.fileStore.UploadFile(ctx, thumbKey, thumb, "image/jpeg"); err != nil {
				a.zapLog().Warn("failed to upload interpreter thumbnail to S3",
					zap.String("attachment_id", created.ID.String()),
					zap.Error(err))
			}
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
