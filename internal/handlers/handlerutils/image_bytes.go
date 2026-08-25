package handlerutils

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// ResolveImageBytes fetches image bytes for an attachment using a shared fallback
// chain so gallery reads, thumbnails, and agent image flows stay aligned.
func ResolveImageBytes(ctx context.Context, logger *zap.Logger, fileStore storage.FileStore, userID uuid.UUID, att *models.FileAttachment, wantThumb bool) ([]byte, string) {
	return storage.ResolveAttachmentImageBytes(ctx, logger, fileStore, userID, att, wantThumb)
}
