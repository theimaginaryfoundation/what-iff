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
	set := &models.FileCommentSet{BaselineText: row.BaselineText, BaselineVersion: row.BaselineVersion, Revision: row.Revision}
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
			SetBaselineVersion(set.BaselineVersion).
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
		SetBaselineVersion(set.BaselineVersion).
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
