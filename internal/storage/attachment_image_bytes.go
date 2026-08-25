package storage

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ResolveAttachmentImageBytes loads image bytes for a file attachment using the same
// key fallbacks as the image gallery (thumbnail vs full). Safe for agent code paths
// that cannot import handlerutils (package cycle).
//
// store and logger may be nil: a nil store skips all object-storage lookups and a nil
// logger suppresses debug logging.
func ResolveAttachmentImageBytes(ctx context.Context, logger *zap.Logger, store FileStore, userID uuid.UUID, att *models.FileAttachment, wantThumb bool) ([]byte, string) {
	if att == nil {
		return nil, ""
	}

	if store != nil {
		tryDownload := func(key string) []byte {
			if key == "" {
				return nil
			}
			data, err := store.DownloadFile(ctx, key)
			if err != nil && logger != nil {
				logger.Debug("image bytes: key miss", zap.String("key", key), zap.Error(err))
			}
			return data
		}

		primaryKey := FileKeyForImage(userID, att.ID, att.Name)
		if wantThumb {
			primaryKey = FileKeyForImageThumbnail(userID, att.ID)
		}
		if data := tryDownload(primaryKey); len(data) > 0 {
			contentType := att.FileType
			if wantThumb {
				contentType = "image/jpeg"
			}
			return data, contentType
		}

		if data := tryDownload(att.S3Key); len(data) > 0 {
			return data, att.FileType
		}

		if wantThumb {
			if data := tryDownload(FileKeyForImage(userID, att.ID, att.Name)); len(data) > 0 {
				return data, att.FileType
			}
		}

		if att.ChatID != nil {
			if data := tryDownload(FileKeyForChat(userID, *att.ChatID, att.ID, att.Name)); len(data) > 0 {
				return data, att.FileType
			}
		}
	}

	return nil, ""
}
