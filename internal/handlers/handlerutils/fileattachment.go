package handlerutils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

// UploadToS3 synchronously archives the temp file to S3 under s3Key.
// Returns nil if fileStore is nil or s3Key is empty (no-op / S3 disabled).
// The caller is responsible for rolling back any DB state on non-nil error.
// A failure is counted on FileUploads, because every caller abandons the upload when it fails.
func UploadToS3(ctx context.Context, fileStore storage.FileStore, s3Key, tempFilePath, contentType string) (err error) {
	if fileStore == nil || s3Key == "" {
		return nil
	}
	defer func() {
		if err != nil {
			telemetry.Global().RecordFileUpload(ctx, contentType, telemetry.FileUploadFailure)
		}
	}()
	content, err := os.ReadFile(tempFilePath)
	if err != nil {
		return fmt.Errorf("reading temp file for S3 upload: %w", err)
	}
	return fileStore.UploadFile(ctx, s3Key, content, contentType)
}

const (
	maxUploadMb = 30
	// MaxUploadBytes is the largest file an upload path accepts. Callers that already hold the
	// bytes in memory (plugins) check it up front so an oversized file is rejected before it is
	// copied anywhere.
	MaxUploadBytes          = maxUploadMb << 20
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

// FileAttachmentDeleter deletes a file from the provider's Files API by the FileID that
// FileAttachmentUploader returned. Like FileAttachmentUploader it is declared here to keep this
// package leaf-level; *provider.OpenAIProvider satisfies it, pinned by an assertion next to the
// implementation in internal/agent/provider/fileattachment.go.
type FileAttachmentDeleter interface {
	DeleteFileAttachment(ctx context.Context, fileID string) error
}

// providerFileCleanupTimeout bounds the best-effort provider delete in DeleteProviderFile.
const providerFileCleanupTimeout = 30 * time.Second

// DeleteProviderFile best-effort deletes the provider-side copy of an upload whose attachment
// record will not be kept (it failed to save, or was rolled back), so the file is not left
// orphaned in the provider's storage. It detaches from ctx's cancellation, since the failure that
// triggers it is often the request context ending. Failures are logged, never returned; a nil
// logger logs nothing, so cleanup can't fail on it.
func DeleteProviderFile(ctx context.Context, logger *zap.Logger, d FileAttachmentDeleter, fileID *string) {
	if d == nil || fileID == nil || *fileID == "" {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerFileCleanupTimeout)
	defer cancel()
	if err := d.DeleteFileAttachment(ctx, *fileID); err != nil {
		logger.Warn("failed to delete orphaned provider file",
			zap.Error(err),
			zap.String("file_id", *fileID))
	}
}

// AbandonFileAttachmentUpload cleans up after UploadFileAttachment succeeded but the attachment
// record could not be saved: it deletes the provider-side file (best effort) and the temp file,
// and counts the upload as failed on FileUploads, since TriggerAsyncFileChunking will not run.
func AbandonFileAttachmentUpload(ctx context.Context, logger *zap.Logger, d FileAttachmentDeleter, attachment models.FileAttachment, tempFilePath string) {
	DeleteProviderFile(ctx, logger, d, attachment.FileID)
	if tempFilePath != "" {
		_ = os.Remove(tempFilePath)
	}
	telemetry.Global().RecordFileUpload(ctx, attachment.FileType, telemetry.FileUploadFailure)
}

// ErrUnsupportedFileType and ErrFileTooLarge are what an UploadError wraps when a file is
// rejected for its type or size, so a caller outside the HTTP layer can tell them apart.
var (
	ErrUnsupportedFileType = errors.New("unsupported file type")
	ErrFileTooLarge        = fmt.Errorf("file too large (max %dMB)", maxUploadMb)
)

// UploadError is why an upload was rejected, in the terms the HTTP layer reports it: the status
// and the message a client sees. Err is the underlying cause, for logs and errors.Is.
type UploadError struct {
	Status  int
	Message string
	Err     error
}

func (e *UploadError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *UploadError) Unwrap() error { return e.Err }

func uploadErr(status int, message string, err error) *UploadError {
	return &UploadError{Status: status, Message: message, Err: err}
}

// RespondWithUploadError writes err as the response: an UploadError keeps its status and message,
// anything else is a 500.
func RespondWithUploadError(w http.ResponseWriter, logger *zap.Logger, err error) {
	var ue *UploadError
	if errors.As(err, &ue) {
		RespondWithError(w, logger, ue.Status, CodeNotSet, ue.Message, ue.Err)
		return
	}
	RespondWithError(w, logger, http.StatusInternalServerError, CodeNotSet, "Error uploading file", err)
}

// UploadFileAttachment parses a multipart file upload and runs it through ProcessUpload. It
// returns the attachment model plus temp file path for optional async chunking, and has already
// written the error response when it returns one.
//
// Upload metrics: any failure here counts as a failed upload on FileUploads; success is counted
// later by TriggerAsyncFileChunking, which every upload path calls once the attachment is stored,
// so each upload is counted exactly once. A caller that fails to store the attachment calls
// AbandonFileAttachmentUpload instead, which counts the failure and deletes the provider file.
func UploadFileAttachment(w http.ResponseWriter, r *http.Request, logger *zap.Logger, a FileAttachmentUploader, userID uuid.UUID, attrs map[string]string) (models.FileAttachment, string, error) {
	// Validate file size
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
		telemetry.Global().RecordFileUpload(r.Context(), "", telemetry.FileUploadFailure)
		RespondWithError(w, logger, http.StatusBadRequest, CodeNotSet, fmt.Sprintf("File too large (max %dMB)", maxUploadMb), err)
		return models.FileAttachment{}, "", err
	}

	// Get the file from the request
	file, header, err := r.FormFile("attachment")
	if err != nil {
		telemetry.Global().RecordFileUpload(r.Context(), "", telemetry.FileUploadFailure)
		RespondWithError(w, logger, http.StatusBadRequest, CodeNotSet, "Error retrieving file", err)
		return models.FileAttachment{}, "", err
	}
	defer file.Close()

	attachment, tempFilePath, err := ProcessUpload(r.Context(), a, userID, attrs, file, header.Filename)
	if err != nil {
		RespondWithUploadError(w, logger, err)
		return models.FileAttachment{}, "", err
	}
	return attachment, tempFilePath, nil
}

// ProcessUpload is the part of an upload that does not depend on HTTP: it validates the file type,
// buffers src to a temp file (at most 30MB), normalizes image uploads, and uploads from disk to the
// provider. It returns the attachment model plus the temp file path for the caller to store and
// chunk, or an *UploadError (which has already been counted as a failed upload).
//
// A caller that gets a path back owns it: pass it to TriggerAsyncFileChunking (which removes it),
// or to AbandonFileAttachmentUpload if the attachment cannot be stored.
func ProcessUpload(ctx context.Context, a FileAttachmentUploader, userID uuid.UUID, attrs map[string]string, src io.Reader, fileName string) (_ models.FileAttachment, _ string, err error) {
	metrics := telemetry.Global()
	var uploadContentType string // set once the extension is recognised
	defer func() {
		if err != nil {
			metrics.RecordFileUpload(ctx, uploadContentType, telemetry.FileUploadFailure)
		}
	}()

	// Check file extension
	fileTypeInfo, err := utils.GetFileType(fileName)
	if err != nil {
		return models.FileAttachment{}, "", uploadErr(http.StatusBadRequest, "Unsupported file type", fmt.Errorf("%w: %v", ErrUnsupportedFileType, err))
	}
	uploadContentType = fileTypeInfo.ContentType

	tempFile, err := os.CreateTemp("", "chat-app-upload-*")
	if err != nil {
		return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error creating temp file", err)
	}
	tempFilePath := tempFile.Name()

	written, err := io.Copy(tempFile, io.LimitReader(src, MaxUploadBytes+1))
	if err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFilePath)
		return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error buffering file", err)
	}
	if written > MaxUploadBytes {
		_ = tempFile.Close()
		_ = os.Remove(tempFilePath)
		return models.FileAttachment{}, "", uploadErr(http.StatusBadRequest, fmt.Sprintf("File too large (max %dMB)", maxUploadMb), ErrFileTooLarge)
	}
	metrics.RecordFileSize(ctx, telemetry.FileOpUpload, telemetry.FileKind(fileTypeInfo.ContentType), written)
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempFilePath)
		return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error closing temp file", err)
	}

	if strings.HasPrefix(fileTypeInfo.ContentType, models.ImageMIMEPrefix) {
		normalizedTempFile, err := os.CreateTemp(filepath.Dir(tempFilePath), "chat-app-upload-normalized-*")
		if err != nil {
			_ = os.Remove(tempFilePath)
			return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error creating normalized image buffer", err)
		}
		normalizedTempFilePath := normalizedTempFile.Name()

		imageFile, err := os.Open(tempFilePath)
		if err != nil {
			_ = normalizedTempFile.Close()
			_ = os.Remove(normalizedTempFilePath)
			_ = os.Remove(tempFilePath)
			return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error opening image upload", err)
		}

		// The upload cap bounds disk usage; stream image data between temp files
		// so the raw upload is not also buffered in application memory.
		doneNormalize := metrics.TimeFileStage(ctx, telemetry.FileOpUpload, telemetry.FileStageNormalize)
		fileName, err = imageutil.NormalizeForUpload(imageFile, normalizedTempFile, fileName, imageutil.DefaultUploadImageMaxPx)
		doneNormalize(err)
		if closeErr := imageFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if closeErr := normalizedTempFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(normalizedTempFilePath)
			_ = os.Remove(tempFilePath)
			return models.FileAttachment{}, "", uploadErr(http.StatusBadRequest, "Invalid image", err)
		}
		if err := os.Rename(normalizedTempFilePath, tempFilePath); err != nil {
			_ = os.Remove(normalizedTempFilePath)
			_ = os.Remove(tempFilePath)
			return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error replacing normalized image", err)
		}

		fileTypeInfo = utils.FileTypeInfo{
			ContentType: imageutil.NormalizedUploadContentType,
			Extension:   ".png",
		}
	}

	uploadReader, err := os.Open(tempFilePath)
	if err != nil {
		_ = os.Remove(tempFilePath)
		return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error opening temp file", err)
	}
	defer uploadReader.Close()

	fileId, err := a.UploadFileAttachment(ctx, userID, attrs, uploadReader, fileName, fileTypeInfo)
	if err != nil {
		_ = os.Remove(tempFilePath)
		return models.FileAttachment{}, "", uploadErr(http.StatusInternalServerError, "Error uploading file", err)
	}

	return models.FileAttachment{
		UserID:   userID,
		FileID:   &fileId,
		Name:     fileName,
		FileType: fileTypeInfo.ContentType,
	}, tempFilePath, nil
}

// AttachmentRecords is the slice of the datastore that storing an uploaded attachment needs.
type AttachmentRecords interface {
	CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error)
	DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error
	SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error
}

// AttachmentStorage is everything StoreChatAttachment writes to. FileStore and Pipeline may be nil
// (no object store, no chunking), as UploadToS3 and TriggerAsyncFileChunking already allow.
type AttachmentStorage struct {
	Records  AttachmentRecords
	Files    storage.FileStore
	Provider FileAttachmentDeleter
	Pipeline *filechunker.FileChunkPipeline
}

// StoreChatAttachment saves an upload that ProcessUpload prepared to a chat: it creates the record,
// archives the file to the object store, and starts chunking. It takes ownership of tempFilePath.
//
// Storage routing: OpenAI vision does NOT read from our S3 bucket. The FileID that ProcessUpload
// got from the provider is an OpenAI Files API identifier; OpenAI resolves it internally from its
// own storage when the model runs. Our S3 paths are only used by Claude (raw bytes download) and
// the image gallery. Given that, images and non-images are routed differently:
//   - Images go to the canonical images/ path (FileKeyForImage + thumbnail). Claude downloads from
//     here; the gallery serves from here.
//   - Others go to the chat-scoped path (FileKeyForChat), used by the pgvector file-chunking
//     pipeline.
//
// Both paths roll back the record on a primary upload failure. The error is an *UploadError.
func StoreChatAttachment(ctx context.Context, logger *zap.Logger, st AttachmentStorage, userID, chatID uuid.UUID, fileAttachment models.FileAttachment, tempFilePath string) (*models.FileAttachment, error) {
	createdAttachment, err := st.Records.CreateFileAttachment(ctx, userID, fileAttachment)
	if err != nil {
		AbandonFileAttachmentUpload(ctx, logger, st.Provider, fileAttachment, tempFilePath)
		return nil, uploadErr(http.StatusInternalServerError, "Error creating file attachment", err)
	}

	isImage := strings.HasPrefix(fileAttachment.FileType, models.ImageMIMEPrefix)
	var s3Key string
	if isImage {
		s3Key = storage.FileKeyForImage(userID, createdAttachment.ID, fileAttachment.Name)
	} else {
		s3Key = storage.FileKeyForChat(userID, chatID, createdAttachment.ID, fileAttachment.Name)
	}
	if err := UploadToS3(ctx, st.Files, s3Key, tempFilePath, fileAttachment.FileType); err != nil {
		kind := "chat"
		if isImage {
			kind = "image"
		}
		logger.Error("S3 "+kind+" upload failed, rolling back file attachment record",
			zap.Error(err),
			zap.String("file_attachment_id", createdAttachment.ID.String()))
		_ = st.Records.DeleteFileAttachment(ctx, userID, createdAttachment.ID)
		DeleteProviderFile(ctx, logger, st.Provider, fileAttachment.FileID)
		_ = os.Remove(tempFilePath)
		return nil, uploadErr(http.StatusInternalServerError, "Error saving file — please retry", err)
	}
	// Persist the S3 key so rename/delete can use it without re-deriving it.
	if err := st.Records.SetFileAttachmentS3Key(ctx, userID, createdAttachment.ID, s3Key); err != nil {
		logger.Warn("failed to persist attachment s3_key",
			zap.String("file_attachment_id", createdAttachment.ID.String()),
			zap.String("s3_key", s3Key),
			zap.Error(err))
	}
	if isImage {
		// Thumbnail is best-effort — failure never blocks the upload response.
		UploadThumbnailFromPath(ctx, st.Files, logger, userID, createdAttachment.ID, tempFilePath)
	}
	// Upload success/failure is counted in this package (ProcessUpload, UploadToS3 and
	// TriggerAsyncFileChunking), shared by every upload path, so it isn't recorded here.
	TriggerAsyncFileChunking(logger, st.Pipeline, createdAttachment.ID, tempFilePath, fileAttachment.Name)
	return createdAttachment, nil
}

// TriggerAsyncFileChunking spawns a goroutine that chunks and embeds eligible text files.
// Skips silently for non-text / non-vector-support file types. Caller owns tempFilePath cleanup
// on early return; the goroutine removes it on completion or error.
// S3 archival must be completed synchronously by the caller before invoking this.
//
// Every upload path calls this exactly once, after the attachment is stored, so it is also where
// a successful upload is counted on FileUploads (failures are counted by UploadFileAttachment and
// UploadToS3).
func TriggerAsyncFileChunking(
	logger *zap.Logger,
	pipeline *filechunker.FileChunkPipeline,
	attachmentID uuid.UUID,
	tempFilePath string,
	fileName string,
) {
	recordUploadSuccess(fileName)
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
		if info.Size() > MaxUploadBytes {
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

// recordUploadSuccess counts a completed upload on FileUploads, deriving its kind from the stored
// file name (images are renamed to .png by normalization, which still maps to image).
func recordUploadSuccess(fileName string) {
	contentType := ""
	if info, err := utils.GetFileType(fileName); err == nil {
		contentType = info.ContentType
	}
	telemetry.Global().RecordFileUpload(context.Background(), contentType, telemetry.FileUploadSuccess)
}
