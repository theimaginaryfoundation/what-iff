package tools

import (
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

type ScratchpadTool struct {
	datastore *datastore.Datastore
	logger    *zap.Logger
}

func NewScratchpadTool(ds *datastore.Datastore, logger *zap.Logger) *ScratchpadTool {
	return &ScratchpadTool{
		datastore: ds,
		logger:    logger,
	}
}
