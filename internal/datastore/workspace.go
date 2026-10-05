package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/workspacefile"
	"github.com/theimaginaryfoundation/what-iff/ent/workspacefilerevision"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ErrWorkspaceFileNotFound is returned when no workspace file exists at a path for this owner.
var ErrWorkspaceFileNotFound = errors.New("workspace file not found")

// WorkspaceConflictError reports a write refused because the file changed since the writer last
// read it (or was created concurrently). Current is the file's revision now.
type WorkspaceConflictError struct {
	Current int
}

func (e *WorkspaceConflictError) Error() string {
	return fmt.Sprintf("workspace file changed: current revision is %d", e.Current)
}

func toWorkspaceFileModel(e *ent.WorkspaceFile, userID uuid.UUID) *models.WorkspaceFile {
	if e == nil {
		return nil
	}
	return &models.WorkspaceFile{
		ID:               e.ID,
		UserID:           userID,
		Root:             string(e.Root),
		RootRef:          e.RootRef,
		Path:             e.Path,
		ContentType:      e.ContentType,
		CurrentRevision:  e.CurrentRevision,
		Size:             e.Size,
		SHA256:           e.Sha256,
		State:            string(e.State),
		AuthorClass:      string(e.AuthorClass),
		StorageKey:       e.StorageKey,
		ContentUpdatedAt: e.ContentUpdatedAt,
		ReadCount:        e.ReadCount,
		LastReadAt:       e.LastReadAt,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}
}

// GetWorkspaceFile returns the file at (root, rootRef, path) for this owner, in any state.
// Callers decide what a deleted file means for them.
func (d *Datastore) GetWorkspaceFile(ctx context.Context, userID uuid.UUID, root string, rootRef uuid.UUID, path string) (*models.WorkspaceFile, error) {
	row, err := d.dbClient.WorkspaceFile.Query().
		Where(
			workspacefile.HasOwnerWith(user.ID(userID)),
			workspacefile.RootEQ(workspacefile.Root(root)),
			workspacefile.RootRef(rootRef),
			workspacefile.Path(path),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrWorkspaceFileNotFound
		}
		return nil, err
	}
	return toWorkspaceFileModel(row, userID), nil
}

// ListWorkspaceFiles returns live files in one root, optionally under a path prefix, ordered by
// path. limit is clamped to [1, 500].
func (d *Datastore) ListWorkspaceFiles(ctx context.Context, userID uuid.UUID, root string, rootRef uuid.UUID, prefix string, limit int) ([]*models.WorkspaceFile, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q := d.dbClient.WorkspaceFile.Query().
		Where(
			workspacefile.HasOwnerWith(user.ID(userID)),
			workspacefile.RootEQ(workspacefile.Root(root)),
			workspacefile.RootRef(rootRef),
			workspacefile.StateEQ(workspacefile.StateLive),
		)
	if prefix = strings.TrimSpace(prefix); prefix != "" {
		q = q.Where(workspacefile.PathHasPrefix(prefix))
	}
	rows, err := q.Order(ent.Asc(workspacefile.FieldPath)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*models.WorkspaceFile, 0, len(rows))
	for _, r := range rows {
		out = append(out, toWorkspaceFileModel(r, userID))
	}
	return out, nil
}

// GetWorkspaceUsage sums the owner's live workspace files, for quota checks.
func (d *Datastore) GetWorkspaceUsage(ctx context.Context, userID uuid.UUID) (models.WorkspaceUsage, error) {
	// Sum is a pointer because SUM over no rows is NULL.
	var rows []struct {
		Count int    `json:"count"`
		Sum   *int64 `json:"sum"`
	}
	err := d.dbClient.WorkspaceFile.Query().
		Where(
			workspacefile.HasOwnerWith(user.ID(userID)),
			workspacefile.StateEQ(workspacefile.StateLive),
		).
		Aggregate(ent.Count(), ent.As(ent.Sum(workspacefile.FieldSize), "sum")).
		Scan(ctx, &rows)
	if err != nil {
		return models.WorkspaceUsage{}, err
	}
	if len(rows) == 0 {
		return models.WorkspaceUsage{}, nil
	}
	usage := models.WorkspaceUsage{Files: rows[0].Count}
	if rows[0].Sum != nil {
		usage.Bytes = *rows[0].Sum
	}
	return usage, nil
}

// CommitWorkspaceRevision records one write as a new revision, atomically:
//
//   - A file that does not exist yet is created at revision 1. A create where a live file already
//     exists (or a concurrent create that loses on the unique index) gets a conflict; a create over
//     a deleted file revives it with the next revision.
//   - Otherwise the file row is advanced with a conditional update on its current revision, so two
//     writers can never both commit on top of the same revision. When in.BaseRevision is set and
//     differs from the current revision the write is refused without trying.
//
// The content object must already be uploaded; on any error the caller should delete it.
func (d *Datastore) CommitWorkspaceRevision(ctx context.Context, userID uuid.UUID, in models.WorkspaceRevisionInput) (*models.WorkspaceFile, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func(e error) (*models.WorkspaceFile, error) {
		_ = tx.Rollback()
		return nil, e
	}

	existing, err := tx.WorkspaceFile.Query().
		Where(
			workspacefile.HasOwnerWith(user.ID(userID)),
			workspacefile.RootEQ(workspacefile.Root(in.Root)),
			workspacefile.RootRef(in.RootRef),
			workspacefile.Path(in.Path),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return rollback(err)
	}

	state := workspacefile.StateLive
	if in.Op == models.WorkspaceOpDelete {
		state = workspacefile.StateDeleted
	}

	var fileID uuid.UUID
	var newRevision int
	if existing == nil {
		if in.BaseRevision > 0 {
			return rollback(&WorkspaceConflictError{Current: 0})
		}
		if in.Op == models.WorkspaceOpDelete {
			return rollback(ErrWorkspaceFileNotFound)
		}
		created, err := tx.WorkspaceFile.Create().
			SetOwnerID(userID).
			SetRoot(workspacefile.Root(in.Root)).
			SetRootRef(in.RootRef).
			SetPath(in.Path).
			SetContentType(in.ContentType).
			SetCurrentRevision(1).
			SetSize(in.Size).
			SetSha256(in.SHA256).
			SetStorageKey(in.StorageKey).
			SetState(state).
			SetAuthorClass(workspacefile.AuthorClass(in.AuthorClass)).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return rollback(&WorkspaceConflictError{Current: 1})
			}
			return rollback(err)
		}
		fileID, newRevision = created.ID, 1
	} else {
		// A create only succeeds where there is no live file. Without this, two writers that both
		// saw "no such file" would each commit, and the second would silently replace the first.
		if in.Op == models.WorkspaceOpCreate && existing.State == workspacefile.StateLive {
			return rollback(&WorkspaceConflictError{Current: existing.CurrentRevision})
		}
		if in.BaseRevision != models.WorkspaceAnyRevision && in.BaseRevision != existing.CurrentRevision {
			return rollback(&WorkspaceConflictError{Current: existing.CurrentRevision})
		}
		newRevision = existing.CurrentRevision + 1
		n, err := tx.WorkspaceFile.Update().
			Where(
				workspacefile.ID(existing.ID),
				workspacefile.CurrentRevision(existing.CurrentRevision),
			).
			SetCurrentRevision(newRevision).
			SetContentType(in.ContentType).
			SetSize(in.Size).
			SetSha256(in.SHA256).
			SetStorageKey(in.StorageKey).
			SetState(state).
			SetAuthorClass(workspacefile.AuthorClass(in.AuthorClass)).
			SetContentUpdatedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil {
			return rollback(err)
		}
		if n == 0 {
			current := existing.CurrentRevision
			if latest, qerr := tx.WorkspaceFile.Get(ctx, existing.ID); qerr == nil {
				current = latest.CurrentRevision
			}
			return rollback(&WorkspaceConflictError{Current: current})
		}
		fileID = existing.ID
	}

	base := in.BaseRevision
	if base == models.WorkspaceAnyRevision {
		base = newRevision - 1
	}
	if _, err := tx.WorkspaceFileRevision.Create().
		SetFileID(fileID).
		SetRevision(newRevision).
		SetOp(workspacefilerevision.Op(in.Op)).
		SetBaseRevision(base).
		SetStorageKey(in.StorageKey).
		SetSize(in.Size).
		SetSha256(in.SHA256).
		SetAuthorClass(workspacefilerevision.AuthorClass(in.AuthorClass)).
		SetNillableChatID(in.ChatID).
		Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return rollback(&WorkspaceConflictError{Current: newRevision})
		}
		return rollback(err)
	}

	row, err := tx.WorkspaceFile.Get(ctx, fileID)
	if err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toWorkspaceFileModel(row, userID), nil
}

// TouchWorkspaceFileRead records that a file was read (last_read_at, read_count), which upkeep
// uses to find cold files. Best-effort: callers ignore the error.
func (d *Datastore) TouchWorkspaceFileRead(ctx context.Context, userID, fileID uuid.UUID) error {
	return d.dbClient.WorkspaceFile.Update().
		Where(
			workspacefile.ID(fileID),
			workspacefile.HasOwnerWith(user.ID(userID)),
		).
		SetLastReadAt(time.Now().UTC()).
		AddReadCount(1).
		Exec(ctx)
}

// PurgeWorkspaceRoot deletes every file in one root, with all revisions, and returns the storage
// keys of the deleted revisions so the caller can remove the objects. Used when the conversation
// (chat root) or personality (agent root) that owns the root is deleted.
func (d *Datastore) PurgeWorkspaceRoot(ctx context.Context, userID uuid.UUID, root string, rootRef uuid.UUID) ([]string, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	files, err := tx.WorkspaceFile.Query().
		Where(
			workspacefile.HasOwnerWith(user.ID(userID)),
			workspacefile.RootEQ(workspacefile.Root(root)),
			workspacefile.RootRef(rootRef),
		).
		IDs(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if len(files) == 0 {
		_ = tx.Rollback()
		return nil, nil
	}
	revisions, err := tx.WorkspaceFileRevision.Query().
		Where(workspacefilerevision.HasFileWith(workspacefile.IDIn(files...))).
		All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	var keys []string
	for _, r := range revisions {
		if r.StorageKey != "" {
			keys = append(keys, r.StorageKey)
		}
	}
	if _, err := tx.WorkspaceFileRevision.Delete().
		Where(workspacefilerevision.HasFileWith(workspacefile.IDIn(files...))).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := tx.WorkspaceFile.Delete().Where(workspacefile.IDIn(files...)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return keys, nil
}
