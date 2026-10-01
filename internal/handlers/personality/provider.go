package personality

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Store defines the datastore operations required by the personality handlers.
// Keeping this as an interface makes the handler unit-testable without a DB.
type Store interface {
	CreatePersonality(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	ListPersonalities(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error)
	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
	// GetPersonalityCard returns the imported character-card passthrough blob (nil when none).
	GetPersonalityCard(ctx context.Context, userID, personalityID uuid.UUID) (*models.PersonalityCard, error)
	// GetFileAttachment loads the cover image's record for PNG card export.
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	// GetUserByID supplies the owner's username for the exported card's creator field.
	GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error)
	// GetPersonalityUsageStats fills Personality.Stats for single-personality
	// responses; GetPersonality itself leaves Stats zeroed.
	GetPersonalityUsageStats(ctx context.Context, userID, personalityID uuid.UUID) (models.PersonalityUsageStats, error)
	UpdatePersonality(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	DeletePersonality(ctx context.Context, userID, id uuid.UUID) error
	ListPersonalityExpressions(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityExpression, error)
	UpsertPersonalityExpression(ctx context.Context, userID, personalityID uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error)
	DeletePersonalityExpression(ctx context.Context, userID, personalityID uuid.UUID, key string) error

	CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error)
	DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error
	SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error

	GetUserPreferences(ctx context.Context, userID uuid.UUID) (*models.UserPreferences, error)
	UpdateUserPreferences(ctx context.Context, userID uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error)

	// Personality generation flow
	GetOrCreateActiveFlow(ctx context.Context, userID uuid.UUID) (*models.PersonalityGenFlow, error)
	GetFlow(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error)
	UpdateFlow(ctx context.Context, userID uuid.UUID, flowID uuid.UUID, req models.UpdateFlowRequest) (*models.PersonalityGenFlow, error)
	ResetFlow(ctx context.Context, userID, flowID uuid.UUID) (*models.PersonalityGenFlow, error)
	SetFlowGenerated(ctx context.Context, userID, flowID uuid.UUID, prompt, aboutMe string, names []string) (*models.PersonalityGenFlow, error)
	AcceptFlow(ctx context.Context, userID, flowID, personalityID uuid.UUID) (*models.PersonalityGenFlow, error)

	FindActivePersonalityMediaJob(ctx context.Context, userID uuid.UUID) (*models.Job, error)
	FindActivePersonalityGenerationJob(ctx context.Context, userID, flowID uuid.UUID) (*models.Job, error)
}
