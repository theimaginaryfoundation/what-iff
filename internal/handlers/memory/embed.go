package memory

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// memoryEmbedTimeout bounds the post-save embedding call so a slow provider
	// cannot hold the request open indefinitely. The save has already committed
	// by then; a timeout only leaves the memory for the startup backfill.
	memoryEmbedTimeout = 20 * time.Second
	// memoryEmbedChunkSize caps inputs per embeddings request (the provider
	// limit is far higher; this keeps a single request small).
	memoryEmbedChunkSize = 100
)

// memoryWriteStore is the part of the datastore the create and patch handlers
// use. It is an interface so the write-then-embed flow can be tested without a
// database; NewHandler wires the real *datastore.Datastore.
type memoryWriteStore interface {
	CreateMemoryFromInput(ctx context.Context, userID uuid.UUID, input models.CreateMemoryInput) (*models.Memory, error)
	CreateMemoriesBatch(ctx context.Context, userID uuid.UUID, input models.BatchCreateMemoryInput) ([]*models.Memory, error)
	UpdateMemory(ctx context.Context, userID, memoryID uuid.UUID, patch models.MemoryPatch) (*models.Memory, error)
	PatchMemoriesBatch(ctx context.Context, userID uuid.UUID, input models.BatchPatchMemoryInput) (*models.BatchPatchMemoryResult, error)
	OwnedMemoriesMissingEmbedding(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]models.MemoryEmbeddingCandidate, error)
	SetMemoryEmbedding(ctx context.Context, memoryID uuid.UUID, content string, vec []float32) (bool, error)
}

func (h *Handler) embeddingAvailable() bool {
	return h.embedTexts != nil || h.oaiClient != nil
}

// embedMemories gives each of ids that lacks an embedding one, after the
// memory itself has been saved. It is best-effort by design: a failure is
// logged and never surfaces to the caller, because the user's write already
// succeeded and the startup BackfillMemoryEmbeddings pass picks up whatever is still missing.
//
// "Lacks an embedding" is what makes this the right hook for both creates and
// edits: a new memory has none, and UpdateMemory drops the embedding in the
// same transaction when (and only when) content changes. A patch that leaves
// content unchanged keeps its embedding, so nothing is re-embedded.
func (h *Handler) embedMemories(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	if !h.embeddingAvailable() {
		h.logger.Debug("skipping memory embedding: OpenAI API key is not configured",
			zap.String("user_id", userID.String()),
			zap.Int("memory_count", len(ids)))
		return
	}

	// The save has committed; finish the embedding even if the client
	// disconnects, but don't let it run unbounded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), memoryEmbedTimeout)
	defer cancel()

	missing, err := h.store.OwnedMemoriesMissingEmbedding(ctx, userID, ids)
	if err != nil {
		h.logger.Warn("failed to look up memories missing an embedding; leaving them for backfill",
			zap.String("user_id", userID.String()),
			zap.Int("memory_count", len(ids)),
			zap.Error(err))
		return
	}

	for start := 0; start < len(missing); start += memoryEmbedChunkSize {
		end := min(start+memoryEmbedChunkSize, len(missing))
		chunk := missing[start:end]

		inputs := make([]string, len(chunk))
		for i, c := range chunk {
			inputs[i] = c.Content
		}
		vectors, err := h.createEmbeddings(ctx, inputs)
		if err == nil && len(vectors) != len(inputs) {
			err = errEmbeddingCountMismatch
		}
		if err != nil {
			h.logger.Warn("failed to embed memories; leaving them for backfill",
				zap.String("user_id", userID.String()),
				zap.Int("memory_count", len(chunk)),
				zap.Error(err))
			continue
		}

		for i, c := range chunk {
			if _, err := h.store.SetMemoryEmbedding(ctx, c.MemoryID, c.Content, vectors[i]); err != nil {
				h.logger.Warn("failed to store memory embedding; leaving it for backfill",
					zap.String("user_id", userID.String()),
					zap.String("memory_id", c.MemoryID.String()),
					zap.Error(err))
			}
		}
	}
}

func memoryIDs(memories []*models.Memory) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(memories))
	for _, m := range memories {
		if m != nil {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
