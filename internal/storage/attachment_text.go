package storage

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ResolveAttachmentTextContent loads UTF-8 text for a document attachment from object
// storage. Returns ("", false) when the attachment is not text,
// bytes cannot be resolved, or content is empty after trim.
//
// Security: the returned string is raw, untrusted user content (may contain HTML, script,
// or other markup). It is intended for LLM context and logging — not HTML rendering.
// Downstream callers that embed this value in HTML (web UI, emails, etc.) must escape or
// sanitize it at render time.
//
// Object keys: att.S3Key is written once at upload and is the stable reference.
// When it is empty (legacy rows), derived keys embed attachment ID; an empty filename
// resolves to an ID-only path via filenameWithFallback.
func ResolveAttachmentTextContent(ctx context.Context, logger *zap.Logger, store FileStore, userID uuid.UUID, att *models.FileAttachment) (string, bool) {
	if att == nil || strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
		return "", false
	}

	var raw []byte
	if store != nil {
		tryDownload := func(key string) bool {
			if key == "" {
				return false
			}
			data, err := store.DownloadFile(ctx, key)
			if err == nil && len(data) > 0 {
				raw = data
				return true
			}
			if err != nil && logger != nil {
				logger.Debug("attachment text: key miss",
					zap.String("attachment_id", att.ID.String()),
					zap.String("key", key),
					zap.Error(err))
			}
			return false
		}

		if !tryDownload(att.S3Key) {
			if att.ChatID != nil {
				tryDownload(FileKeyForChat(userID, *att.ChatID, att.ID, att.Name))
			}
		}
		if len(raw) == 0 && att.PersonalityID != nil {
			tryDownload(FileKeyForPersonality(userID, *att.PersonalityID, att.ID, att.Name))
		}
		if len(raw) == 0 {
			tryDownload(FileKeyFallback(userID, att.ID, att.Name))
		}
		if len(raw) == 0 {
			tryDownload(FileKeyForAttachment(userID, att.ID, att.Name, att.FileType, att.ChatID, att.PersonalityID))
		}
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", false
	}
	return text, true
}
