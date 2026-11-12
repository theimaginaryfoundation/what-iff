package datastore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/filecommentset"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Comments on text and code files (ADR 0x045): one set per file, beside it.

// CommentFileID is the file whose comments the user's row id shows: the original of a chat copy,
// so a file and its chat copies share one set (ADR 0x045). A copy made before copies named their
// original is resolved the way the gallery resolves one (the earliest of the owner's rows sharing
// its stored object, context-only rows aside), and the answer is recorded on it.
func (d *Datastore) CommentFileID(ctx context.Context, userID, id uuid.UUID) (uuid.UUID, error) {
	row, err := d.dbClient.FileAttachment.Query().
		Where(entfileattachment.ID(id), entfileattachment.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return uuid.Nil, ErrFileAttachmentNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if row.OriginalID != nil {
		original, err := d.dbClient.FileAttachment.Query().
			Where(entfileattachment.ID(*row.OriginalID), entfileattachment.HasOwnerWith(user.ID(userID))).
			Exist(ctx)
		if err != nil {
			return uuid.Nil, err
		}
		// An original that is gone leaves the copy as the file.
		if original {
			return *row.OriginalID, nil
		}
		return id, nil
	}
	if row.ContextOnly || row.S3Key == "" {
		return id, nil
	}
	first, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.S3Key(row.S3Key),
			entfileattachment.ContextOnly(false),
		).
		Order(ent.Asc(entfileattachment.FieldCreatedAt), ent.Asc(entfileattachment.FieldID)).
		First(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if first.ID == id {
		return id, nil
	}
	if err := d.dbClient.FileAttachment.UpdateOneID(id).SetOriginalID(first.ID).Exec(ctx); err != nil {
		return uuid.Nil, err
	}
	return first.ID, nil
}

// handOverCopies runs before row id is deleted: when other rows are copies of it (linked to it, or
// unlinked rows on the same object, which CommentFileID would link to it), the earliest of them that
// is not context-only becomes the original, the rest link to it, and the file's comment set moves
// to it, so deleting a gallery file leaves its chat copies with their comments (ADR 0x045).
func handOverCopies(ctx context.Context, tx *ent.Tx, userID, id uuid.UUID) error {
	row, err := tx.FileAttachment.Get(ctx, id)
	if err != nil {
		return err
	}
	if row.OriginalID != nil || row.ContextOnly {
		return nil
	}
	copies := entfileattachment.OriginalID(id)
	if row.S3Key != "" {
		copies = entfileattachment.Or(copies, entfileattachment.And(entfileattachment.OriginalIDIsNil(), entfileattachment.S3Key(row.S3Key)))
	}
	mine := entfileattachment.And(
		entfileattachment.IDNEQ(id),
		entfileattachment.HasOwnerWith(user.ID(userID)),
		entfileattachment.ContextOnly(false),
		copies,
	)
	heir, err := tx.FileAttachment.Query().
		Where(mine).
		Order(ent.Asc(entfileattachment.FieldCreatedAt), ent.Asc(entfileattachment.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := tx.FileAttachment.UpdateOneID(heir.ID).ClearOriginalID().Exec(ctx); err != nil {
		return err
	}
	if err := tx.FileAttachment.Update().Where(mine, entfileattachment.IDNEQ(heir.ID)).SetOriginalID(heir.ID).Exec(ctx); err != nil {
		return err
	}
	return tx.FileCommentSet.Update().Where(filecommentset.FileAttachmentID(id)).SetFileAttachmentID(heir.ID).Exec(ctx)
}

// ErrFileCommentsRevision means the comments changed since the caller read them.
var ErrFileCommentsRevision = errors.New("the comments changed since they were read")

// GetFileCommentSet is the user's file's comment set, or nil when it has none.
func (d *Datastore) GetFileCommentSet(ctx context.Context, userID, fileID uuid.UUID) (*models.FileCommentSet, error) {
	row, err := d.dbClient.FileCommentSet.Query().
		Where(filecommentset.FileAttachmentID(fileID), filecommentset.HasFileAttachmentWith(entfileattachment.HasOwnerWith(user.ID(userID)))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set := &models.FileCommentSet{BaselineText: row.BaselineText, BaselineKey: row.BaselineKey, Revision: row.Revision}
	if err := json.Unmarshal([]byte(row.Threads), &set.Threads); err != nil {
		return nil, err
	}
	return set, nil
}

// PutFileCommentSet stores the user's file's comment set, provided it is still at expectRevision
// (0 for a file with none yet), and returns the new revision.
func (d *Datastore) PutFileCommentSet(ctx context.Context, userID, fileID uuid.UUID, set models.FileCommentSet, expectRevision int) (int, error) {
	threads := set.Threads
	if threads == nil {
		threads = []models.FileCommentThread{}
	}
	raw, err := json.Marshal(threads)
	if err != nil {
		return 0, err
	}
	owned, err := d.dbClient.FileAttachment.Query().Where(entfileattachment.ID(fileID), entfileattachment.HasOwnerWith(user.ID(userID))).Exist(ctx)
	if err != nil {
		return 0, err
	}
	if !owned {
		return 0, ErrFileAttachmentNotFound
	}
	if expectRevision == 0 {
		_, err := d.dbClient.FileCommentSet.Create().
			SetFileAttachmentID(fileID).
			SetThreads(string(raw)).
			SetBaselineText(set.BaselineText).
			SetBaselineKey(set.BaselineKey).
			SetRevision(1).
			Save(ctx)
		if ent.IsConstraintError(err) {
			return 0, ErrFileCommentsRevision
		}
		if err != nil {
			return 0, err
		}
		return 1, nil
	}
	n, err := d.dbClient.FileCommentSet.Update().
		Where(filecommentset.FileAttachmentID(fileID), filecommentset.Revision(expectRevision)).
		SetThreads(string(raw)).
		SetBaselineText(set.BaselineText).
		SetBaselineKey(set.BaselineKey).
		AddRevision(1).
		Save(ctx)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrFileCommentsRevision
	}
	return expectRevision + 1, nil
}
