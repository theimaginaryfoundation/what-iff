package agent

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// historyImagesForMessage collects vision payloads for image attachments on a prior message.
// Raw bytes are loaded when loadRawBytes is true (Claude) or when an attachment lacks an
// OpenAI FileID and needs a data-URL fallback.
func historyImagesForMessage(
	ctx context.Context,
	logger *zap.Logger,
	fileStore storage.FileStore,
	userID uuid.UUID,
	chatID uuid.UUID,
	msg *models.ChatMessage,
	loadRawBytes bool,
) []provider.UserMessageImage {
	if msg == nil || len(msg.Attachments) == 0 {
		return nil
	}
	needBytes := loadRawBytes
	if !needBytes {
		for _, att := range msg.Attachments {
			if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
				continue
			}
			if att.FileID == nil || strings.TrimSpace(*att.FileID) == "" {
				needBytes = true
				break
			}
		}
	}
	imageBytes := map[uuid.UUID][]byte(nil)
	if needBytes {
		imageBytes = loadImageBytesForAttachments(ctx, logger, fileStore, userID, chatID, msg.Attachments)
	}
	return userMessageImagesFromAttachments(msg.Attachments, imageBytes)
}

// loadImageBytesForAttachments is the canonical path for resolving historical and
// tool-result image bytes for model context (Claude raw bytes, OpenAI data-URL fallback).
//
// fileStore and logger may both be nil. A nil store skips object-storage lookups and
// a nil logger skips debug logging.
func loadImageBytesForAttachments(
	ctx context.Context,
	logger *zap.Logger,
	fileStore storage.FileStore,
	userID, chatID uuid.UUID,
	attachments []*models.FileAttachment,
) map[uuid.UUID][]byte {
	result := make(map[uuid.UUID][]byte)
	for _, att := range attachments {
		if att == nil || !strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			continue
		}
		raw, _ := storage.ResolveAttachmentImageBytes(ctx, logger, fileStore, userID, att, false)
		if len(raw) == 0 && fileStore != nil && att.S3Key == "" {
			chatKey := storage.FileKeyForChat(userID, chatID, att.ID, att.Name)
			data, err := fileStore.DownloadFile(ctx, chatKey)
			if err == nil && len(data) > 0 {
				raw = data
			} else if err != nil && logger != nil {
				logger.Debug("history vision: chat key miss",
					zap.String("attachment_id", att.ID.String()),
					zap.String("key", chatKey),
					zap.Error(err))
			}
		}
		if len(raw) > 0 {
			result[att.ID] = raw
			continue
		}
		if logger != nil {
			logger.Warn("history vision: could not resolve image bytes",
				zap.String("attachment_id", att.ID.String()),
				zap.String("s3_key", att.S3Key))
		}
	}
	return result
}
