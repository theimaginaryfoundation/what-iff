package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// activeMemoryLookup reports which of ids are the user's memories and still active. It is the
// datastore's ActiveMemoryIDs; tests substitute a fake.
type activeMemoryLookup func(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error)

// persistedMemoryIDs collects the distinct memory ids on persisted additional-context items.
func persistedMemoryIDs(persisted ...[]models.AdditionalContextItem) []uuid.UUID {
	var ids []uuid.UUID
	seen := map[uuid.UUID]struct{}{}
	for _, items := range persisted {
		for _, it := range items {
			if it.Type != models.AdditionalContextTypeMemory || it.MemoryID == nil || *it.MemoryID == uuid.Nil {
				continue
			}
			if _, dup := seen[*it.MemoryID]; !dup {
				seen[*it.MemoryID] = struct{}{}
				ids = append(ids, *it.MemoryID)
			}
		}
	}
	return ids
}

// activeMemoryFilter is the replay re-check for a chat that is not sandboxed: a memory line
// persisted on an earlier turn is dropped once its memory is no longer active (archived by an
// agent with archive_memory, folded by a merge, or archived by the user), so an archived memory is
// not shown to the model again through the carry-over (ADR 0x036). Everything else is kept as
// before: items that are not memories, memory items without an id, and every item when the lookup
// fails. Returns nil (keep everything) when there is nothing to check.
func (b *messageContextBuilder) activeMemoryFilter(ctx context.Context, userID uuid.UUID, chat *models.Chat, ids []uuid.UUID) func(models.AdditionalContextItem) bool {
	if len(ids) == 0 {
		return nil
	}
	lookup := b.activeMemoryLookup
	if lookup == nil && b.ds != nil {
		lookup = b.ds.ActiveMemoryIDs
	}
	if lookup == nil {
		return nil
	}
	active, err := lookup(ctx, userID, ids)
	if err != nil {
		b.telemetry.Logger.Warn("failed to re-check replayed memory context; keeping it",
			zap.String("chat_id", chat.ID.String()), zap.Error(err))
		return nil
	}
	return func(it models.AdditionalContextItem) bool {
		if it.Type != models.AdditionalContextTypeMemory || it.MemoryID == nil {
			return true
		}
		_, ok := active[*it.MemoryID]
		return ok
	}
}
