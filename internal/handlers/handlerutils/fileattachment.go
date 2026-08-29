package handlerutils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

// UploadToS3 synchronously archives the temp file to S3 under s3Key.
// Returns nil if fileStore is nil or s3Key is empty (no-op / S3 disabled).
// The caller is responsible for rolling back any DB state on non-nil error.
func UploadToS3(ctx context.Context, fileStore storage.FileStore, s3Key, tempFilePath, contentType string) error {
	if fileStore == nil || s3Key == "" {
		return nil
	}
	content, err := os.ReadFile(tempFilePath)
	if err != nil {
		return fmt.Errorf("reading temp file for S3 upload: %w", err)
	}
	return fileStore.UploadFile(ctx, s3Key, content, contentType)
}

const (
	maxUploadMb             = 30
	maxUploadBytes          = maxUploadMb << 20
	maxAsyncProcessingBytes = 16 << 20 // keep goroutine memory bounded
)

// FileAttachmentUploader is the single capability this package needs from the
// agent layer. It is declared here, rather than taking an *agent.Agent, because
// importing internal/agent would close an import cycle:
//
//	handlerutils → agent → middleware → handlerutils
//
// That cycle is what previously stopped internal/middleware from using the
// shared response helpers at all. Keeping this package leaf-level is what lets
// anything in the HTTP layer reach RespondWithError.
//
// *provider.OpenAIProvider satisfies this structurally; callers pass
// agent.OpenAIProvider. That relationship is pinned by a compile-time assertion
// next to the implementation — `var _ handlerutils.FileAttachmentUploader` in
// internal/agent/provider/fileattachment.go — so provider-side drift fails there
// rather than as three unrelated errors at the handler call sites. The assertion
// has to live on that side; declaring it here would need the import this
// interface exists to avoid.
type FileAttachmentUploader interface {
	UploadFileAttachment(
		ctx context.Context,
		userID uuid.UUID,
		attrs map[string]string,
		file io.Reader,
		fileName string,
		fileTypeInfo utils.FileTypeInfo,
	) (string, error)
}

// UploadFileAttachment parses a multipart file upload, validates the file type,
// streams it to a temp file, normalizes image uploads, uploads from disk, and
// returns the attachment model plus temp file path for optional async chunking.
func UploadFileAttachment(w http.ResponseWriter, r *http.Request, logger *zap.Logger, a FileAttachmentUploader, userID uuid.UUID, attrs map[string]string) (models.FileAttachment, string, error) {
	// Validate file size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		RespondWithError(w, logger, http.StatusBadRequest, CodeNotSet, fmt.Sprintf("File too large (max %dMB)", maxUploadMb), err)
		return models.FileAttachment{}, "", err
	}

	// Get the file from the request
	file, header, err := r.FormFile("attachment")
	if err != nil {
		RespondWithError(w, logger, http.StatusBadRequest, CodeNotSet, "Error retrieving file", err)
		return models.FileAttachment{}, "", err
	}
	defer file.Close()

	// Check file extension
	fileName := header.Filename
	fileTypeInfo, err := utils.GetFileType(fileName)
	if err != nil {
		RespondWithError(w, logger, http.StatusBadRequest, CodeNotSet, "Unsupported file type", err)
		return models.FileAttachment{}, "", err
	}

	tempFile, err := os.CreateTemp("", "chat-app-upload-*")
	if err != nil {
		RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error creating temp file", err)
		return models.FileAttachment{}, "", err
	}
	tempFilePath := tempFile.Name()

	if _, err := io.Copy(tempFile, file); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFilePath)
		RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error buffering file", err)
		return models.FileAttachment{}, "", err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempFilePath)
		RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error closing temp file", err)
		return models.FileAttachment{}, "", err
	}

	if strings.HasPrefix(fileTypeInfo.ContentType, models.ImageMIMEPrefix) {
		imageBytes, err := os.ReadFile(tempFilePath)
		if err != nil {
			_ = os.Remove(tempFilePath)
			RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error reading image upload", err)
			return models.FileAttachment{}, "", err
		}

		normalized, err := imageutil.NormalizeForUpload(imageBytes, imageutil.DefaultUploadImageMaxPx)
		if err != nil {
			_ = os.Remove(tempFilePath)
			RespondWithError(w, logger, http.StatusBadRequest, CodeNotSet, "Invalid image", err)
			return models.FileAttachment{}, "", err
		}
		if err := os.WriteFile(tempFilePath, normalized, 0o600); err != nil {
			_ = os.Remove(tempFilePath)
			RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error normalizing image", err)
			return models.FileAttachment{}, "", err
		}

		ext := filepath.Ext(fileName)
		fileName = strings.TrimSuffix(fileName, ext) + ".png"
		fileTypeInfo, err = utils.GetFileType(fileName)
		if err != nil {
			_ = os.Remove(tempFilePath)
			RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error resolving normalized image type", err)
			return models.FileAttachment{}, "", err
		}
	}

	uploadReader, err := os.Open(tempFilePath)
	if err != nil {
		_ = os.Remove(tempFilePath)
		RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error opening temp file", err)
		return models.FileAttachment{}, "", err
	}
	defer uploadReader.Close()

	fileId, err := a.UploadFileAttachment(r.Context(), userID, attrs, uploadReader, fileName, fileTypeInfo)
	if err != nil {
		_ = os.Remove(tempFilePath)
		RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error uploading file", err)
		return models.FileAttachment{}, "", err
	}

	return models.FileAttachment{
		UserID:   userID,
		FileID:   &fileId,
		Name:     fileName,
		FileType: fileTypeInfo.ContentType,
	}, tempFilePath, nil
}

// TriggerAsyncFileChunking spawns a goroutine that chunks and embeds eligible text files.
// Skips silently for non-text / non-vector-support file types. Caller owns tempFilePath cleanup
// on early return; the goroutine removes it on completion or error.
// S3 archival must be completed synchronously by the caller before invoking this.
func TriggerAsyncFileChunking(
	logger *zap.Logger,
	pipeline *filechunker.FileChunkPipeline,
	attachmentID uuid.UUID,
	tempFilePath string,
	fileName string,
) {
	if tempFilePath == "" {
		return
	}

	if info, err := os.Stat(tempFilePath); err == nil {
		if info.Size() > maxAsyncProcessingBytes {
			_ = os.Remove(tempFilePath)
			logger.Warn("skipping async file chunking: upload exceeds size limit",
				zap.String("file_attachment_id", attachmentID.String()),
				zap.String("file_name", fileName),
				zap.Int64("size_bytes", info.Size()),
				zap.Int("max_bytes", maxAsyncProcessingBytes))
			return
		}
	} else {
		logger.Warn("failed to stat temp upload file before async chunking",
			zap.Error(err),
			zap.String("temp_file_path", tempFilePath),
			zap.String("file_attachment_id", attachmentID.String()),
			zap.String("file_name", fileName))
	}

	fileTypeInfo, err := utils.GetFileType(fileName)
	if err != nil {
		_ = os.Remove(tempFilePath)
		logger.Warn("failed to detect file type for chunking",
			zap.Error(err),
			zap.String("file_name", fileName))
		return
	}

	if !fileTypeInfo.VectorSupport {
		_ = os.Remove(tempFilePath)
		return
	}

	if !filechunker.IsTextType(fileTypeInfo.ContentType) &&
		!filechunker.IsTextFileByExtension(fileName) {
		_ = os.Remove(tempFilePath)
		return
	}

	go func() {
		defer func() {
			if err := os.Remove(tempFilePath); err != nil && !os.IsNotExist(err) {
				logger.Warn("failed to remove temp upload file",
					zap.Error(err),
					zap.String("temp_file_path", tempFilePath))
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in async file chunking recovered",
					zap.Any("panic", r),
					zap.String("file_attachment_id", attachmentID.String()),
					zap.String("file_name", fileName))
			}
		}()

		info, err := os.Stat(tempFilePath)
		if err != nil {
			logger.Error("failed to stat temp upload file",
				zap.Error(err),
				zap.String("temp_file_path", tempFilePath),
				zap.String("file_attachment_id", attachmentID.String()))
			return
		}
		if info.Size() > maxUploadBytes {
			logger.Warn("skipping async processing: file exceeds size cap",
				zap.Int64("size_bytes", info.Size()),
				zap.Int("cap_mb", maxUploadMb),
				zap.String("file_name", fileName),
				zap.String("file_attachment_id", attachmentID.String()))
			return
		}

		fileContent, err := os.ReadFile(tempFilePath)
		if err != nil {
			logger.Error("failed to read temp upload file for chunking",
				zap.Error(err),
				zap.String("temp_file_path", tempFilePath),
				zap.String("file_name", fileName),
				zap.String("file_attachment_id", attachmentID.String()))
			return
		}

		if err := pipeline.ProcessAndStore(context.Background(), attachmentID, fileContent, fileName, fileTypeInfo.ContentType); err != nil {
			logger.Error("async file chunking failed",
				zap.Error(err),
				zap.String("file_attachment_id", attachmentID.String()))
		}
	}()
}
