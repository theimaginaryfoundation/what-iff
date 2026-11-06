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

// ArchiveMemory archives one memory on an agent's behalf (archive_memory, ADR 0x036). It works
// like a merge retiring a memory: the row, its content and its embedding stay, the status becomes
// inactive (so search, find_context and recall stop returning it), and nothing is deleted. The
// row also gets archived_at and the archiving personality and conversation, and an "archive"
// lifecycle event records who archived it and why. The user restores it from the Archived view
// (UpdateMemory) or by undoing the event (UndoMemoryMergeEvent).
//
// Only an active Context memory the user has not starred can be archived. Anything else is refused
// with an ErrMemoryNotArchivable that says why, in words meant for the model. Which memories the
// asking conversation may see (sandbox, personality pins) is the caller's check.
func (d *Datastore) ArchiveMemory(ctx context.Context, userID, memoryID uuid.UUID, archival models.MemoryArchival) (*models.MemoryMergeEvent, error) {
	reason := strings.TrimSpace(archival.Reason)
	if utf8.RuneCountInString(reason) > models.MaxMemoryArchiveReasonRunes {
		return nil, fmt.Errorf("%w: the reason is longer than %d characters", ErrMemoryNotArchivable, models.MaxMemoryArchiveReasonRunes)
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
	if why := memoryNotArchivableReason(mem); why != "" {
		_ = tx.Rollback()
		return nil, fmt.Errorf("%w: %s", ErrMemoryNotArchivable, why)
	}

	now := time.Now().UTC()
	if _, err := tx.Memory.UpdateOneID(mem.ID).
		SetStatus(memory.StatusInactive).
		SetArchivedAt(now).
		SetNillableArchivedByPersonalityID(archival.PersonalityID).
		SetNillableArchivedByChatID(archival.ChatID).
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	id := mem.ID
	event, err := tx.MemoryMergeEvent.Create().
		SetUserID(userID).
		SetSurvivorMemoryID(mem.ID).
		SetMergeType(entmerge.MergeTypeArchive).
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
			Archival: &entschema.MemoryArchivalSnapshot{
				PersonalityID: archival.PersonalityID,
				ChatID:        archival.ChatID,
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

// memoryNotArchivableReason is why mem cannot be archived by an agent, or "" when it can.
func memoryNotArchivableReason(mem *ent.Memory) string {
	switch {
	case mem.ArchivedAt != nil:
		return fmt.Sprintf("an agent already archived it on %s", mem.ArchivedAt.UTC().Format(time.RFC3339))
	case mem.Status != memory.StatusActive:
		return "it is already out of active memory (the user archived it, or a merge folded it into another memory)"
	case mem.Scope == memory.ScopeSummary:
		return "it is a conversation's checkpoint summary, which checkpoints rewrite"
	case mem.Starred:
		return "the user starred it; ask them to unstar or archive it in Memories"
	}
	return ""
}

// undoArchiveMemory reverts an archive event: the memory is active again and loses its archive
// marks. A memory restored since (the user unarchived it, which closes the event) is left as it is;
// one the user has deleted since cannot come back.
func undoArchiveMemory(ctx context.Context, tx *ent.Tx, userID uuid.UUID, event *ent.MemoryMergeEvent) error {
	mem, err := tx.Memory.Query().
		Where(memory.ID(event.SurvivorMemoryID), memory.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrMemoryNotFound
		}
		return err
	}
	if mem.ArchivedAt == nil {
		return nil
	}
	return tx.Memory.UpdateOneID(mem.ID).
		SetStatus(memory.StatusActive).
		ClearArchivedAt().
		ClearArchivedByPersonalityID().
		ClearArchivedByChatID().
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
}

// closeArchiveEventsTx marks memoryID's open archive events reverted when the user restores the
// memory from the Archived view (UpdateMemory setting it active), so an old event cannot be undone
// later on top of a newer archive.
func closeArchiveEventsTx(ctx context.Context, tx *ent.Tx, userID, memoryID uuid.UUID, now time.Time) error {
	return tx.MemoryMergeEvent.Update().
		Where(
			entmerge.UserIDEQ(userID),
			entmerge.SurvivorMemoryIDEQ(memoryID),
			entmerge.MergeTypeEQ(entmerge.MergeTypeArchive),
			entmerge.RevertedAtIsNil(),
		).
		SetRevertedAt(now).
		SetUpdatedAt(now).
		Exec(ctx)
}
