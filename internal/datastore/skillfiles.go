package datastore

import (
	"context"
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
)

// Skill bundle files (ADR 0x035 §3): file_attachments rows with origin = skill and a skill_id,
// filed under skills/<slug>/<subdir> with the file's own name, so the bundle's layout is the
// gallery path (skills/pdf-forms/scripts/fill.py). They are read-only to agents and to gallery
// moves; the skill editor and import manage them.

// SkillFileFolder is the gallery folder a bundle file at relPath ("scripts/fill.py") sits in for
// the skill with slug: "skills/<slug>/scripts". The folder is normalized like any gallery folder.
func SkillFileFolder(slug, relPath string) (folder, name string, err error) {
	dir, base := path.Split(strings.Trim(strings.ReplaceAll(relPath, "\\", "/"), "/"))
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

// CreateSkillFile records a bundle file for the skill: a file_attachments row with origin = skill at
// folder/name pointing at s3Key. The skill must belong to the user.
func (d *Datastore) CreateSkillFile(ctx context.Context, userID, ritualID uuid.UUID, folder, name, fileType, s3Key string) (*models.FileAttachment, error) {
	owned, err := d.dbClient.Ritual.Query().
		Where(entritual.ID(ritualID), entritual.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrRitualNotFound
	}
	id := ritualID
	return d.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name:     name,
		FileType: fileType,
		S3Key:    s3Key,
		Folder:   folder,
		Origin:   models.FileOriginSkill,
		SkillID:  &id,
	})
}

// ReplaceSkillFileObject points the skill file id at newKey (a newly uploaded version) and returns
// the key it replaced, so the caller can release the old object.
func (d *Datastore) ReplaceSkillFileObject(ctx context.Context, userID, ritualID, id uuid.UUID, newKey, fileType string) (string, error) {
	row, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.OriginEQ(entfileattachment.OriginSkill),
			entfileattachment.SkillID(ritualID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", ErrFileAttachmentNotFound
		}
		return "", err
	}
	if err := d.dbClient.FileAttachment.UpdateOneID(id).SetS3Key(newKey).SetFileType(fileType).Exec(ctx); err != nil {
		return "", err
	}
	return row.S3Key, nil
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

// ListRitualFileAttachmentObjectRefs returns the bundle file rows a DeleteRitual cascade will
// remove, with the fields that locate their objects.
func (d *Datastore) ListRitualFileAttachmentObjectRefs(ctx context.Context, userID, ritualID uuid.UUID) ([]models.FileAttachment, error) {
	return d.listFileAttachmentObjectRefs(ctx, userID, entfileattachment.SkillID(ritualID))
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
