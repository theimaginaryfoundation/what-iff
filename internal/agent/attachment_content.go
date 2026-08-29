package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// ResolveAttachmentTextContent loads raw text for an attachment through the
// agent's configured file store. The returned content is untrusted user data
// and must be escaped by any UI that renders it.
func (a *Agent) ResolveAttachmentTextContent(ctx context.Context, userID uuid.UUID, attachment *models.FileAttachment) (string, bool) {
	if a == nil {
		return "", false
	}
	return storage.ResolveAttachmentTextContent(ctx, a.logger, a.fileStore, userID, attachment)
}
