package datastore

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ErrFolderIntoItself is returned when a folder is moved to somewhere inside itself.
var ErrFolderIntoItself = errors.New("a folder cannot be moved into itself")

// galleryFiles scopes a query to the user's gallery files of one kind (images, every other file,
// or both), with the lightweight reference copies (one per chat reuse) left out, so each stored
// file counts once. Reference copies never carry a folder, so moves and counts act on the
// original rows.
func galleryFiles(userID uuid.UUID, kind models.GalleryKind) []predicate.FileAttachment {
	scope := []predicate.FileAttachment{
		entfileattachment.HasOwnerWith(user.ID(userID)),
		excludeReferenceCopies(),
	}
	if p := galleryKindPredicate(kind); p != nil {
		scope = append(scope, p)
	}
	return scope
}

// galleryKindPredicate narrows to images or to every other file. Any other kind (all, or unset)
// returns nil: no narrowing.
func galleryKindPredicate(kind models.GalleryKind) predicate.FileAttachment {
	switch kind {
	case models.GalleryKindImages:
		return entfileattachment.FileTypeHasPrefix(models.ImageMIMEPrefix)
	case models.GalleryKindFiles:
		return entfileattachment.Not(entfileattachment.FileTypeHasPrefix(models.ImageMIMEPrefix))
	default:
		return nil
	}
}

// ListGalleryFolders returns every folder that holds at least one of the user's gallery files of
// this kind, with the number of them directly in it. The top level is not listed. Ancestors that
// hold nothing of their own are not listed either; a client derives them from the paths.
func (d *Datastore) ListGalleryFolders(ctx context.Context, userID uuid.UUID, kind models.GalleryKind) ([]models.FolderCount, error) {
	var rows []struct {
		Folder string `json:"folder"`
		Count  int    `json:"count"`
	}
	err := d.dbClient.FileAttachment.Query().
		Where(galleryFiles(userID, kind)...).
		Where(entfileattachment.FolderNEQ("")).
		GroupBy(entfileattachment.FieldFolder).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		d.logger.Error("failed to list gallery folders", zap.Error(err))
		return nil, err
	}
	out := make([]models.FolderCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.FolderCount{Path: r.Folder, Count: r.Count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// MoveFileAttachmentsToFolder puts the user's gallery files with these ids (images, documents or
// any other file) in folder (a normalized path; "" is the top level) and returns how many were
// moved. An id may name a file or one of its chat-reuse copies (see CreateFileAttachmentReference);
// a copy moves the original it shares a stored object with, since only originals are in the
// gallery. Ids that are not the user's files are skipped, not an error. Only the label changes;
// the stored object does not move.
func (d *Datastore) MoveFileAttachmentsToFolder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	named, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.IDIn(ids...),
		).
		Select(entfileattachment.FieldS3Key).
		Strings(ctx)
	if err != nil {
		d.logger.Error("failed to resolve files to move", zap.Error(err))
		return 0, err
	}
	match := []predicate.FileAttachment{entfileattachment.IDIn(ids...)}
	if keys := nonEmpty(named); len(keys) > 0 {
		match = append(match, entfileattachment.S3KeyIn(keys...))
	}
	moved, err := d.dbClient.FileAttachment.Update().
		Where(galleryFiles(userID, models.GalleryKindAll)...).
		Where(entfileattachment.Or(match...)).
		SetFolder(folder).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to move files to a folder", zap.Error(err))
		return 0, err
	}
	return moved, nil
}

func nonEmpty(values []string) []string {
	out := values[:0:0]
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// maxFolderMovePasses bounds how many times a folder rename re-reads what is left under the old
// path. One pass normally moves everything; a second only finds files filed there mid-rename.
const maxFolderMovePasses = 5

// MoveGalleryFolder renames folder from to to, carrying everything beneath it along: moving "charts"
// to "archive/charts" turns "charts/oura" into "archive/charts/oura". If to already exists the two
// are merged. Both are normalized paths and from must not be the top level; to may be (it moves the
// contents up a level). It returns how many files changed folder; every kind of file moves.
//
// It is one UPDATE per distinct folder under from (a user's folders are few and nesting is capped),
// all in one transaction. The folders are read inside the transaction and re-read until none are
// left, so a file filed under from while the rename runs is carried along rather than stranded.
func (d *Datastore) MoveGalleryFolder(ctx context.Context, userID uuid.UUID, from, to string) (int, error) {
	if from == "" {
		return 0, fmt.Errorf("%w: choose a folder to move", models.ErrInvalidFolder)
	}
	if from == to {
		return 0, nil
	}
	if models.FolderIsWithin(to, from) {
		return 0, ErrFolderIntoItself
	}

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	total := 0
	for pass := 0; pass < maxFolderMovePasses; pass++ {
		folders, err := tx.FileAttachment.Query().
			Where(galleryFiles(userID, models.GalleryKindAll)...).
			Where(entfileattachment.Or(
				entfileattachment.FolderEQ(from),
				entfileattachment.FolderHasPrefix(from+"/"),
			)).
			GroupBy(entfileattachment.FieldFolder).
			Strings(ctx)
		if err != nil {
			return 0, rollbackWith(tx, d.logger, err)
		}
		if len(folders) == 0 {
			break
		}
		for _, folder := range folders {
			dest := to + folder[len(from):] // from's tail ("" or "/sub/path") rides along
			if to == "" && dest != "" {
				dest = dest[1:] // moving up to the top level: drop the separator
			}
			n, err := tx.FileAttachment.Update().
				Where(galleryFiles(userID, models.GalleryKindAll)...).
				Where(entfileattachment.FolderEQ(folder)).
				SetFolder(dest).
				Save(ctx)
			if err != nil {
				return 0, rollbackWith(tx, d.logger, err)
			}
			total += n
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

// rollbackWith rolls tx back and returns err, logging (not masking) a failed rollback.
func rollbackWith(tx *ent.Tx, logger *zap.Logger, err error) error {
	if rerr := tx.Rollback(); rerr != nil {
		logger.Error("rollback failed", zap.Error(rerr))
	}
	return err
}
