package storage

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// ResolveAttachmentImageBytes loads image bytes for a file attachment using the same
// key fallbacks as the image gallery (thumbnail vs full). Safe for agent code paths
// that cannot import handlerutils (package cycle).
//
// store and logger may be nil: a nil store skips all object-storage lookups and a nil
// logger suppresses debug logging.
func ResolveAttachmentImageBytes(ctx context.Context, logger *zap.Logger, store FileStore, userID uuid.UUID, att *models.FileAttachment, wantThumb bool) ([]byte, string) {
	data, contentType, _ := ResolveAttachmentImage(ctx, logger, store, userID, att, wantThumb)
	return data, contentType
}

// ResolveAttachmentImage is ResolveAttachmentImageBytes that also reports which key answered, as
// one of the telemetry.ImageSource* values. A thumbnail request answered by anything but
// telemetry.ImageSourceThumbnail got the full image instead; callers can hand those bytes to
// imageutil.BackfillThumbnail so the next read is a single small fetch. Every read is counted on
// telemetry.ImageResolves.
func ResolveAttachmentImage(ctx context.Context, logger *zap.Logger, store FileStore, userID uuid.UUID, att *models.FileAttachment, wantThumb bool) ([]byte, string, string) {
	if att == nil {
		return nil, "", telemetry.ImageSourceNone
	}
	variant := telemetry.ImageVariantFull
	if wantThumb {
		variant = telemetry.ImageVariantThumbnail
	}
	data, contentType, source := resolveAttachmentImage(ctx, logger, store, userID, att, wantThumb)
	telemetry.Global().RecordImageResolve(ctx, variant, source)
	return data, contentType, source
}

func resolveAttachmentImage(ctx context.Context, logger *zap.Logger, store FileStore, userID uuid.UUID, att *models.FileAttachment, wantThumb bool) ([]byte, string, string) {
	if store == nil {
		return nil, "", telemetry.ImageSourceNone
	}
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

	canonicalKey := FileKeyForImage(userID, att.ID, att.Name)
	if wantThumb {
		if data := tryDownload(FileKeyForImageThumbnail(userID, att.ID)); len(data) > 0 {
			return data, "image/jpeg", telemetry.ImageSourceThumbnail
		}
	} else if data := tryDownload(canonicalKey); len(data) > 0 {
		return data, att.FileType, telemetry.ImageSourceCanonical
	}

	// The row's key is usually the canonical key itself; a key that already missed isn't refetched.
	if wantThumb || att.S3Key != canonicalKey {
		if data := tryDownload(att.S3Key); len(data) > 0 {
			return data, att.FileType, telemetry.ImageSourceS3Key
		}
	}

	if wantThumb && canonicalKey != att.S3Key {
		if data := tryDownload(canonicalKey); len(data) > 0 {
			return data, att.FileType, telemetry.ImageSourceCanonical
		}
	}

	if att.ChatID != nil {
		if data := tryDownload(FileKeyForChat(userID, *att.ChatID, att.ID, att.Name)); len(data) > 0 {
			return data, att.FileType, telemetry.ImageSourceChat
		}
	}

	return nil, "", telemetry.ImageSourceNone
}
