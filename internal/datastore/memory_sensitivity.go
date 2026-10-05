package datastore

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// This file is the SQL side of memory sensitivity gating. Every datastore path that hands
// memories to a conversation takes a models.MemorySensitivity limit and applies
// memorySensitivityAtMost in the WHERE clause, so a restricted chat's
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

// chatLimitAtMost matches chats whose own memory_sensitivity_limit is at or below limit: the
// conversations a chat with that limit may read (models.ConversationReadableUnder). It returns nil
// for an unrestricted limit, which may read every conversation.
func chatLimitAtMost(limit models.MemorySensitivity) predicate.Chat {
	if !limit.Restricted() {
		return nil
	}
	levels := models.SensitivitiesUpTo(limit)
	vals := make([]entchat.MemorySensitivityLimit, 0, len(levels))
	for _, l := range levels {
		vals = append(vals, entchat.MemorySensitivityLimit(l))
	}
	return entchat.MemorySensitivityLimitIn(vals...)
}

// summarySensitivity is the level of a chat's checkpoint summary written under the chat's limit:
// the default level capped by the limit (models.CapToLimit), so a public thread's summary is
// public and an ordinary thread's is personal.
func summarySensitivity(chatLimit models.MemorySensitivity) models.MemorySensitivity {
	return models.CapToLimit(models.DefaultMemorySensitivity, chatLimit)
}

// GetChatMemorySensitivityLimit returns the stored memory_sensitivity_limit of one of the user's
// chats. It returns ErrChatNotFound when the chat does not exist or belongs to someone else, so a
// caller deciding whether another conversation is readable can fail closed on any error.
func (d *Datastore) GetChatMemorySensitivityLimit(ctx context.Context, userID, chatID uuid.UUID) (models.MemorySensitivity, error) {
	row, err := d.dbClient.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Select(entchat.FieldMemorySensitivityLimit).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", ErrChatNotFound
		}
		return "", err
	}
	return models.MemorySensitivity(row.MemorySensitivityLimit), nil
}

// mergeSurvivorSensitivityAtMost matches merge events whose survivor memory is at or below the
// limit. The survivor id is a plain column (no edge), so this is a subquery on the memories table.
func mergeSurvivorSensitivityAtMost(limit models.MemorySensitivity) predicate.MemoryMergeEvent {
	levels := models.SensitivitiesUpTo(limit)
	vals := make([]any, 0, len(levels))
	for _, l := range levels {
		vals = append(vals, l)
	}
	return func(s *sql.Selector) {
		t := sql.Table(memory.Table)
		s.Where(sql.In(
			s.C(entmerge.FieldSurvivorMemoryID),
			sql.Select(t.C(memory.FieldID)).From(t).Where(sql.In(t.C(memory.FieldSensitivity), vals...)),
		))
	}
}

// MemoryIDsWithinSensitivity returns which of ids are owner-scoped, still exist and are at or
// below the limit. It backs re-filtering of memory context persisted on earlier turns of a chat
// whose limit may have been lowered since they were loaded.
func (d *Datastore) MemoryIDsWithinSensitivity(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, limit models.MemorySensitivity) (map[uuid.UUID]struct{}, error) {
	out := make(map[uuid.UUID]struct{}, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	preds := []predicate.Memory{memory.IDIn(ids...), memory.HasOwnerWith(user.ID(userID))}
	if p := memorySensitivityAtMost(limit); p != nil {
		preds = append(preds, p)
	}
	found, err := d.dbClient.Memory.Query().Where(preds...).IDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range found {
		out[id] = struct{}{}
	}
	return out, nil
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
