package datastore

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

// Skill bundle files (ADR 0x035 §3): file_attachments rows with origin = skill and a skill_id,
// filed under skills/<slug>/<subdir> with the file's own name, so the bundle's layout is the
// gallery path (skills/pdf-forms/scripts/fill.py). They are read-only to agents and to gallery
// moves; the skill editor and import manage them.

// SkillFileFolder is the gallery folder a bundle file at relPath ("scripts/fill.py") sits in for
// the skill with slug: "skills/<slug>/scripts". The folder is normalized like any gallery folder.
func SkillFileFolder(slug, relPath string) (folder, name string, err error) {
	rel := strings.Trim(strings.ReplaceAll(relPath, "\\", "/"), "/")
	if skills.IsReservedBundlePath(rel) {
		return "", "", fmt.Errorf("%w: %s is the skill's instructions, edited in the skill itself", models.ErrInvalidFolder, skills.SkillFileName)
	}
	dir, base := path.Split(rel)
	// Concatenate rather than path.Join: Join would resolve a ".." out of the skill's folder, while
	// NormalizeFolder refuses it.
	folder, err = models.NormalizeFolder(models.ReservedSkillsFolder + "/" + slug + "/" + dir)
	if err != nil {
		return "", "", err
	}
	return folder, strings.TrimSpace(base), nil
}

// SkillFileRelPath is the inverse of SkillFileFolder: the file's path inside its bundle.
func SkillFileRelPath(att *models.FileAttachment, slug string) string {
	prefix := models.ReservedSkillsFolder + "/" + slug
	rel := strings.TrimPrefix(att.Folder, prefix)
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return att.Name
	}
	return rel + "/" + att.Name
}

// ListSkillFiles returns the skill's bundle files, by folder then name.
func (d *Datastore) ListSkillFiles(ctx context.Context, userID, ritualID uuid.UUID) ([]*models.FileAttachment, error) {
	rows, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.OriginEQ(entfileattachment.OriginSkill),
			entfileattachment.SkillID(ritualID),
		).
		WithOwner().
		Order(ent.Asc(entfileattachment.FieldFolder), ent.Asc(entfileattachment.FieldName)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list a skill's files", zap.String("ritual_id", ritualID.String()), zap.Error(err))
		return nil, err
	}
	out := make([]*models.FileAttachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, toFileAttachmentModel(row))
	}
	return out, nil
}

// CountSkillFiles returns how many bundle files each of the given skills holds (skills with none
// are absent from the map).
func (d *Datastore) CountSkillFiles(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ritualIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SkillID uuid.UUID `json:"skill_id"`
		Count   int       `json:"count"`
	}
	err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.OriginEQ(entfileattachment.OriginSkill),
			entfileattachment.SkillIDIn(ritualIDs...),
		).
		GroupBy(entfileattachment.FieldSkillID).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		d.logger.Error("failed to count skill files", zap.Error(err))
		return nil, err
	}
	for _, r := range rows {
		out[r.SkillID] = r.Count
	}
	return out, nil
}

// FindSkillFile returns the skill's bundle file at folder/name, or nil when there is none.
func (d *Datastore) FindSkillFile(ctx context.Context, userID, ritualID uuid.UUID, folder, name string) (*models.FileAttachment, error) {
	row, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.OriginEQ(entfileattachment.OriginSkill),
			entfileattachment.SkillID(ritualID),
			entfileattachment.FolderEQ(folder),
			entfileattachment.NameEqualFold(strings.TrimSpace(name)),
		).
		WithOwner().
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return toFileAttachmentModel(row), nil
}

// SkillFileObject is a stored object for a bundle file: its key, content type and size.
type SkillFileObject struct {
	Key      string
	FileType string
	Size     int64
}

// SkillFileRecord is one file of a bundle being written: where it sits and its stored object.
type SkillFileRecord struct {
	Folder, Name string
	Object       SkillFileObject
}

// lockSkillForFiles locks the skill's row for the rest of tx, after checking it belongs to the
// user, so writers of the same bundle run one at a time and the file and byte limits hold.
func lockSkillForFiles(ctx context.Context, tx *ent.Tx, userID, ritualID uuid.UUID) error {
	_, err := tx.Ritual.Query().
		Where(entritual.ID(ritualID), entritual.HasOwnerWith(user.ID(userID))).
		Modify(lockRowsForUpdate).
		Only(ctx)
	if ent.IsNotFound(err) {
		return ErrRitualNotFound
	}
	return err
}

// skillBundleUsage is how many files and bytes the skill's bundle holds, leaving out exceptID.
func skillBundleUsage(ctx context.Context, tx *ent.Tx, ritualID, exceptID uuid.UUID) (files int, bytes int64, err error) {
	rows, err := tx.FileAttachment.Query().
		Where(entfileattachment.OriginEQ(entfileattachment.OriginSkill), entfileattachment.SkillID(ritualID)).
		Select(entfileattachment.FieldID, entfileattachment.FieldSizeBytes).
		All(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, row := range rows {
		if row.ID == exceptID {
			continue
		}
		files++
		if row.SizeBytes != nil {
			bytes += *row.SizeBytes
		}
	}
	return files, bytes, nil
}

// checkSkillBundleLimits refuses a bundle of files files and bytes bytes over the limits.
func checkSkillBundleLimits(files int, bytes int64) error {
	if files > skills.MaxBundleFiles {
		return fmt.Errorf("%w: a skill holds at most %d files", skills.ErrInvalid, skills.MaxBundleFiles)
	}
	if bytes > skills.MaxBundleBytes {
		return fmt.Errorf("%w: a skill's files total at most %d MB", skills.ErrInvalid, skills.MaxBundleBytes>>20)
	}
	return nil
}

// errSkillFileExists is what a write without replace returns for a path the bundle already holds.
func errSkillFileExists(folder, name string) error {
	return fmt.Errorf("%w: a file is already at %s/%s", skills.ErrInvalid, folder, name)
}

func withSkillFilesTx[T any](ctx context.Context, d *Datastore, userID, ritualID uuid.UUID, fn func(tx *ent.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockSkillForFiles(ctx, tx, userID, ritualID); err != nil {
		return zero, err
	}
	out, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

// PutSkillFile records obj as the bundle file at folder/name: a new row, or, with replace, the
// row already there pointed at obj. It returns the file and the key it replaced ("" for a new
// file), which the caller releases with storage.ReleaseReplacedObject. The skill's row is locked,
// so the file-count and byte limits hold under concurrent uploads, and the unique index on
// (skill_id, folder, name) backs the path check. Limit and path problems wrap skills.ErrInvalid.
func (d *Datastore) PutSkillFile(ctx context.Context, userID, ritualID uuid.UUID, folder, name string, obj SkillFileObject, replace bool) (*models.FileAttachment, string, error) {
	type result struct {
		file   *models.FileAttachment
		oldKey string
	}
	res, err := withSkillFilesTx(ctx, d, userID, ritualID, func(tx *ent.Tx) (result, error) {
		existing, err := tx.FileAttachment.Query().
			Where(
				entfileattachment.OriginEQ(entfileattachment.OriginSkill),
				entfileattachment.SkillID(ritualID),
				entfileattachment.FolderEQ(folder),
				entfileattachment.NameEqualFold(strings.TrimSpace(name)),
			).
			Only(ctx)
		switch {
		case ent.IsNotFound(err):
			existing = nil
		case err != nil:
			return result{}, err
		}
		if existing != nil && !replace {
			return result{}, errSkillFileExists(folder, name)
		}
		var except uuid.UUID
		if existing != nil {
			except = existing.ID
		}
		files, bytes, err := skillBundleUsage(ctx, tx, ritualID, except)
		if err != nil {
			return result{}, err
		}
		if err := checkSkillBundleLimits(files+1, bytes+obj.Size); err != nil {
			return result{}, err
		}
		if existing != nil {
			row, err := tx.FileAttachment.UpdateOneID(existing.ID).
				SetS3Key(obj.Key).SetFileType(obj.FileType).SetSizeBytes(obj.Size).
				Save(ctx)
			if err != nil {
				return result{}, err
			}
			return result{file: toFileAttachmentModel(row), oldKey: existing.S3Key}, nil
		}
		row, err := createSkillFileRow(ctx, tx, userID, ritualID, SkillFileRecord{Folder: folder, Name: name, Object: obj})
		if ent.IsConstraintError(err) {
			return result{}, errSkillFileExists(folder, name)
		}
		if err != nil {
			return result{}, err
		}
		return result{file: toFileAttachmentModel(row)}, nil
	})
	return res.file, res.oldKey, err
}

func createSkillFileRow(ctx context.Context, tx *ent.Tx, userID, ritualID uuid.UUID, rec SkillFileRecord) (*ent.FileAttachment, error) {
	return tx.FileAttachment.Create().
		SetOwnerID(userID).
		SetName(rec.Name).
		SetFileType(rec.Object.FileType).
		SetS3Key(rec.Object.Key).
		SetFolder(rec.Folder).
		SetOrigin(entfileattachment.OriginSkill).
		SetSkillID(ritualID).
		SetSizeBytes(rec.Object.Size).
		Save(ctx)
}

// CreateSkillFile records a new bundle file for the skill at folder/name pointing at obj; it is
// PutSkillFile without replace.
func (d *Datastore) CreateSkillFile(ctx context.Context, userID, ritualID uuid.UUID, folder, name string, obj SkillFileObject) (*models.FileAttachment, error) {
	file, _, err := d.PutSkillFile(ctx, userID, ritualID, folder, name, obj, false)
	return file, err
}

// ReplaceSkillFileObject points the skill file id at obj (a newly uploaded version) and returns
// the key it replaced, for storage.ReleaseReplacedObject. The byte limit is checked against the
// rest of the bundle.
func (d *Datastore) ReplaceSkillFileObject(ctx context.Context, userID, ritualID, id uuid.UUID, obj SkillFileObject) (string, error) {
	return withSkillFilesTx(ctx, d, userID, ritualID, func(tx *ent.Tx) (string, error) {
		row, err := tx.FileAttachment.Query().
			Where(
				entfileattachment.ID(id),
				entfileattachment.OriginEQ(entfileattachment.OriginSkill),
				entfileattachment.SkillID(ritualID),
			).
			Only(ctx)
		if ent.IsNotFound(err) {
			return "", ErrFileAttachmentNotFound
		}
		if err != nil {
			return "", err
		}
		files, bytes, err := skillBundleUsage(ctx, tx, ritualID, id)
		if err != nil {
			return "", err
		}
		if err := checkSkillBundleLimits(files+1, bytes+obj.Size); err != nil {
			return "", err
		}
		if err := tx.FileAttachment.UpdateOneID(id).
			SetS3Key(obj.Key).SetFileType(obj.FileType).SetSizeBytes(obj.Size).
			Exec(ctx); err != nil {
			return "", err
		}
		return row.S3Key, nil
	})
}

// ReplaceSkillFiles makes files the skill's whole bundle in one transaction: the old rows go and
// the new ones are created, or nothing changes. It returns the removed rows, whose objects the
// caller releases. The objects must already be uploaded; on an error the caller deletes them.
func (d *Datastore) ReplaceSkillFiles(ctx context.Context, userID, ritualID uuid.UUID, files []SkillFileRecord) ([]models.FileAttachment, error) {
	return withSkillFilesTx(ctx, d, userID, ritualID, func(tx *ent.Tx) ([]models.FileAttachment, error) {
		return replaceSkillFilesLocked(ctx, tx, userID, ritualID, files)
	})
}

// replaceSkillFilesInTx is ReplaceSkillFiles inside the caller's transaction, for a write that must
// change the skill and its files together, such as installing a new version of a shared skill. It
// locks the skill's row for the rest of tx.
func replaceSkillFilesInTx(ctx context.Context, tx *ent.Tx, userID, ritualID uuid.UUID, files []SkillFileRecord) ([]models.FileAttachment, error) {
	if err := lockSkillForFiles(ctx, tx, userID, ritualID); err != nil {
		return nil, err
	}
	return replaceSkillFilesLocked(ctx, tx, userID, ritualID, files)
}

// replaceSkillFilesLocked swaps the bundle's rows; the skill's row is already locked in tx.
func replaceSkillFilesLocked(ctx context.Context, tx *ent.Tx, userID, ritualID uuid.UUID, files []SkillFileRecord) ([]models.FileAttachment, error) {
	var total int64
	for _, f := range files {
		total += f.Object.Size
	}
	if err := checkSkillBundleLimits(len(files), total); err != nil {
		return nil, err
	}
	// Paths are matched without regard to case (PutSkillFile), but the unique index is
	// case-sensitive, so twins would both be created and then break every later write there.
	seen := make(map[string]struct{}, len(files))
	for _, f := range files {
		key := strings.ToLower(f.Folder + "/" + f.Name)
		if _, dup := seen[key]; dup {
			return nil, errSkillFileExists(f.Folder, f.Name)
		}
		seen[key] = struct{}{}
	}
	old, err := tx.FileAttachment.Query().
		Where(entfileattachment.OriginEQ(entfileattachment.OriginSkill), entfileattachment.SkillID(ritualID)).
		WithOwner().
		All(ctx)
	if err != nil {
		return nil, err
	}
	removed := make([]models.FileAttachment, 0, len(old))
	ids := make([]uuid.UUID, 0, len(old))
	for _, row := range old {
		removed = append(removed, *toFileAttachmentModel(row))
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		if _, err := tx.FileAttachment.Delete().Where(entfileattachment.IDIn(ids...)).Exec(ctx); err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		if _, err := createSkillFileRow(ctx, tx, userID, ritualID, f); err != nil {
			if ent.IsConstraintError(err) {
				return nil, errSkillFileExists(f.Folder, f.Name)
			}
			return nil, err
		}
	}
	return removed, nil
}

// DeleteSkillFiles removes the skill's bundle file rows (all of them, or only ids when given) and
// returns the rows it removed, so the caller can release their objects.
func (d *Datastore) DeleteSkillFiles(ctx context.Context, userID, ritualID uuid.UUID, ids []uuid.UUID) ([]models.FileAttachment, error) {
	preds := []predicate.FileAttachment{
		entfileattachment.HasOwnerWith(user.ID(userID)),
		entfileattachment.OriginEQ(entfileattachment.OriginSkill),
		entfileattachment.SkillID(ritualID),
	}
	if len(ids) > 0 {
		preds = append(preds, entfileattachment.IDIn(ids...))
	}
	rows, err := d.dbClient.FileAttachment.Query().Where(preds...).WithOwner().All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	rowIDs := make([]uuid.UUID, 0, len(rows))
	out := make([]models.FileAttachment, 0, len(rows))
	for _, row := range rows {
		rowIDs = append(rowIDs, row.ID)
		out = append(out, *toFileAttachmentModel(row))
	}
	if _, err := d.dbClient.FileAttachment.Delete().Where(entfileattachment.IDIn(rowIDs...)).Exec(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// moveSkillFilesToSlug refiles a skill's bundle files from skills/<oldSlug>/… to skills/<newSlug>/…
// after the skill's slug changed. Run inside the ritual update's transaction.
func moveSkillFilesToSlug(ctx context.Context, tx *ent.Tx, ritualID uuid.UUID, oldSlug, newSlug string) error {
	if oldSlug == newSlug {
		return nil
	}
	rows, err := tx.FileAttachment.Query().
		Where(entfileattachment.OriginEQ(entfileattachment.OriginSkill), entfileattachment.SkillID(ritualID)).
		Select(entfileattachment.FieldID, entfileattachment.FieldFolder).
		All(ctx)
	if err != nil {
		return err
	}
	oldPrefix := models.ReservedSkillsFolder + "/" + oldSlug
	newPrefix := models.ReservedSkillsFolder + "/" + newSlug
	for _, row := range rows {
		folder := row.Folder
		switch {
		case folder == oldPrefix:
			folder = newPrefix
		case strings.HasPrefix(folder, oldPrefix+"/"):
			folder = newPrefix + folder[len(oldPrefix):]
		default:
			continue
		}
		if err := tx.FileAttachment.UpdateOneID(row.ID).SetFolder(folder).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
