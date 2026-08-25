package mood

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Store defines the datastore operations required by the mood handlers.
type Store interface {
	CreateMood(ctx context.Context, userID uuid.UUID, req models.CreateMoodRequest) (*models.Mood, error)
	GetMood(ctx context.Context, userID, id uuid.UUID) (*models.Mood, error)
	ListMoods(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MoodFilters) (*models.PaginatedResponse, error)
	UpdateMood(ctx context.Context, userID, id uuid.UUID, req models.UpdateMoodRequest) (*models.Mood, error)
	SetMoodThumbnail(ctx context.Context, id uuid.UUID, jpegData []byte) error
	DeleteMood(ctx context.Context, userID, id uuid.UUID) error
	// SetMoodPersonalities replaces all personality associations for a mood.
	SetMoodPersonalities(ctx context.Context, userID, moodID uuid.UUID, personalityIDs []uuid.UUID) error

	// Needed for thumbnail generation: fetch a file attachment to get its S3 key.
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
}
