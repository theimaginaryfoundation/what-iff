package handlerutils

import (
	"context"
	"os"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// Re-export for convenience so callers within the handlers layer don't need a
// second import just for the constant.
const DefaultThumbnailMaxPx = imageutil.DefaultThumbnailMaxPx

// GenerateImageThumbnail is a thin wrapper around imageutil.GenerateThumbnail.
func GenerateImageThumbnail(data []byte, maxPx int) ([]byte, error) {
	return imageutil.GenerateThumbnail(data, maxPx)
}

// UploadImageToGalleryPath reads the temp file at tempFilePath and delegates to
// imageutil.UploadBytesToGalleryPath. Best-effort; failures are logged.
func UploadImageToGalleryPath(
	ctx context.Context,
	fileStore storage.FileStore,
	logger *zap.Logger,
	userID, attachmentID uuid.UUID,
	filename, contentType, tempFilePath string,
) {
	if fileStore == nil || tempFilePath == "" {
		return
	}

	data, err := os.ReadFile(tempFilePath)
	if err != nil {
		logger.Warn("image gallery upload: failed to read temp file",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
		return
	}

	imageutil.UploadBytesToGalleryPath(ctx, fileStore, logger, userID, attachmentID, filename, contentType, data)
}

// UploadThumbnailFromPath reads the file at tempFilePath, generates a JPEG thumbnail,
// and uploads it to the FileKeyForImageThumbnail path. Best-effort: failures are logged.
func UploadThumbnailFromPath(
	ctx context.Context,
	fileStore storage.FileStore,
	logger *zap.Logger,
	userID, attachmentID uuid.UUID,
	tempFilePath string,
) {
	if fileStore == nil || tempFilePath == "" {
		return
	}
	data, err := os.ReadFile(tempFilePath)
	if err != nil {
		logger.Warn("thumbnail upload: failed to read temp file",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
		return
	}
	thumb, err := imageutil.GenerateThumbnail(data, imageutil.DefaultThumbnailMaxPx)
	if err != nil {
		logger.Warn("thumbnail upload: failed to generate thumbnail",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
		return
	}
	key := storage.FileKeyForImageThumbnail(userID, attachmentID)
	if err := fileStore.UploadFile(ctx, key, thumb, "image/jpeg"); err != nil {
		logger.Warn("thumbnail upload: failed to upload",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
	}
}

// UploadImageBytesToGalleryPath is an alias kept for callers that already have
// bytes in memory (e.g. generated images from the agent).
//
// Deprecated: prefer imageutil.UploadBytesToGalleryPath directly from packages
// that already import imageutil (e.g. the agent layer).
func UploadImageBytesToGalleryPath(
	ctx context.Context,
	fileStore storage.FileStore,
	logger *zap.Logger,
	userID, attachmentID uuid.UUID,
	filename, contentType string,
	data []byte,
) {
	imageutil.UploadBytesToGalleryPath(ctx, fileStore, logger, userID, attachmentID, filename, contentType, data)
}
