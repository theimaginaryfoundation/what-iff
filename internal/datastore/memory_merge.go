package datastore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
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

// MergeGroupOption tunes PersistMemoryMergeGroup and PersistMemoryLinkGroup. Options are variadic
// so existing callers are unchanged.
type MergeGroupOption func(*mergeGroupOptions)

type mergeGroupOptions struct {
	chatOnly bool
}

// WithChatMemoriesOnly confines a merge or link to the asking chat. It is the datastore-level
// guard for sandboxed chats, whatever the caller's plan says:
//   - it may not fold, rewrite, retire or relink a memory made elsewhere (the owner's, or another
//     conversation's): any such member makes the call fail with ErrMemoryOutsideChat before
//     anything is written;
//   - anything it creates is Chat-scoped to the asking chat, never User-scoped.
func WithChatMemoriesOnly() MergeGroupOption {
	return func(o *mergeGroupOptions) { o.chatOnly = true }
}

// ensureMemoriesCreatedInChatTx fails with ErrMemoryOutsideChat when any of ids is one of the
// user's memories that is not chatID's own (memoryOfChat: created in it and Chat- or
// Summary-scoped). IDs that do not exist are ignored (the writes that follow treat them as
// no-ops).
func ensureMemoriesCreatedInChatTx(ctx context.Context, tx *ent.Tx, userID, chatID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	outside, err := tx.Memory.Query().
		Where(
			memory.IDIn(ids...),
			memory.HasOwnerWith(user.ID(userID)),
			memory.Not(memoryOfChat(chatID)),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if outside > 0 {
		return ErrMemoryOutsideChat
	}
	return nil
}

// PersistMemoryMergeGroup writes one merge grouping using only known memory IDs from
// thread context (no duplicate scans). When survivorMemoryID is set, that row is
// updated in place; absorbMemoryIDs are soft-retired when consolidating duplicates.
//
// embeddingVector is an embedding of group.CanonicalContent. With no survivor it embeds the new
// row. With a survivor it lets the fold adopt the canonical phrasing (see decideSurvivorRewrite);
// pass nil when the caller did not embed, and the survivor keeps its wording. The datastore never
// calls the embedding API itself.
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
	opts ...MergeGroupOption,
) (*models.Memory, error) {
	var mergeOpts mergeGroupOptions
	for _, opt := range opts {
		opt(&mergeOpts)
	}
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
	if group.Scope != string(memory.ScopeUser) && group.Scope != string(memory.ScopeChat) || mergeOpts.chatOnly {
		targetScope = memory.ScopeChat
	}
	if mergeOpts.chatOnly {
		var existingIDs []uuid.UUID
		if survivorMemoryID != nil && *survivorMemoryID != uuid.Nil {
			existingIDs = append(existingIDs, *survivorMemoryID)
		}
		existingIDs = append(existingIDs, absorbMemoryIDs...)
		if err := ensureMemoriesCreatedInChatTx(ctx, tx, userID, chatID, existingIDs); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	confidence := group.Confidence
	if confidence == "" {
		confidence = models.MemoryConfidenceMedium
	}

	if survivorMemoryID != nil && *survivorMemoryID != uuid.Nil {
		// Lock the survivor for the rest of the transaction so a concurrent user edit or star
		// cannot land between this read and the fold's write (and be overwritten by it).
		survivorQuery := tx.Memory.Query().
			Where(
				memory.ID(*survivorMemoryID),
				memory.HasOwnerWith(user.ID(userID)),
				memory.StatusEQ(memory.StatusActive),
			)
		survivorQuery.Modify(lockRowsForUpdate)
		existing, err := survivorQuery.Only(ctx)
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
		absorbed, err := retireAbsorbedMemoriesTx(ctx, tx, userID, existing.ID, absorbMemoryIDs, now)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		mem, foldErr := d.foldIntoLiveMemory(ctx, tx, userID, existing, extract, now, foldLiveOptions{
			sourceMembers:      sourceMembers,
			compactionEventID:  compactionEventID,
			canonicalEmbedding: embeddingVector,
			absorbed:           absorbed,
		})
		if foldErr != nil {
			_ = tx.Rollback()
			return nil, foldErr
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
// restoring a prior group. New members are auto-pinned to activePersonalityID under the same rule
// as any other new memory (autoPinPersonalityIDTx); existing members are never repinned.
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
	activePersonalityID uuid.UUID,
	opts ...MergeGroupOption,
) (*models.MemoryMergeEvent, error) {
	var linkOpts mergeGroupOptions
	for _, opt := range opts {
		opt(&linkOpts)
	}
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
	if scope != string(memory.ScopeUser) && scope != string(memory.ScopeChat) || linkOpts.chatOnly {
		targetScope = memory.ScopeChat
	}
	if linkOpts.chatOnly {
		if err := ensureMemoriesCreatedInChatTx(ctx, tx, userID, chatID, existingMemberIDs); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}

	// New members follow the same auto-pin rule as any other new memory; existing members keep
	// whatever pin they already have.
	var pinnedPersonalityID *uuid.UUID
	if len(newMembers) > 0 {
		pinnedPersonalityID = d.autoPinPersonalityIDTx(ctx, tx, targetScope, activePersonalityID)
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
		if pinnedPersonalityID != nil {
			create = create.SetPinnedPersonalityID(*pinnedPersonalityID)
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
		// liveMatch already equals extract.Content after normalization, so this fold never rewrites
		// the survivor and needs no embedding.
		mem, mergeErr := d.foldIntoLiveMemory(ctx, tx, userID, liveMatch, extract, now, foldLiveOptions{sourceMembers: sourceMembers})
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

// lockRowsForUpdate is a query modifier that locks the selected rows until the transaction ends
// (SELECT ... FOR UPDATE). Only Postgres gets the clause: SQLite, which the datastore tests run
// on, has no row locks and rejects FOR UPDATE, and it serializes writers per database anyway.
func lockRowsForUpdate(s *sql.Selector) {
	if s.Dialect() == dialect.Postgres {
		s.ForUpdate()
	}
}

// foldLiveOptions carries the optional inputs of a fold_live write.
type foldLiveOptions struct {
	sourceMembers     []models.MemoryMergeSourceMember
	compactionEventID *uuid.UUID
	// canonicalEmbedding embeds extract.Content; without it the survivor is never rewritten.
	canonicalEmbedding []float32
	// absorbed are the memories this fold retired (retireAbsorbedMemoriesTx), recorded for undo.
	absorbed []entschema.MemoryMergeAbsorbedMember
}

// survivorRewriteDecision is the outcome of decideSurvivorRewrite; non-rewrite values double as
// log reasons.
type survivorRewriteDecision string

const (
	survivorRewrite            survivorRewriteDecision = "rewrite"
	survivorKeepSameContent    survivorRewriteDecision = "same_content"
	survivorKeepStarred        survivorRewriteDecision = "starred"
	survivorKeepNoNewEmbedding survivorRewriteDecision = "no_embedding"
)

// decideSurvivorRewrite is the fold rewrite rule (#249). A fold replaces the survivor's content
// with the merger's canonical phrasing only when ALL of these hold:
//  1. The canonical content differs from the survivor's after NormalizeContentForDedupe. A
//     difference in case or whitespace alone keeps the stored wording.
//  2. The survivor is not starred. Starring is the user's explicit "keep this" signal, so an
//     automatic merge never rewords a starred memory; the fold still bumps its tally and retires
//     the absorbed duplicates. There is no author field yet; when one exists, user-authored
//     memories should get the same protection here.
//  3. The caller supplied an embedding of the canonical content. Content and embedding change
//     together or not at all, otherwise recall would match the old wording to the new text.
func decideSurvivorRewrite(existing *ent.Memory, canonical string, canonicalEmbedding []float32) survivorRewriteDecision {
	normalized := memoryutil.NormalizeContentForDedupe(canonical)
	if normalized == "" || normalized == memoryutil.NormalizeContentForDedupe(existing.Content) {
		return survivorKeepSameContent
	}
	if existing.Starred {
		return survivorKeepStarred
	}
	if len(canonicalEmbedding) == 0 {
		return survivorKeepNoNewEmbedding
	}
	return survivorRewrite
}

func (d *Datastore) foldIntoLiveMemory(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
	existing *ent.Memory,
	extract memoryutil.CollapsedExtractedMemory,
	now time.Time,
	opts foldLiveOptions,
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

	snapshot := &entschema.MemoryMergeUndoSnapshot{
		PriorConfidence:          priorConfidence,
		PriorChainMetadata:       priorChain,
		PriorChainMetadataWasNil: priorChainWasNil,
		Version:                  entschema.MemoryMergeUndoSnapshotVersion,
		CanonicalContent:         strings.TrimSpace(extract.Content),
		AbsorbedMembers:          opts.absorbed,
	}

	update := tx.Memory.UpdateOneID(existing.ID).
		SetUpdatedAt(now).
		SetConfidence(mergedConfidence).
		SetChainMetadata(&entschema.MemoryChainMetadata{
			DuplicateCount:          duplicateCount,
			VerifiedTimestampsFirst: first,
			VerifiedTimestampsLast:  last,
			MergedFromMemoryIDs:     existingChainSourceIDs(existing),
		})
	switch decision := decideSurvivorRewrite(existing, extract.Content, opts.canonicalEmbedding); decision {
	case survivorRewrite:
		priorVector, hadEmbedding, err := replaceMemoryEmbeddingTx(ctx, tx, existing.ID, opts.canonicalEmbedding)
		if err != nil {
			return nil, err
		}
		snapshot.ContentRewritten = true
		snapshot.PriorContent = existing.Content
		snapshot.PriorEmbedding = priorVector
		snapshot.PriorEmbeddingMissing = !hadEmbedding
		update = update.SetContent(snapshot.CanonicalContent)
	case survivorKeepStarred, survivorKeepNoNewEmbedding:
		d.logger.Info("memory fold kept the survivor's wording instead of the canonical content",
			zap.String("reason", string(decision)),
			zap.String("memory_id", existing.ID.String()),
			zap.String("user_id", userID.String()),
		)
	}

	updated, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}

	// content is the survivor's text AFTER the fold, i.e. the canonical phrasing when rewritten;
	// the snapshot keeps the proposal even when the survivor declined it.
	if _, err := tx.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(existing.ID).
		SetMergeType(entmerge.MergeTypeFoldLive).
		SetContent(updated.Content).
		SetDuplicatesFolded(extract.BatchDuplicateCount).
		SetSourceMembers(toEntSourceMembers(opts.sourceMembers)).
		SetSnapshot(snapshot).
		SetNillableCompactionEventID(opts.compactionEventID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		return nil, err
	}

	return toMemoryModel(updated), nil
}

// errDuplicateMemoryEmbedding means a memory has more than one embedding row. Embeddings are keyed
// by memory ID, so this is a data bug; replaceMemoryEmbeddingTx refuses to guess which row a fold
// snapshots (and undo restores) and fails the transaction instead.
var errDuplicateMemoryEmbedding = errors.New("memory has more than one embedding row")

// replaceMemoryEmbeddingTx points memoryID's embedding at vector, creating the row when the memory
// had none. It returns the previous vector (nil when there was none) and whether a row existed, so
// a fold can snapshot it for undo. A memory has at most one embedding row; more than one returns
// errDuplicateMemoryEmbedding with nothing written, so the snapshot always matches what is replaced.
func replaceMemoryEmbeddingTx(ctx context.Context, tx *ent.Tx, memoryID uuid.UUID, vector []float32) ([]float32, bool, error) {
	rows, err := tx.Embedding.Query().
		Where(embedding.HasMemoryWith(memory.ID(memoryID))).
		All(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		// Key the new row by the memory ID (as account import and SetMemoryEmbedding do), so a
		// concurrent writer for the same memory conflicts on the primary key and upserts instead of
		// leaving a second embedding row.
		if err := tx.Embedding.Create().
			SetID(memoryID).
			SetEmbedding(pgvector.NewVector(vector)).
			SetMemoryID(memoryID).
			OnConflictColumns(embedding.FieldID).
			UpdateNewValues().
			Exec(ctx); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if len(rows) > 1 {
		return nil, false, fmt.Errorf("%w: memory %s has %d", errDuplicateMemoryEmbedding, memoryID, len(rows))
	}
	prior := append([]float32(nil), rows[0].Embedding.Slice()...)
	if _, err := tx.Embedding.Update().
		Where(embedding.HasMemoryWith(memory.ID(memoryID))).
		SetEmbedding(pgvector.NewVector(vector)).
		Save(ctx); err != nil {
		return nil, false, err
	}
	return prior, true, nil
}

// retireAbsorbedMemoriesTx sets the memories a fold absorbs to inactive and returns each one's
// prior status for the undo snapshot. Only rows the user owns are touched, and never the survivor.
//
// Embeddings are deliberately KEPT (#250). Recall only searches active memories
// (GetRelatedMemories and GetRelatedSummaryMemories both filter status=active), so an inactive row
// with an embedding is already invisible, and undo can make it visible again exactly, without
// re-embedding.
func retireAbsorbedMemoriesTx(ctx context.Context, tx *ent.Tx, userID, survivorID uuid.UUID, ids []uuid.UUID, now time.Time) ([]entschema.MemoryMergeAbsorbedMember, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := tx.Memory.Query().
		Where(
			memory.IDIn(ids...),
			memory.IDNEQ(survivorID),
			memory.HasOwnerWith(user.ID(userID)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	priorStatus := make(map[uuid.UUID]memory.Status, len(rows))
	for _, row := range rows {
		priorStatus[row.ID] = row.Status
	}
	// Keep the caller's order (stable audit rows) and drop repeats.
	absorbed := make([]entschema.MemoryMergeAbsorbedMember, 0, len(rows))
	retiredIDs := make([]uuid.UUID, 0, len(rows))
	for _, id := range ids {
		status, ok := priorStatus[id]
		if !ok {
			continue
		}
		delete(priorStatus, id)
		absorbed = append(absorbed, entschema.MemoryMergeAbsorbedMember{MemoryID: id, PriorStatus: string(status)})
		retiredIDs = append(retiredIDs, id)
	}
	if _, err := tx.Memory.Update().
		Where(memory.IDIn(retiredIDs...)).
		SetStatus(memory.StatusInactive).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		return nil, err
	}
	return absorbed, nil
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
	}
	if pinned := d.autoPinPersonalityIDTx(ctx, tx, targetScope, activePersonalityID); pinned != nil {
		create = create.SetPinnedPersonalityID(*pinned)
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
	if filters.OnlyChatID != nil {
		// Only folds whose survivor memory is the chat's own (created in it and Chat-scoped, as
		// memoryOfChat): a link's members can come from anywhere, so link events are left out. The
		// survivor id is a plain column (no edge), hence the subquery on the memories table.
		chatID := *filters.OnlyChatID
		query = query.Where(
			entmerge.MergeTypeEQ(entmerge.MergeTypeFoldLive),
			func(s *sql.Selector) {
				t := sql.Table(memory.Table)
				s.Where(sql.In(
					s.C(entmerge.FieldSurvivorMemoryID),
					sql.Select(t.C(memory.FieldID)).From(t).Where(sql.And(
						sql.EQ(t.C(memory.ChatColumn), chatID),
						sql.In(t.C(memory.FieldScope), memory.ScopeChat, memory.ScopeSummary),
					)),
				))
			},
		)
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
		if err := d.undoFoldLiveMerge(ctx, tx, userID, event); err != nil {
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

// undoFoldLiveMerge reverts a fold_live event: the survivor gets back its confidence, chain
// metadata and, when the fold rewrote it, its content and embedding; every absorbed memory gets
// back its prior status.
//
// Events written before the snapshot was versioned (Version 0) carry no absorbed-member list, so
// the absorbed set is read from source_members (stored rows other than the survivor) and only rows
// still inactive are reactivated. Those older folds hard-deleted the absorbed embeddings, so a
// reactivated row may have none; it is logged so a backfill can re-embed it, rather than failing
// the undo or calling the embedding API from the datastore.
func (d *Datastore) undoFoldLiveMerge(ctx context.Context, tx *ent.Tx, userID uuid.UUID, event *ent.MemoryMergeEvent) error {
	snap := event.Snapshot
	if snap == nil {
		return fmt.Errorf("merge event missing undo snapshot")
	}

	survivorQuery := tx.Memory.Query().
		Where(
			memory.ID(event.SurvivorMemoryID),
			memory.HasOwnerWith(user.ID(userID)),
		)
	survivorQuery.Modify(lockRowsForUpdate)
	entMemory, err := survivorQuery.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrMemoryNotFound
		}
		return err
	}

	now := time.Now().UTC()
	update := tx.Memory.UpdateOneID(entMemory.ID).
		SetUpdatedAt(now).
		SetConfidence(snap.PriorConfidence)

	if snap.PriorChainMetadataWasNil {
		update = update.ClearChainMetadata()
	} else if snap.PriorChainMetadata != nil {
		update = update.SetChainMetadata(snap.PriorChainMetadata)
	} else {
		update = update.ClearChainMetadata()
	}

	if snap.ContentRewritten {
		// Only put the prior wording back while the survivor still holds the fold's text
		// (event.Content). If the user edited it, or a later fold rewrote it, that newer text wins:
		// the rest of the fold is still undone, but the content and its embedding are left alone.
		if entMemory.Content == event.Content {
			update = update.SetContent(snap.PriorContent)
			if err := restoreSurvivorEmbeddingTx(ctx, tx, entMemory.ID, snap); err != nil {
				return err
			}
		} else {
			d.logger.Warn("fold undo kept the survivor's content: it changed after the fold",
				zap.String("merge_event_id", event.ID.String()),
				zap.String("memory_id", entMemory.ID.String()),
				zap.String("user_id", userID.String()),
			)
		}
	}

	if _, err := update.Save(ctx); err != nil {
		return err
	}

	return d.restoreAbsorbedMemoriesTx(ctx, tx, userID, event, string(entMemory.Scope), now)
}

// restoreSurvivorEmbeddingTx puts back the survivor's pre-rewrite embedding from the snapshot, or
// removes the rewrite's embedding when the survivor had none before.
func restoreSurvivorEmbeddingTx(ctx context.Context, tx *ent.Tx, memoryID uuid.UUID, snap *entschema.MemoryMergeUndoSnapshot) error {
	if snap.PriorEmbeddingMissing || len(snap.PriorEmbedding) == 0 {
		_, err := tx.Embedding.Delete().
			Where(embedding.HasMemoryWith(memory.ID(memoryID))).
			Exec(ctx)
		return err
	}
	_, _, err := replaceMemoryEmbeddingTx(ctx, tx, memoryID, snap.PriorEmbedding)
	return err
}

// restoreAbsorbedMemoriesTx returns each memory the fold retired to its prior status.
// survivorScope is the survivor's current scope, used only to pick a legacy event's absorbed set.
func (d *Datastore) restoreAbsorbedMemoriesTx(ctx context.Context, tx *ent.Tx, userID uuid.UUID, event *ent.MemoryMergeEvent, survivorScope string, now time.Time) error {
	byStatus := make(map[memory.Status][]uuid.UUID)
	legacy := event.Snapshot.Version < entschema.MemoryMergeUndoSnapshotVersion
	if legacy {
		// Pre-versioned events: absorbed rows were active candidates when folded.
		for _, id := range legacyAbsorbedMemoryIDs(event, survivorScope) {
			byStatus[memory.StatusActive] = append(byStatus[memory.StatusActive], id)
		}
	} else {
		for _, member := range event.Snapshot.AbsorbedMembers {
			status := memory.Status(member.PriorStatus)
			if memory.StatusValidator(status) != nil {
				status = memory.StatusActive
			}
			byStatus[status] = append(byStatus[status], member.MemoryID)
		}
	}

	var reactivated []uuid.UUID
	for status, ids := range byStatus {
		q := tx.Memory.Update().
			Where(
				memory.IDIn(ids...),
				memory.HasOwnerWith(user.ID(userID)),
			)
		if legacy {
			// Without a recorded prior status, only undo the fold's own effect.
			q = q.Where(memory.StatusEQ(memory.StatusInactive))
		}
		if _, err := q.SetStatus(status).SetUpdatedAt(now).Save(ctx); err != nil {
			return err
		}
		if status == memory.StatusActive {
			reactivated = append(reactivated, ids...)
		}
	}
	if len(reactivated) == 0 {
		return nil
	}

	// Folds before #250 hard-deleted absorbed embeddings. A reactivated row without one is active but
	// invisible to recall until something re-embeds it.
	active, err := tx.Memory.Query().
		Where(
			memory.IDIn(reactivated...),
			memory.HasOwnerWith(user.ID(userID)),
			memory.StatusEQ(memory.StatusActive),
		).
		IDs(ctx)
	if err != nil || len(active) == 0 {
		return err
	}
	embedded, err := tx.Embedding.Query().
		Where(embedding.HasMemoryWith(memory.IDIn(active...))).
		QueryMemory().
		IDs(ctx)
	if err != nil {
		return err
	}
	hasEmbedding := make(map[uuid.UUID]struct{}, len(embedded))
	for _, id := range embedded {
		hasEmbedding[id] = struct{}{}
	}
	var unembedded []uuid.UUID
	for _, id := range active {
		if _, ok := hasEmbedding[id]; !ok {
			unembedded = append(unembedded, id)
		}
	}
	if len(unembedded) > 0 {
		ids := make([]string, len(unembedded))
		for i, id := range unembedded {
			ids[i] = id.String()
		}
		d.logger.Warn("fold undo reactivated memories that have no embedding; they need a backfill re-embed before recall can find them",
			zap.String("merge_event_id", event.ID.String()),
			zap.String("user_id", userID.String()),
			zap.Strings("memory_ids", ids),
		)
	}
	return nil
}

// legacyAbsorbedMemoryIDs derives the absorbed set of a pre-versioned fold_live event from its
// source_members. It mirrors the old fold's rule (survivorMemoryIDForGroup in the agent): only
// stored members in the group's scope were absorbed, and the survivor's scope is the group's scope.
// That scope is read from the survivor's own source member, falling back to fallbackScope (the
// survivor row's current scope) when the event did not list it.
func legacyAbsorbedMemoryIDs(event *ent.MemoryMergeEvent, fallbackScope string) []uuid.UUID {
	groupScope := fallbackScope
	for _, member := range event.SourceMembers {
		if member.MemoryID != nil && *member.MemoryID == event.SurvivorMemoryID && member.Scope != "" {
			groupScope = member.Scope
			break
		}
	}
	seen := map[uuid.UUID]struct{}{event.SurvivorMemoryID: {}}
	var ids []uuid.UUID
	for _, member := range event.SourceMembers {
		if member.IsNew || member.MemoryID == nil || *member.MemoryID == uuid.Nil {
			continue
		}
		if !strings.EqualFold(member.Scope, groupScope) {
			continue
		}
		if _, dup := seen[*member.MemoryID]; dup {
			continue
		}
		seen[*member.MemoryID] = struct{}{}
		ids = append(ids, *member.MemoryID)
	}
	return ids
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
