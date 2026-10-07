package imagegallery

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Store defines the datastore operations required by the image gallery handlers.
type Store interface {
	ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error)
	DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error
	UpdateFileAttachmentName(ctx context.Context, userID, id uuid.UUID, name string) (*models.FileAttachment, error)
	CreateFileAttachmentReference(ctx context.Context, userID, srcID uuid.UUID) (*models.FileAttachment, error)
	SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error
	ListGalleryFolders(ctx context.Context, userID uuid.UUID, kind models.GalleryKind) ([]models.FolderCount, error)
	MoveFileAttachmentsToFolder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error)
	MoveGalleryFolder(ctx context.Context, userID uuid.UUID, from, to string) (int, error)
}
