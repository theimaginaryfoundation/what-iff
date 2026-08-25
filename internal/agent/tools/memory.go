package tools

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

const (
	// Memory scope constants
	MemoryScopeUser = "User"
	MemoryScopeChat = "Chat"
)

type VectorStoreMemoryTool struct {
	ds        *datastore.Datastore
	oaiClient *openai.Client
	logger    *zap.Logger
}

func NewVectorStoreMemoryTool(ds *datastore.Datastore, oaiClient *openai.Client, logger *zap.Logger) *VectorStoreMemoryTool {
	return &VectorStoreMemoryTool{
		ds:        ds,
		oaiClient: oaiClient,
		logger:    logger,
	}
}

// CreateEmbedding creates an embedding for the given input using the shared embedding package.
func (t *VectorStoreMemoryTool) CreateEmbedding(ctx context.Context, input string) ([]float32, error) {
	return embedding.CreateEmbedding(ctx, t.oaiClient, input)
}
