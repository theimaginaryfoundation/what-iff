package datastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entsnap "github.com/theimaginaryfoundation/what-iff/ent/checkpointsnapshot"
	entcompaction "github.com/theimaginaryfoundation/what-iff/ent/compactionevent"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	entpersonality "github.com/theimaginaryfoundation/what-iff/ent/personality"
	entschema "github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	// ErrCompactionEventNotFound is returned when a compaction event does not exist for the user.
	ErrCompactionEventNotFound = fmt.Errorf("compaction event not found")
	// ErrCheckpointSnapshotNotFound is returned when a snapshot does not exist for the user.
	ErrCheckpointSnapshotNotFound = fmt.Errorf("checkpoint snapshot not found")
	// ErrCheckpointSnapshotOwnerMissing is returned when a snapshot lacks the owner reference (chat
	// for summary, personality for scratchpad) needed to revert it.
	ErrCheckpointSnapshotOwnerMissing = fmt.Errorf("checkpoint snapshot missing owner reference")
)

const maxCompactionEventsPageSize = 100

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// getOrCreateCheckpointSnapshotTx returns the content-addressed snapshot row for the given
// (user, kind, owner, content), creating it if absent. Because rows are reused, the snapshot a
// compaction records as "new" is the same row the next compaction records as "old" — no duplication.
func (d *Datastore) getOrCreateCheckpointSnapshotTx(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
	kind entsnap.Kind,
	chatID *uuid.UUID,
	personalityID *uuid.UUID,
	content string,
) (*ent.CheckpointSnapshot, error) {
	hash := contentHash(content)
	q := tx.CheckpointSnapshot.Query().Where(
		entsnap.UserID(userID),
		entsnap.KindEQ(kind),
		entsnap.ContentHash(hash),
	)
	if chatID != nil {
		q = q.Where(entsnap.ChatID(*chatID))
	} else {
		q = q.Where(entsnap.ChatIDIsNil())
	}
	if personalityID != nil {
		q = q.Where(entsnap.PersonalityID(*personalityID))
	} else {
		q = q.Where(entsnap.PersonalityIDIsNil())
	}

	existing, err := q.First(ctx)
	if err == nil {
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}

	create := tx.CheckpointSnapshot.Create().
		SetUserID(userID).
		SetKind(kind).
		SetContent(content).
		SetContentHash(hash)
	if chatID != nil {
		create = create.SetChatID(*chatID)
	}
	if personalityID != nil {
		create = create.SetPersonalityID(*personalityID)
	}
	return create.Save(ctx)
}

// CreateCompactionEvent records the start of a compaction: old summary, old/new scratchpad, loaded
// memories and metadata. The new summary is unknown at this point and is attached later via
// SetCompactionEventNewSummary. Returns the event so its ID can be threaded into the merge events.
func (d *Datastore) CreateCompactionEvent(ctx context.Context, userID uuid.UUID, input models.CompactionEventInput) (*models.CompactionEvent, error) {
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

	chatID := input.ChatID

	oldSummary, err := d.getOrCreateCheckpointSnapshotTx(ctx, tx, userID, entsnap.KindSummary, &chatID, nil, input.OldSummary)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	create := tx.CompactionEvent.Create().
		SetUserID(userID).
		SetChatID(chatID).
		SetNillablePersonalityID(input.PersonalityID).
		SetNillableAssistantMessageID(input.AssistantMessageID).
		SetProvider(input.Provider).
		SetReason(input.Reason).
		SetOldSummaryID(oldSummary.ID).
		SetLoadedMemories(toEntLoadedMemories(input.LoadedMemories))

	// Scratchpad snapshots only exist when the checkpoint had a personality to update.
	if input.HasScratchpad && input.PersonalityID != nil {
		oldScratch, err := d.getOrCreateCheckpointSnapshotTx(ctx, tx, userID, entsnap.KindScratchpad, nil, input.PersonalityID, input.OldScratchpad)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		newScratch, err := d.getOrCreateCheckpointSnapshotTx(ctx, tx, userID, entsnap.KindScratchpad, nil, input.PersonalityID, input.NewScratchpad)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		create = create.SetOldScratchpadID(oldScratch.ID).SetNewScratchpadID(newScratch.ID)
	}

	event, err := create.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	full, err := d.loadCompactionEvent(ctx, userID, event.ID)
	if err != nil {
		return nil, err
	}
	return full, nil
}

// AppendCompactionEventCreatedMemories appends newly created memory snapshots onto a compaction
// audit record. Empty inputs are no-ops. Missing/pruned compaction events are best-effort: the
// helper returns nil so memory persistence is never blocked by audit attach.
func (d *Datastore) AppendCompactionEventCreatedMemories(ctx context.Context, userID, eventID uuid.UUID, memories []models.CompactionLoadedMemory) error {
	if len(memories) == 0 {
		return nil
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := d.appendCompactionEventCreatedMemoriesTx(ctx, tx, userID, eventID, memories); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, ErrCompactionEventNotFound) {
			return nil
		}
		return err
	}
	return tx.Commit()
}

// appendCompactionEventCreatedMemoriesTx uses Ent's sqljson.Append so concurrent writers for the
// same compaction event do not clobber each other via read-modify-write.
func (d *Datastore) appendCompactionEventCreatedMemoriesTx(ctx context.Context, tx *ent.Tx, userID, eventID uuid.UUID, memories []models.CompactionLoadedMemory) error {
	if len(memories) == 0 {
		return nil
	}
	n, err := tx.CompactionEvent.Update().
		Where(entcompaction.ID(eventID), entcompaction.UserID(userID)).
		AppendCreatedMemories(toEntLoadedMemories(memories)).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCompactionEventNotFound
	}
	return nil
}

// SetCompactionEventNewSummary attaches the post-checkpoint summary snapshot to an existing event.
func (d *Datastore) SetCompactionEventNewSummary(ctx context.Context, userID, eventID uuid.UUID, newSummary string) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	event, err := tx.CompactionEvent.Query().
		Where(entcompaction.ID(eventID), entcompaction.UserID(userID)).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return ErrCompactionEventNotFound
		}
		return err
	}

	snap, err := d.getOrCreateCheckpointSnapshotTx(ctx, tx, userID, entsnap.KindSummary, &event.ChatID, nil, newSummary)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.CompactionEvent.UpdateOneID(event.ID).SetNewSummaryID(snap.ID).Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Datastore) loadCompactionEvent(ctx context.Context, userID, eventID uuid.UUID) (*models.CompactionEvent, error) {
	row, err := d.dbClient.CompactionEvent.Query().
		Where(entcompaction.ID(eventID), entcompaction.UserID(userID)).
		WithOldSummary().
		WithNewSummary().
		WithOldScratchpad().
		WithNewScratchpad().
		WithMergeEvents(func(q *ent.MemoryMergeEventQuery) {
			q.Order(ent.Asc(entmerge.FieldCreatedAt))
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrCompactionEventNotFound
		}
		return nil, err
	}
	event := toCompactionEventModel(row)
	d.populateCompactionEventChatNames(ctx, userID, []*models.CompactionEvent{event})
	return event, nil
}

// GetCompactionEvent returns one compaction event with all snapshots and merge events resolved.
func (d *Datastore) GetCompactionEvent(ctx context.Context, userID, eventID uuid.UUID) (*models.CompactionEvent, error) {
	return d.loadCompactionEvent(ctx, userID, eventID)
}

// ListCompactionEvents returns paginated compaction events (newest first) with snapshots and merge
// events resolved.
func (d *Datastore) ListCompactionEvents(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, chatID, personalityID *uuid.UUID) (*models.PaginatedResponse, error) {
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > maxCompactionEventsPageSize {
		pageSize = maxCompactionEventsPageSize
	}

	query := d.dbClient.CompactionEvent.Query().Where(entcompaction.UserID(userID))
	if chatID != nil {
		query = query.Where(entcompaction.ChatID(*chatID))
	}
	if personalityID != nil {
		query = query.Where(entcompaction.PersonalityID(*personalityID))
	}
	totalCount, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := query.
		Order(ent.Desc(entcompaction.FieldCreatedAt)).
		Offset((pageNum - 1) * pageSize).
		Limit(pageSize).
		WithOldSummary().
		WithNewSummary().
		WithOldScratchpad().
		WithNewScratchpad().
		WithMergeEvents(func(q *ent.MemoryMergeEventQuery) {
			q.Order(ent.Asc(entmerge.FieldCreatedAt))
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}

	events := make([]*models.CompactionEvent, len(rows))
	for i, row := range rows {
		events[i] = toCompactionEventModel(row)
	}
	d.populateCompactionEventChatNames(ctx, userID, events)

	results := make([]any, len(events))
	for i, event := range events {
		results[i] = event
	}
	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// populateCompactionEventChatNames best-effort enriches audit events with their current chat names.
// The event audit record remains useful when a chat has been deleted or this optional lookup fails.
func (d *Datastore) populateCompactionEventChatNames(ctx context.Context, userID uuid.UUID, events []*models.CompactionEvent) {
	chatIDs := make([]uuid.UUID, 0, len(events))
	seen := make(map[uuid.UUID]struct{}, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		if _, ok := seen[event.ChatID]; ok {
			continue
		}
		seen[event.ChatID] = struct{}{}
		chatIDs = append(chatIDs, event.ChatID)
	}
	if len(chatIDs) == 0 {
		return
	}

	chats, err := d.dbClient.Chat.Query().
		Where(entchat.IDIn(chatIDs...), entchat.HasOwnerWith(user.ID(userID))).
		Select(entchat.FieldID, entchat.FieldName).
		All(ctx)
	if err != nil {
		d.logger.Warn("failed to load chat names for compaction events",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		return
	}
	names := make(map[uuid.UUID]string, len(chats))
	for _, chat := range chats {
		names[chat.ID] = chat.Name
	}
	for _, event := range events {
		if event != nil {
			event.ChatName = names[event.ChatID]
		}
	}
}

// RevertCheckpointSnapshot restores a snapshot's content to the live summary or scratchpad it came
// from. Summary snapshots restore Chat.checkpoint_summary; scratchpad snapshots restore
// Personality.scratchpad (which is shared across that personality's chats). Snapshot rows are never
// mutated or deleted — reverting only rewrites the live value.
func (d *Datastore) RevertCheckpointSnapshot(ctx context.Context, userID, snapshotID uuid.UUID) (*models.CheckpointSnapshot, error) {
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

	snap, err := tx.CheckpointSnapshot.Query().
		Where(entsnap.ID(snapshotID), entsnap.UserID(userID)).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, ErrCheckpointSnapshotNotFound
		}
		return nil, err
	}

	switch snap.Kind {
	case entsnap.KindSummary:
		if snap.ChatID == nil {
			_ = tx.Rollback()
			return nil, ErrCheckpointSnapshotOwnerMissing
		}
		updated, err := tx.Chat.Update().
			Where(entchat.ID(*snap.ChatID), entchat.HasOwnerWith(user.ID(userID))).
			SetCheckpointSummary(snap.Content).
			Save(ctx)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if updated == 0 {
			_ = tx.Rollback()
			return nil, ErrChatNotFound
		}
	case entsnap.KindScratchpad:
		if snap.PersonalityID == nil {
			_ = tx.Rollback()
			return nil, ErrCheckpointSnapshotOwnerMissing
		}
		updated, err := tx.Personality.Update().
			Where(entpersonality.ID(*snap.PersonalityID), entpersonality.HasUserWith(user.ID(userID))).
			SetScratchpad(snap.Content).
			Save(ctx)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if updated == 0 {
			_ = tx.Rollback()
			return nil, ErrPersonalityNotFound
		}
	default:
		_ = tx.Rollback()
		return nil, fmt.Errorf("unsupported snapshot kind: %s", snap.Kind)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toCheckpointSnapshotModel(snap), nil
}

func toEntLoadedMemories(memories []models.CompactionLoadedMemory) []entschema.CompactionLoadedMemory {
	if len(memories) == 0 {
		return nil
	}
	out := make([]entschema.CompactionLoadedMemory, len(memories))
	for i, m := range memories {
		out[i] = entschema.CompactionLoadedMemory{
			MemoryID:   m.MemoryID,
			Content:    m.Content,
			Scope:      m.Scope,
			Confidence: m.Confidence,
		}
	}
	return out
}

func loadedMemoriesToModel(memories []entschema.CompactionLoadedMemory) []models.CompactionLoadedMemory {
	if len(memories) == 0 {
		return nil
	}
	out := make([]models.CompactionLoadedMemory, len(memories))
	for i, m := range memories {
		out[i] = models.CompactionLoadedMemory{
			MemoryID:   m.MemoryID,
			Content:    m.Content,
			Scope:      m.Scope,
			Confidence: m.Confidence,
		}
	}
	return out
}

func toCheckpointSnapshotModel(row *ent.CheckpointSnapshot) *models.CheckpointSnapshot {
	if row == nil {
		return nil
	}
	return &models.CheckpointSnapshot{
		ID:            row.ID,
		Kind:          models.CheckpointSnapshotKind(row.Kind),
		ChatID:        row.ChatID,
		PersonalityID: row.PersonalityID,
		Content:       row.Content,
		CreatedAt:     row.CreatedAt,
	}
}

func toCompactionEventModel(row *ent.CompactionEvent) *models.CompactionEvent {
	if row == nil {
		return nil
	}
	event := &models.CompactionEvent{
		ID:                 row.ID,
		ChatID:             row.ChatID,
		PersonalityID:      row.PersonalityID,
		AssistantMessageID: row.AssistantMessageID,
		Provider:           row.Provider,
		Reason:             row.Reason,
		LoadedMemories:     loadedMemoriesToModel(row.LoadedMemories),
		CreatedMemories:    loadedMemoriesToModel(row.CreatedMemories),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
	if row.Edges.OldSummary != nil {
		event.OldSummary = toCheckpointSnapshotModel(row.Edges.OldSummary)
	}
	if row.Edges.NewSummary != nil {
		event.NewSummary = toCheckpointSnapshotModel(row.Edges.NewSummary)
	}
	if row.Edges.OldScratchpad != nil {
		event.OldScratchpad = toCheckpointSnapshotModel(row.Edges.OldScratchpad)
	}
	if row.Edges.NewScratchpad != nil {
		event.NewScratchpad = toCheckpointSnapshotModel(row.Edges.NewScratchpad)
	}
	if len(row.Edges.MergeEvents) > 0 {
		event.MergeEvents = make([]models.MemoryMergeEvent, 0, len(row.Edges.MergeEvents))
		for _, me := range row.Edges.MergeEvents {
			if m := toMemoryMergeEventModel(me); m != nil {
				event.MergeEvents = append(event.MergeEvents, *m)
			}
		}
	}
	return event
}
