package datastore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/filechunk"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

// Content writes and renames for the file workspace (ADR 0x043 §3). A file is its object graph:
// the gallery row and the chat-reuse copies that share its stored object are one file, so a save
// or a rename moves them together, while context-only rows (what a reply saw) keep the old object.

var (
	// ErrFileAttachmentPathTaken means another of the user's gallery files already has that
	// folder and name.
	ErrFileAttachmentPathTaken = errors.New("a file with that name is already in the folder")
	// ErrSkillFileImmutable means the file belongs to a skill bundle, which the gallery never
	// renames or overwrites: the skill editor manages it (ADR 0x035 §3).
	ErrSkillFileImmutable = errors.New("skill bundle files are managed in the skill editor")
)

// SwapFileObjectOptions tunes SwapFileObject.
type SwapFileObjectOptions struct {
	// AgentOnly swaps only an agent-authored row (the agent file tools' rule).
	AgentOnly bool
	// ExpectVersion, when set, refuses the swap unless the row's content_version equals it: the
	// If-Match check of a save from the file workspace.
	ExpectVersion *int
	// SizeBytes and SHA256 describe the new object and are stored on every row moved.
	SizeBytes int64
	SHA256    string
	// MoveSharedRows also repoints the owner's other rows on oldKey that are not context-only,
	// so a gallery file and its chat copies stay one file.
	MoveSharedRows bool
}

// SwapFileObject points the user's file id at newKey, a newly uploaded version, provided it still
// points at oldKey (and, with ExpectVersion, is still at that version). It bumps content_version,
// stores the new size and hash and records the change time. It reports whether the swap happened:
// false means the row changed under the caller (or is gone, or opts exclude it), and the caller
// should drop newKey and re-read.
func (d *Datastore) SwapFileObject(ctx context.Context, userID, id uuid.UUID, oldKey, newKey string, opts SwapFileObjectOptions) (bool, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return false, err
	}
	rollback := func(err error) (bool, error) {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return false, err
	}
	now := time.Now()
	primary := tx.FileAttachment.Update().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.S3Key(oldKey),
		)
	if opts.AgentOnly {
		primary.Where(entfileattachment.OriginEQ(entfileattachment.OriginAgent))
	}
	if opts.ExpectVersion != nil {
		primary.Where(entfileattachment.ContentVersion(*opts.ExpectVersion))
	}
	n, err := applySwap(primary, newKey, now, opts).Save(ctx)
	if err != nil {
		d.logger.Error("failed to swap a file's object", zap.Error(err))
		return rollback(err)
	}
	if n != 1 {
		return rollback(nil)
	}
	if opts.MoveSharedRows && strings.TrimSpace(oldKey) != "" {
		shared := tx.FileAttachment.Update().
			Where(
				entfileattachment.IDNEQ(id),
				entfileattachment.HasOwnerWith(user.ID(userID)),
				entfileattachment.S3Key(oldKey),
				entfileattachment.ContextOnly(false),
			)
		if _, err := applySwap(shared, newKey, now, opts).Save(ctx); err != nil {
			d.logger.Error("failed to move a file's shared rows", zap.Error(err))
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func applySwap(u *ent.FileAttachmentUpdate, newKey string, now time.Time, opts SwapFileObjectOptions) *ent.FileAttachmentUpdate {
	u.SetS3Key(newKey).SetUpdatedAt(now).AddContentVersion(1)
	if opts.SizeBytes > 0 || opts.SHA256 != "" {
		u.SetSizeBytes(opts.SizeBytes)
	}
	if opts.SHA256 != "" {
		u.SetContentSha256(opts.SHA256)
	}
	return u
}

// ClearFileChunks removes the file's search chunks and resets its chunk status, so the chunking
// pipeline (whose CreateFileChunks is otherwise idempotent) indexes the new content after a save.
func (d *Datastore) ClearFileChunks(ctx context.Context, fileAttachmentID uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}
	if _, err := tx.FileChunk.Delete().
		Where(filechunk.HasFileAttachmentWith(entfileattachment.ID(fileAttachmentID))).
		Exec(ctx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}
	if _, err := tx.FileAttachment.Update().
		Where(entfileattachment.ID(fileAttachmentID)).
		ClearChunkStatus().
		Save(ctx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}
	return tx.Commit()
}

// UpdateFileAttachmentName renames one of the user's gallery files. The name is normalized
// (models.NormalizeFileName, so a name without an extension keeps the current one) and must be
// free in the file's folder (ErrFileAttachmentPathTaken); skill bundle files are refused
// (ErrSkillFileImmutable). When the extension changes to one the upload allowlist knows, and the
// file stays an image or stays a non-image, file_type follows it. The chat copies that share the
// stored object are renamed with it. The object key never changes.
func (d *Datastore) UpdateFileAttachmentName(ctx context.Context, userID, id uuid.UUID, name string) (*models.FileAttachment, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	rollback := func(err error) (*models.FileAttachment, error) {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	row, err := tx.FileAttachment.Query().
		Where(entfileattachment.ID(id), entfileattachment.HasOwnerWith(user.ID(userID)), entfileattachment.ContextOnly(false)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return rollback(ErrFileAttachmentNotFound)
		}
		return rollback(err)
	}
	if row.Origin == entfileattachment.OriginSkill {
		return rollback(ErrSkillFileImmutable)
	}
	norm, err := models.NormalizeFileName(name, row.Name)
	if err != nil {
		return rollback(err)
	}
	if norm != row.Name {
		taken, err := tx.FileAttachment.Query().
			Where(galleryFiles(userID, models.GalleryKindAll)...).
			Where(
				entfileattachment.IDNEQ(id),
				entfileattachment.FolderEQ(row.Folder),
				entfileattachment.NameEqualFold(norm),
			).
			Exist(ctx)
		if err != nil {
			return rollback(err)
		}
		if taken {
			return rollback(ErrFileAttachmentPathTaken)
		}
	}
	fileType := renamedFileType(row.Name, norm, row.FileType)
	if norm != row.Name || fileType != row.FileType {
		if _, err := tx.FileAttachment.Update().
			Where(entfileattachment.ID(id)).
			SetName(norm).SetFileType(fileType).
			Save(ctx); err != nil {
			return rollback(err)
		}
		if k := strings.TrimSpace(row.S3Key); k != "" {
			if _, err := tx.FileAttachment.Update().
				Where(
					entfileattachment.IDNEQ(id),
					entfileattachment.HasOwnerWith(user.ID(userID)),
					entfileattachment.S3Key(k),
					entfileattachment.ContextOnly(false),
				).
				SetName(norm).SetFileType(fileType).
				Save(ctx); err != nil {
				return rollback(err)
			}
		}
	}

	result, err := tx.FileAttachment.Query().
		Where(entfileattachment.ID(id)).
		WithOwner().
		WithChatMessage(func(q *ent.ChatMessageQuery) { q.WithChat() }).
		Only(ctx)
	if err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toFileAttachmentModel(result), nil
}

// renamedFileType returns the MIME type a file has after being renamed from oldName to newName:
// the allowlist's type for the new extension when it changed and the file stays an image or stays
// a non-image, else current.
func renamedFileType(oldName, newName, current string) string {
	oldExt, newExt := strings.ToLower(models.FileExtension(oldName)), strings.ToLower(models.FileExtension(newName))
	if newExt == "" || newExt == oldExt {
		return current
	}
	info, err := utils.GetFileType(newName)
	if err != nil {
		return current
	}
	wasImage := strings.HasPrefix(current, models.ImageMIMEPrefix)
	isImage := strings.HasPrefix(info.ContentType, models.ImageMIMEPrefix)
	if wasImage != isImage {
		return current
	}
	return info.ContentType
}
