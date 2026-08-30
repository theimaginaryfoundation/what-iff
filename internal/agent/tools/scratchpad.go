package tools

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type scratchpadDatastore interface {
	GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error)
	UpdatePersonalityScratchpad(ctx context.Context, userID uuid.UUID, personalityModel models.Personality) (*models.Personality, error)
}

type ScratchpadTool struct {
	datastore scratchpadDatastore
	logger    *zap.Logger
}

func NewScratchpadTool(ds *datastore.Datastore, logger *zap.Logger) *ScratchpadTool {
	return &ScratchpadTool{
		datastore: ds,
		logger:    logger,
	}
}
