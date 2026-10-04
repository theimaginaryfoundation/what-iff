package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/entity"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// This file is the SQL side of memory sensitivity gating. Every datastore path that hands
// memories or entities to a conversation takes a models.MemorySensitivity limit and applies
// memorySensitivityAtMost / entitySensitivityAtMost in the WHERE clause, so a restricted chat's
// result set is filled from permitted rows only (never post-filtered, which would return fewer
// rows than asked for). An unrestricted limit (sensitive, or empty) adds no predicate.

// memorySensitivityAtMost matches memories a chat with this limit may read. It returns nil for
// an unrestricted limit so callers can skip the predicate.
func memorySensitivityAtMost(limit models.MemorySensitivity) predicate.Memory {
	if !limit.Restricted() {
		return nil
	}
	levels := models.SensitivitiesUpTo(limit)
	vals := make([]memory.Sensitivity, 0, len(levels))
	for _, l := range levels {
		vals = append(vals, memory.Sensitivity(l))
	}
	return memory.SensitivityIn(vals...)
}

// entitySensitivityAtMost is memorySensitivityAtMost for entities.
func entitySensitivityAtMost(limit models.MemorySensitivity) predicate.Entity {
	if !limit.Restricted() {
		return nil
	}
	levels := models.SensitivitiesUpTo(limit)
	vals := make([]entity.Sensitivity, 0, len(levels))
	for _, l := range levels {
		vals = append(vals, entity.Sensitivity(l))
	}
	return entity.SensitivityIn(vals...)
}

// applyMemorySensitivityFilters applies the list filters: an exact Sensitivity match (the memory
// manager filter) and a MaxSensitivity ceiling (the chat sandbox gate).
func applyMemorySensitivityFilters(query *ent.MemoryQuery, filters models.MemoryFilters) *ent.MemoryQuery {
	if filters.Sensitivity != nil && *filters.Sensitivity != "" {
		query = query.Where(memory.SensitivityEQ(memory.Sensitivity(*filters.Sensitivity)))
	}
	if filters.MaxSensitivity != nil {
		if p := memorySensitivityAtMost(*filters.MaxSensitivity); p != nil {
			query = query.Where(p)
		}
	}
	return query
}

// patchMemoriesSensitivityBatch sets sensitivity on many memories in one UPDATE inside one
// transaction. It is the bulk path for the memory manager: owner-scoped, Summary memories are
// never touched (they are thread-management state and carry no sensitivity). Ids that are
// missing, foreign or Summary count as not found: with AllOrNone the whole batch is rejected
// with ErrMemoryNotFound and nothing changes; otherwise they are skipped.
func (d *Datastore) patchMemoriesSensitivityBatch(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, level models.MemorySensitivity, allOrNone bool) (*models.BatchPatchMemoryResult, error) {
	if !level.Valid() {
		return nil, fmt.Errorf("%w: invalid sensitivity: %s", ErrInvalidRequestBody, level)
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	eligible := []predicate.Memory{
		memory.IDIn(ids...),
		memory.HasOwnerWith(user.ID(userID)),
		memory.ScopeNEQ(memory.ScopeSummary),
	}
	found, err := tx.Memory.Query().Where(eligible...).IDs(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if allOrNone && len(found) != len(uniqueUUIDs(ids)) {
		_ = tx.Rollback()
		return nil, ErrMemoryNotFound
	}
	if len(found) == 0 {
		_ = tx.Rollback()
		return &models.BatchPatchMemoryResult{Results: []*models.Memory{}}, nil
	}
	if _, err := tx.Memory.Update().
		Where(memory.IDIn(found...)).
		SetSensitivity(memory.Sensitivity(level)).
		SetUpdatedAt(time.Now()).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	reloaded, err := tx.Memory.Query().Where(memory.IDIn(found...)).WithChat().All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*ent.Memory, len(reloaded))
	for _, m := range reloaded {
		byID[m.ID] = m
	}
	out := make([]*models.Memory, 0, len(found))
	seen := make(map[uuid.UUID]struct{}, len(found))
	for _, id := range ids { // preserve request order
		m, ok := byID[id]
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, toMemoryModel(m))
	}
	return &models.BatchPatchMemoryResult{Results: out, UpdatedCount: len(out)}, nil
}

func uniqueUUIDs(ids []uuid.UUID) map[uuid.UUID]struct{} {
	set := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// isSensitivityOnlyPatch reports whether a patch changes nothing but sensitivity, which the
// batch path can apply as one UPDATE.
func isSensitivityOnlyPatch(p models.MemoryPatch) bool {
	return p.Sensitivity != nil &&
		p.Content == nil && p.Level == nil && !p.SetChatID && !p.SetPinnedPersonalityID &&
		p.Type == nil && p.Starred == nil && p.Status == nil && p.Confidence == nil
}
