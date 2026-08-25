package datastore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/embedding"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	entschema "github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/memoryutil"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"go.uber.org/zap"
)

// PersistMemoryMergeGroup writes one merge grouping using only known memory IDs from
// thread context (no duplicate scans). When survivorMemoryID is set, that row is
// updated in place; absorbMemoryIDs are soft-retired when consolidating duplicates.
func (d *Datastore) PersistMemoryMergeGroup(
	ctx context.Context,
	userID uuid.UUID,
	chatID uuid.UUID,
	group models.MemoryMergeGroupProposal,
	duplicatesFolded int,
	survivorMemoryID *uuid.UUID,
	absorbMemoryIDs []uuid.UUID,
	embeddingVector []float32,
	activePersonalityID uuid.UUID,
	sourceMembers []models.MemoryMergeSourceMember,
	compactionEventID *uuid.UUID,
) (*models.Memory, error) {
	if strings.TrimSpace(group.CanonicalContent) == "" {
		return nil, nil
	}
	if duplicatesFolded < 1 {
		duplicatesFolded = 1
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

	now := time.Now().UTC()
	targetScope := memory.Scope(group.Scope)
	if group.Scope != string(memory.ScopeUser) && group.Scope != string(memory.ScopeChat) {
		targetScope = memory.ScopeChat
	}
	confidence := group.Confidence
	if confidence == "" {
		confidence = models.MemoryConfidenceMedium
	}

	if survivorMemoryID != nil && *survivorMemoryID != uuid.Nil {
		existing, err := tx.Memory.Query().
			Where(
				memory.ID(*survivorMemoryID),
				memory.HasOwnerWith(user.ID(userID)),
				memory.StatusEQ(memory.StatusActive),
			).
			Only(ctx)
		if err != nil {
			_ = tx.Rollback()
			if ent.IsNotFound(err) {
				return nil, ErrMemoryNotFound
			}
			return nil, err
		}
		// duplicatesFolded counts every occurrence in the group INCLUDING the survivor candidate.
		// foldIntoLiveMemory's contract is "additional observations, survivor excluded", so drop
		// one for the survivor whose occurrence is already carried by its prior count.
		foldedIn := duplicatesFolded - 1
		if foldedIn < 0 {
			foldedIn = 0
		}
		extract := memoryutil.CollapsedExtractedMemory{
			Content:             group.CanonicalContent,
			Scope:               group.Scope,
			Confidence:          confidence,
			BatchDuplicateCount: foldedIn,
		}
		mem, foldErr := d.foldIntoLiveMemory(ctx, tx, userID, existing, extract, now, sourceMembers, compactionEventID)
		if foldErr != nil {
			_ = tx.Rollback()
			return nil, foldErr
		}
		if len(absorbMemoryIDs) > 0 {
			if _, err := tx.Embedding.Delete().
				Where(embedding.HasMemoryWith(memory.IDIn(absorbMemoryIDs...))).
				Exec(ctx); err != nil {
				_ = tx.Rollback()
				return nil, err
			}
			if _, err := tx.Memory.Update().
				Where(memory.IDIn(absorbMemoryIDs...)).
				SetStatus(memory.StatusInactive).
				SetUpdatedAt(now).
				Save(ctx); err != nil {
				_ = tx.Rollback()
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return mem, nil
	}

	extract := memoryutil.CollapsedExtractedMemory{
		Content:             group.CanonicalContent,
		Scope:               group.Scope,
		Confidence:          confidence,
		BatchDuplicateCount: duplicatesFolded,
	}
	mem, createErr := d.createMergedMemory(ctx, tx, userID, chatID, extract, embeddingVector, activePersonalityID, targetScope, now, compactionEventID)
	if createErr != nil {
		_ = tx.Rollback()
		return nil, createErr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return mem, nil
}

// LinkGroupNewMember is a freshly-extracted memory that will be created and then cross-referenced
// into a link group. Embedding is created by the caller (agent) so the new row stays searchable.
type LinkGroupNewMember struct {
	Content    string
	Confidence models.MemoryConfidence
	Embedding  []float32
}

// PersistMemoryLinkGroup cross-references related-but-distinct memories — the same topic or event
// accessed through different functional registers (technical / emotional / narrative / question /
// metaphor) — under one shared link_group_id. Unlike a merge, NOTHING is folded or de-indexed:
// new members are created as normal active memories and every member keeps its own surface and
// embedding. Reverting a link only withdraws the cross-reference (see undoLinkGroup); the
// memories themselves persist.
//
// V1 simplifications (documented priors): a memory belongs to at most one link group, so linking
// overwrites any prior link_group_id on existing members, and revert clears to null rather than
// restoring a prior group. New members are not auto-pinned to a personality.
func (d *Datastore) PersistMemoryLinkGroup(
	ctx context.Context,
	userID uuid.UUID,
	chatID uuid.UUID,
	scope string,
	linkLabel string,
	existingMemberIDs []uuid.UUID,
	newMembers []LinkGroupNewMember,
	sourceMembers []models.MemoryMergeSourceMember,
	compactionEventID *uuid.UUID,
) (*models.MemoryMergeEvent, error) {
	if len(existingMemberIDs)+len(newMembers) < 2 {
		// A link needs at least two surfaces to relate.
		return nil, nil
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

	now := time.Now().UTC()
	linkGroupID := uuid.New()
	targetScope := memory.Scope(scope)
	if scope != string(memory.ScopeUser) && scope != string(memory.ScopeChat) {
		targetScope = memory.ScopeChat
	}

	allMemberIDs := make([]uuid.UUID, 0, len(existingMemberIDs)+len(newMembers))
	createdForAudit := make([]models.CompactionLoadedMemory, 0, len(newMembers))

	for _, nm := range newMembers {
		confidence := nm.Confidence
		if confidence == "" {
			confidence = models.MemoryConfidenceMedium
		}
		create := tx.Memory.Create().
			SetContent(nm.Content).
			SetScope(targetScope).
			SetType(memory.TypeContext).
			SetStatus(memory.StatusActive).
			SetConfidence(confidence.Float()).
			SetOwnerID(userID).
			SetLinkGroupID(linkGroupID).
			SetCreatedAt(now).
			SetUpdatedAt(now)
		if targetScope == memory.ScopeChat {
			create = create.SetChatID(chatID)
		}
		newMem, createErr := create.Save(ctx)
		if createErr != nil {
			_ = tx.Rollback()
			return nil, createErr
		}
		if len(nm.Embedding) > 0 {
			if _, err := tx.Embedding.Create().
				SetEmbedding(pgvector.NewVector(nm.Embedding)).
				SetMemoryID(newMem.ID).
				Save(ctx); err != nil {
				_ = tx.Rollback()
				return nil, err
			}
		}
		allMemberIDs = append(allMemberIDs, newMem.ID)
		id := newMem.ID
		createdForAudit = append(createdForAudit, models.CompactionLoadedMemory{
			MemoryID:   &id,
			Content:    newMem.Content,
			Scope:      string(newMem.Scope),
			Confidence: newMem.Confidence,
		})
	}

	if len(existingMemberIDs) > 0 {
		if _, err := tx.Memory.Update().
			Where(
				memory.IDIn(existingMemberIDs...),
				memory.HasOwnerWith(user.ID(userID)),
				memory.StatusEQ(memory.StatusActive),
			).
			SetLinkGroupID(linkGroupID).
			SetUpdatedAt(now).
			Save(ctx); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		allMemberIDs = append(allMemberIDs, existingMemberIDs...)
	}

	if len(allMemberIDs) < 2 {
		_ = tx.Rollback()
		return nil, nil
	}

	// Anchor the event on the first member; link events retire nothing, so this is only a handle.
	event, err := tx.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(allMemberIDs[0]).
		SetMergeType(entmerge.MergeTypeLink).
		SetLinkGroupID(linkGroupID).
		SetContent(linkLabel).
		SetDuplicatesFolded(0).
		SetSourceMembers(toEntSourceMembers(sourceMembers)).
		SetNillableCompactionEventID(compactionEventID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if compactionEventID != nil && *compactionEventID != uuid.Nil && len(createdForAudit) > 0 {
		if err := d.appendCompactionEventCreatedMemoriesTx(ctx, tx, userID, *compactionEventID, createdForAudit); err != nil {
			if !errors.Is(err, ErrCompactionEventNotFound) {
				_ = tx.Rollback()
				return nil, err
			}
			// Audit attach is best-effort: keep the link + new memories even if the compaction row is gone.
			d.logger.Warn("compaction event missing while attaching link-created memories",
				zap.String("compaction_event_id", compactionEventID.String()),
				zap.String("user_id", userID.String()),
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toMemoryMergeEventModel(event), nil
}

// MergeLiveExtractedMemory persists one collapsed extraction row, folding only into
// memories that were live in the current turn context (by ID). Batch duplicates are
// already collapsed upstream; duplicates_folded tracks how many rows were folded.
func (d *Datastore) MergeLiveExtractedMemory(
	ctx context.Context,
	userID uuid.UUID,
	chatID uuid.UUID,
	extract memoryutil.CollapsedExtractedMemory,
	embeddingVector []float32,
	activePersonalityID uuid.UUID,
	liveMemoryIDs []uuid.UUID,
) (*models.Memory, error) {
	normalized := memoryutil.NormalizeContentForDedupe(extract.Content)
	if normalized == "" {
		return nil, nil
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

	now := time.Now().UTC()
	targetScope := memory.Scope(extract.Scope)
	if extract.Scope != string(memory.ScopeUser) && extract.Scope != string(memory.ScopeChat) {
		targetScope = memory.ScopeChat
	}

	liveMatch, err := findLiveMemoryMatch(ctx, tx, userID, chatID, targetScope, normalized, liveMemoryIDs)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if liveMatch != nil {
		sourceMembers := sourceMembersForLiveFold(liveMatch, extract)
		mem, mergeErr := d.foldIntoLiveMemory(ctx, tx, userID, liveMatch, extract, now, sourceMembers, nil)
		if mergeErr != nil {
			_ = tx.Rollback()
			return nil, mergeErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return mem, nil
	}

	mem, mergeErr := d.createMergedMemory(ctx, tx, userID, chatID, extract, embeddingVector, activePersonalityID, targetScope, now, nil)
	if mergeErr != nil {
		_ = tx.Rollback()
		return nil, mergeErr
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return mem, nil
}

func findLiveMemoryMatch(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
	chatID uuid.UUID,
	targetScope memory.Scope,
	normalized string,
	liveMemoryIDs []uuid.UUID,
) (*ent.Memory, error) {
	if len(liveMemoryIDs) == 0 {
		return nil, nil
	}

	q := tx.Memory.Query().
		Where(
			memory.IDIn(liveMemoryIDs...),
			memory.HasOwnerWith(user.ID(userID)),
			memory.StatusEQ(memory.StatusActive),
			memory.ScopeEQ(targetScope),
		)
	if targetScope == memory.ScopeChat {
		q = q.Where(memory.HasChatWith(entchat.ID(chatID)))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if memoryutil.NormalizeContentForDedupe(row.Content) != normalized {
			continue
		}
		return row, nil
	}
	return nil, nil
}

func (d *Datastore) foldIntoLiveMemory(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
	existing *ent.Memory,
	extract memoryutil.CollapsedExtractedMemory,
	now time.Time,
	sourceMembers []models.MemoryMergeSourceMember,
	compactionEventID *uuid.UUID,
) (*models.Memory, error) {
	priorConfidence := models.ClampConfidence(existing.Confidence)
	var priorChain *entschema.MemoryChainMetadata
	priorChainWasNil := existing.ChainMetadata == nil
	if existing.ChainMetadata != nil {
		copyMeta := *existing.ChainMetadata
		priorChain = &copyMeta
	}

	// Reconcile the incoming (bucketed -> float) confidence with the survivor's stored value.
	// Behaviour-preserving: keep the lower of the two (conservative on a fold).
	mergedConfidence := extract.Confidence.Float()
	if priorConfidence < mergedConfidence {
		mergedConfidence = priorConfidence
	}

	// duplicate_count is the confidence signal (how many times this fact has been observed), so
	// keep it an honest tally. Contract: extract.BatchDuplicateCount is the number of ADDITIONAL
	// observations being folded into the survivor (callers exclude the survivor's own
	// occurrence). The survivor itself always stands for at least one observation — even a
	// legacy row with no chain metadata — so we floor its prior count at 1 before adding. This
	// fixes the old under-count (a nil-chain survivor contributed 0).
	// NOTE: absorbed members that themselves carried a prior count >1 are currently counted as
	// one observation each; summing per-member counts is a follow-up precision pass.
	priorCount := 0
	if existing.ChainMetadata != nil {
		priorCount = existing.ChainMetadata.DuplicateCount
	}
	if priorCount < 1 {
		priorCount = 1
	}
	duplicateCount := priorCount + extract.BatchDuplicateCount

	verifiedTimes := []time.Time{now}
	if existing.ChainMetadata != nil {
		verifiedTimes = append(verifiedTimes, existing.ChainMetadata.VerifiedTimestampsFirst...)
		verifiedTimes = append(verifiedTimes, existing.ChainMetadata.VerifiedTimestampsLast...)
	}
	sort.Slice(verifiedTimes, func(i, j int) bool { return verifiedTimes[i].Before(verifiedTimes[j]) })

	first := make([]time.Time, 0, 3)
	last := make([]time.Time, 0, 3)
	for i := 0; i < len(verifiedTimes) && i < 3; i++ {
		first = append(first, verifiedTimes[i])
	}
	for i := len(verifiedTimes) - 3; i < len(verifiedTimes); i++ {
		if i >= 0 {
			last = append(last, verifiedTimes[i])
		}
	}

	updated, err := tx.Memory.UpdateOneID(existing.ID).
		SetUpdatedAt(now).
		SetConfidence(mergedConfidence).
		SetChainMetadata(&entschema.MemoryChainMetadata{
			DuplicateCount:          duplicateCount,
			VerifiedTimestampsFirst: first,
			VerifiedTimestampsLast:  last,
			MergedFromMemoryIDs:     existingChainSourceIDs(existing),
		}).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &entschema.MemoryMergeUndoSnapshot{
		PriorConfidence:          priorConfidence,
		PriorChainMetadata:       priorChain,
		PriorChainMetadataWasNil: priorChainWasNil,
	}
	if _, err := tx.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(existing.ID).
		SetMergeType(entmerge.MergeTypeFoldLive).
		SetContent(updated.Content).
		SetDuplicatesFolded(extract.BatchDuplicateCount).
		SetSourceMembers(toEntSourceMembers(sourceMembers)).
		SetSnapshot(snapshot).
		SetNillableCompactionEventID(compactionEventID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		return nil, err
	}

	return toMemoryModel(updated), nil
}

func existingChainSourceIDs(existing *ent.Memory) []uuid.UUID {
	if existing.ChainMetadata == nil {
		return nil
	}
	return append([]uuid.UUID(nil), existing.ChainMetadata.MergedFromMemoryIDs...)
}

// createMergedMemory inserts a new memory (and optional embedding / compaction audit attach)
// on the caller's transaction. It never commits or rolls back tx: every error path returns
// without touching tx lifecycle, so callers (PersistMemoryMergeGroup, MergeLiveExtractedMemory)
// MUST Rollback on any non-nil error before returning to their own callers.
func (d *Datastore) createMergedMemory(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
	chatID uuid.UUID,
	extract memoryutil.CollapsedExtractedMemory,
	embeddingVector []float32,
	activePersonalityID uuid.UUID,
	targetScope memory.Scope,
	now time.Time,
	compactionEventID *uuid.UUID,
) (*models.Memory, error) {
	confidence := extract.Confidence
	if confidence == "" {
		confidence = models.MemoryConfidenceMedium
	}

	create := tx.Memory.Create().
		SetContent(extract.Content).
		SetScope(targetScope).
		SetType(memory.TypeContext).
		SetStatus(memory.StatusActive).
		SetConfidence(confidence.Float()).
		SetOwnerID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now)

	if extract.BatchDuplicateCount > 1 {
		create = create.SetChainMetadata(&entschema.MemoryChainMetadata{
			DuplicateCount:          extract.BatchDuplicateCount,
			VerifiedTimestampsFirst: []time.Time{now},
			VerifiedTimestampsLast:  []time.Time{now},
		})
	}

	if targetScope == memory.ScopeChat {
		create = create.SetChatID(chatID)
	} else if activePersonalityID != uuid.Nil {
		p, pErr := tx.Personality.Get(ctx, activePersonalityID)
		if pErr == nil && p != nil && p.AutoPinMemories {
			create = create.SetPinnedPersonalityID(activePersonalityID)
		}
	}

	newMem, err := create.Save(ctx)
	if err != nil {
		return nil, err
	}

	if len(embeddingVector) > 0 {
		if _, err := tx.Embedding.Create().
			SetEmbedding(pgvector.NewVector(embeddingVector)).
			SetMemoryID(newMem.ID).
			Save(ctx); err != nil {
			return nil, err
		}
	}

	// New-only rows are not merge interactions: existing agent state did not change. Attach them
	// to the compaction audit's created_memories list instead of emitting a create-type merge event.
	if compactionEventID != nil && *compactionEventID != uuid.Nil {
		id := newMem.ID
		if err := d.appendCompactionEventCreatedMemoriesTx(ctx, tx, userID, *compactionEventID, []models.CompactionLoadedMemory{{
			MemoryID:   &id,
			Content:    newMem.Content,
			Scope:      string(newMem.Scope),
			Confidence: newMem.Confidence,
		}}); err != nil {
			if !errors.Is(err, ErrCompactionEventNotFound) {
				return nil, err
			}
			// Audit attach is best-effort: keep the new memory even if the compaction row is gone.
			d.logger.Warn("compaction event missing while attaching created memory",
				zap.String("compaction_event_id", compactionEventID.String()),
				zap.String("memory_id", id.String()),
				zap.String("user_id", userID.String()),
			)
		}
	}

	return toMemoryModel(newMem), nil
}

func (d *Datastore) ListMemoryMergeEvents(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MemoryMergeEventFilters) (*models.PaginatedResponse, error) {
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Merge history is relationships that changed existing agent state (folds + links).
	// Legacy create-type rows are hidden; new memories belong on CompactionEvent.created_memories.
	query := d.dbClient.MemoryMergeEvent.Query().
		Where(
			entmerge.UserIDEQ(userID),
			entmerge.MergeTypeNEQ(entmerge.MergeTypeCreate),
		).
		Order(ent.Desc(entmerge.FieldCreatedAt))

	if filters.Query != nil && *filters.Query != "" {
		query = query.Where(entmerge.ContentContainsFold(*filters.Query))
	}
	if filters.SurvivorMemoryID != nil {
		query = query.Where(entmerge.SurvivorMemoryIDEQ(*filters.SurvivorMemoryID))
	}
	if filters.MinDate != nil {
		query = query.Where(entmerge.CreatedAtGTE(*filters.MinDate))
	}
	if filters.MaxDate != nil {
		query = query.Where(entmerge.CreatedAtLTE(*filters.MaxDate))
	}
	if filters.ExcludeReverted {
		query = query.Where(entmerge.RevertedAtIsNil())
	}

	totalCount, err := query.Clone().Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "memory_merge_events"), zap.Error(err))
		return nil, err
	}

	offset := (pageNum - 1) * pageSize
	rows, err := query.Offset(offset).Limit(pageSize).All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "memory_merge_events"), zap.Error(err))
		return nil, err
	}

	results := make([]any, len(rows))
	for i, row := range rows {
		results[i] = toMemoryMergeEventModel(row)
	}

	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

func toMemoryMergeEventModel(row *ent.MemoryMergeEvent) *models.MemoryMergeEvent {
	if row == nil {
		return nil
	}
	event := &models.MemoryMergeEvent{
		ID:               row.ID,
		SurvivorMemoryID: row.SurvivorMemoryID,
		MergeType:        models.MemoryMergeType(row.MergeType),
		Content:          row.Content,
		DuplicatesFolded: row.DuplicatesFolded,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
	if row.RevertedAt != nil {
		t := row.RevertedAt.UTC()
		event.RevertedAt = &t
	}
	if row.LinkGroupID != nil {
		lg := *row.LinkGroupID
		event.LinkGroupID = &lg
	}
	if row.CompactionEventID != nil {
		ce := *row.CompactionEventID
		event.CompactionEventID = &ce
	}
	if len(row.SourceMembers) > 0 {
		event.SourceMembers = sourceMembersToModel(row.SourceMembers)
	}
	if row.Snapshot != nil {
		event.Snapshot = &models.MemoryMergeUndoSnapshot{
			PriorConfidence:          row.Snapshot.PriorConfidence,
			PriorChainMetadataWasNil: row.Snapshot.PriorChainMetadataWasNil,
		}
		if row.Snapshot.PriorChainMetadata != nil {
			event.Snapshot.PriorChainMetadata = chainMetadataToModel(row.Snapshot.PriorChainMetadata)
		}
	}
	return event
}

var ErrMemoryMergeEventNotFound = fmt.Errorf("memory merge event not found")
var ErrMemoryMergeAlreadyReverted = fmt.Errorf("memory merge event already reverted")

func (d *Datastore) UndoMemoryMergeEvent(ctx context.Context, userID, eventID uuid.UUID) (*models.MemoryMergeEvent, error) {
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

	event, err := tx.MemoryMergeEvent.Query().
		Where(
			entmerge.ID(eventID),
			entmerge.UserIDEQ(userID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			_ = tx.Rollback()
			return nil, ErrMemoryMergeEventNotFound
		}
		_ = tx.Rollback()
		return nil, err
	}
	if event.RevertedAt != nil {
		_ = tx.Rollback()
		return nil, ErrMemoryMergeAlreadyReverted
	}

	now := time.Now().UTC()
	switch event.MergeType {
	case entmerge.MergeTypeCreate:
		if err := undoCreateMerge(ctx, tx, userID, event.SurvivorMemoryID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	case entmerge.MergeTypeFoldLive:
		if err := undoFoldLiveMerge(ctx, tx, userID, event); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	case entmerge.MergeTypeLink:
		if err := undoLinkGroup(ctx, tx, userID, event); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	default:
		_ = tx.Rollback()
		return nil, fmt.Errorf("unsupported merge type: %s", event.MergeType)
	}

	updated, err := tx.MemoryMergeEvent.UpdateOneID(event.ID).
		SetRevertedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toMemoryMergeEventModel(updated), nil
}

// undoLinkGroup withdraws the cross-reference created by a link event: every member carrying this
// link_group_id is unlinked (set back to null). The memories themselves — including any created by
// the link — remain as standalone rows; only the relationship is removed.
func undoLinkGroup(ctx context.Context, tx *ent.Tx, userID uuid.UUID, event *ent.MemoryMergeEvent) error {
	if event.LinkGroupID == nil {
		return fmt.Errorf("link event missing link_group_id")
	}
	_, err := tx.Memory.Update().
		Where(
			memory.LinkGroupID(*event.LinkGroupID),
			memory.HasOwnerWith(user.ID(userID)),
		).
		ClearLinkGroupID().
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	return err
}

func undoCreateMerge(ctx context.Context, tx *ent.Tx, userID, survivorID uuid.UUID) error {
	exists, err := tx.Memory.Query().
		Where(
			memory.ID(survivorID),
			memory.HasOwnerWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrMemoryNotFound
	}

	if _, err := tx.Embedding.Delete().
		Where(embedding.HasMemoryWith(memory.ID(survivorID))).
		Exec(ctx); err != nil {
		return err
	}
	return tx.Memory.DeleteOneID(survivorID).Exec(ctx)
}

func undoFoldLiveMerge(ctx context.Context, tx *ent.Tx, userID uuid.UUID, event *ent.MemoryMergeEvent) error {
	if event.Snapshot == nil {
		return fmt.Errorf("merge event missing undo snapshot")
	}

	entMemory, err := tx.Memory.Query().
		Where(
			memory.ID(event.SurvivorMemoryID),
			memory.HasOwnerWith(user.ID(userID)),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrMemoryNotFound
		}
		return err
	}

	update := tx.Memory.UpdateOneID(entMemory.ID).
		SetUpdatedAt(time.Now().UTC()).
		SetConfidence(event.Snapshot.PriorConfidence)

	if event.Snapshot.PriorChainMetadataWasNil {
		update = update.ClearChainMetadata()
	} else if event.Snapshot.PriorChainMetadata != nil {
		update = update.SetChainMetadata(event.Snapshot.PriorChainMetadata)
	} else {
		update = update.ClearChainMetadata()
	}

	_, err = update.Save(ctx)
	return err
}

func toEntSourceMembers(members []models.MemoryMergeSourceMember) []entschema.MemoryMergeSourceMember {
	if len(members) == 0 {
		return nil
	}
	out := make([]entschema.MemoryMergeSourceMember, len(members))
	for i, member := range members {
		out[i] = entschema.MemoryMergeSourceMember{
			Content:    member.Content,
			Scope:      member.Scope,
			Confidence: string(member.Confidence),
			MemoryID:   member.MemoryID,
			IsNew:      member.IsNew,
		}
	}
	return out
}

func sourceMembersToModel(members []entschema.MemoryMergeSourceMember) []models.MemoryMergeSourceMember {
	if len(members) == 0 {
		return nil
	}
	out := make([]models.MemoryMergeSourceMember, len(members))
	for i, member := range members {
		out[i] = models.MemoryMergeSourceMember{
			Content:    member.Content,
			Scope:      member.Scope,
			Confidence: models.MemoryConfidence(member.Confidence),
			MemoryID:   member.MemoryID,
			IsNew:      member.IsNew,
		}
	}
	return out
}

func sourceMembersForLiveFold(existing *ent.Memory, extract memoryutil.CollapsedExtractedMemory) []models.MemoryMergeSourceMember {
	confidence := models.MemoryConfidenceFromFloat(existing.Confidence)
	id := existing.ID
	members := []models.MemoryMergeSourceMember{{
		Content:    existing.Content,
		Scope:      string(existing.Scope),
		Confidence: confidence,
		MemoryID:   &id,
	}}
	count := extract.BatchDuplicateCount
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		members = append(members, models.MemoryMergeSourceMember{
			Content:    extract.Content,
			Scope:      extract.Scope,
			Confidence: extract.Confidence,
			IsNew:      true,
		})
	}
	return members
}
