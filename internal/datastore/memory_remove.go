package datastore

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmerge "github.com/theimaginaryfoundation/what-iff/ent/memorymergeevent"
	entschema "github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// RemoveMemory takes one memory out of active memory on an agent's behalf (remove_memory, ADR
// 0x036). It is a soft delete: the row stays, set inactive like a memory a merge retired, with
// removed_at and the removing personality and conversation on it, and a "remove" lifecycle event
// records who removed it and why. Undoing that event (UndoMemoryMergeEvent) restores the memory.
//
// Only an active Context memory the user has not starred can be removed. Anything else is refused
// with an ErrMemoryNotRemovable that says why, in words meant for the model. Which memories the
// asking conversation may see (sandbox, personality pins) is the caller's check.
func (d *Datastore) RemoveMemory(ctx context.Context, userID, memoryID uuid.UUID, removal models.MemoryRemoval) (*models.MemoryMergeEvent, error) {
	reason := strings.TrimSpace(removal.Reason)
	if utf8.RuneCountInString(reason) > models.MaxMemoryRemovalReasonRunes {
		return nil, fmt.Errorf("%w: the reason is longer than %d characters", ErrMemoryNotRemovable, models.MaxMemoryRemovalReasonRunes)
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

	query := tx.Memory.Query().Where(memory.ID(memoryID), memory.HasOwnerWith(user.ID(userID)))
	query.Modify(lockRowsForUpdate)
	mem, err := query.Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, ErrMemoryNotFound
		}
		return nil, err
	}
	if why := memoryNotRemovableReason(mem); why != "" {
		_ = tx.Rollback()
		return nil, fmt.Errorf("%w: %s", ErrMemoryNotRemovable, why)
	}

	now := time.Now().UTC()
	if _, err := tx.Memory.UpdateOneID(mem.ID).
		SetStatus(memory.StatusInactive).
		SetRemovedAt(now).
		SetNillableRemovedByPersonalityID(removal.PersonalityID).
		SetNillableRemovedByChatID(removal.ChatID).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	id := mem.ID
	event, err := tx.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(mem.ID).
		SetMergeType(entmerge.MergeTypeRemove).
		SetContent(mem.Content).
		SetDuplicatesFolded(0).
		SetSourceMembers([]entschema.MemoryMergeSourceMember{{
			Content:    mem.Content,
			Scope:      string(mem.Scope),
			Confidence: string(models.MemoryConfidenceFromFloat(mem.Confidence)),
			MemoryID:   &id,
		}}).
		SetSnapshot(&entschema.MemoryMergeUndoSnapshot{
			Version: entschema.MemoryMergeUndoSnapshotVersion,
			Removal: &entschema.MemoryRemovalSnapshot{
				PersonalityID: removal.PersonalityID,
				ChatID:        removal.ChatID,
				Reason:        reason,
			},
		}).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toMemoryMergeEventModel(event), nil
}

// memoryNotRemovableReason is why mem cannot be removed, or "" when it can.
func memoryNotRemovableReason(mem *ent.Memory) string {
	switch {
	case mem.RemovedAt != nil:
		return fmt.Sprintf("it was already removed on %s", mem.RemovedAt.UTC().Format(time.RFC3339))
	case mem.Status != memory.StatusActive:
		return "it is not in active memory (the user archived it, or a merge folded it into another memory)"
	case mem.Scope == memory.ScopeSummary:
		return "it is a conversation's checkpoint summary, which checkpoints rewrite"
	case mem.Starred:
		return "the user starred it; ask them to unstar it, or to delete it in Memories"
	}
	return ""
}

// undoRemoveMemory reverts a remove event: the memory is active again and loses its removal
// marks. A memory restored since (the user set it active, which closes the event) is left as it
// is; one the user has deleted since cannot come back.
func undoRemoveMemory(ctx context.Context, tx *ent.Tx, userID uuid.UUID, event *ent.MemoryMergeEvent) error {
	mem, err := tx.Memory.Query().
		Where(memory.ID(event.SurvivorMemoryID), memory.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrMemoryNotFound
		}
		return err
	}
	if mem.RemovedAt == nil {
		return nil
	}
	return tx.Memory.UpdateOneID(mem.ID).
		SetStatus(memory.StatusActive).
		ClearRemovedAt().
		ClearRemovedByPersonalityID().
		ClearRemovedByChatID().
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
}

// closeRemovalEventsTx marks memoryID's open remove events reverted when the user restores the
// memory by hand (UpdateMemory setting it active), so an old event cannot be undone later on top
// of a newer removal.
func closeRemovalEventsTx(ctx context.Context, tx *ent.Tx, userID, memoryID uuid.UUID, now time.Time) error {
	return tx.MemoryMergeEvent.Update().
		Where(
			entmerge.UserIDEQ(userID),
			entmerge.SurvivorMemoryIDEQ(memoryID),
			entmerge.MergeTypeEQ(entmerge.MergeTypeRemove),
			entmerge.RevertedAtIsNil(),
		).
		SetRevertedAt(now).
		SetUpdatedAt(now).
		Exec(ctx)
}
