package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

func TestSkillFileFolderAndRelPath(t *testing.T) {
	folder, name, err := SkillFileFolder("pdf-forms", "scripts/fill.py")
	require.NoError(t, err)
	require.Equal(t, "skills/pdf-forms/scripts", folder)
	require.Equal(t, "fill.py", name)

	folder, name, err = SkillFileFolder("pdf-forms", "README.md")
	require.NoError(t, err)
	require.Equal(t, "skills/pdf-forms", folder)
	require.Equal(t, "README.md", name)

	_, _, err = SkillFileFolder("pdf-forms", "../escape.md")
	require.Error(t, err)
	_, _, err = SkillFileFolder("pdf-forms", "skill.md")
	require.Error(t, err, "SKILL.md is rendered from the skill, never one of its files")
	_, _, err = SkillFileFolder("pdf-forms", "references/SKILL.md")
	require.NoError(t, err, "only the bundle root's SKILL.md is reserved")

	att := &models.FileAttachment{Folder: "skills/pdf-forms/references/deep", Name: "FORMS.md"}
	require.Equal(t, "references/deep/FORMS.md", SkillFileRelPath(att, "pdf-forms"))
	require.Equal(t, "README.md", SkillFileRelPath(&models.FileAttachment{Folder: "skills/pdf-forms", Name: "README.md"}, "pdf-forms"))
}

func TestSkillFiles_CreateListCountReplaceDeleteAndGalleryExclusion(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)
	other := createMoodTestUser(t, ds)

	skill, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "PDF Forms", Description: "d", Content: "c"})
	require.NoError(t, err)
	folder, name, err := SkillFileFolder(skill.Slug, "scripts/fill.py")
	require.NoError(t, err)
	script, err := ds.CreateSkillFile(ctx, userID, skill.ID, folder, name, SkillFileObject{Key: "users/u/skills/s/v1_fill.py", FileType: "text/x-python", Size: 10})
	require.NoError(t, err)
	require.Equal(t, models.FileOriginSkill, script.Origin)
	require.Equal(t, &skill.ID, script.SkillID)
	folder, name, err = SkillFileFolder(skill.Slug, "references/FORMS.md")
	require.NoError(t, err)
	_, err = ds.CreateSkillFile(ctx, userID, skill.ID, folder, name, SkillFileObject{Key: "users/u/skills/s/v1_forms.md", FileType: "text/markdown", Size: 20})
	require.NoError(t, err)

	// Another user's skill id is refused.
	_, err = ds.CreateSkillFile(ctx, other, skill.ID, folder, "x.md", SkillFileObject{Key: "k", FileType: "text/markdown"})
	require.ErrorIs(t, err, ErrRitualNotFound)

	files, err := ds.ListSkillFiles(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "skills/pdf-forms/references", files[0].Folder, "ordered by folder then name")

	counts, err := ds.CountSkillFiles(ctx, userID, []uuid.UUID{skill.ID, uuid.New()})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]int{skill.ID: 2}, counts)

	found, err := ds.FindSkillFile(ctx, userID, skill.ID, "skills/pdf-forms/scripts", "FILL.PY")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, script.ID, found.ID)
	missing, err := ds.FindSkillFile(ctx, userID, skill.ID, "skills/pdf-forms/scripts", "nope.py")
	require.NoError(t, err)
	require.Nil(t, missing)

	// Agents find skill files by path (read-only), but the gallery leaves them out unless it looks
	// under the skills folder, and never moves or renames them.
	atPath, err := ds.FindFileAttachmentsAtPath(ctx, userID, "skills/pdf-forms/scripts", "fill.py")
	require.NoError(t, err)
	require.Len(t, atPath, 1)
	gallery, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{})
	require.NoError(t, err)
	require.Equal(t, 0, gallery.TotalCount)
	bySkill, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{Origin: models.FileOriginSkill})
	require.NoError(t, err)
	require.Equal(t, 2, bySkill.TotalCount)
	prefix := "skills/pdf-forms"
	underFolder, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{FolderPrefix: &prefix})
	require.NoError(t, err)
	require.Equal(t, 2, underFolder.TotalCount)
	folders, err := ds.ListGalleryFolders(ctx, userID, models.GalleryKindAll)
	require.NoError(t, err)
	require.Empty(t, folders)
	moved, err := ds.MoveFileAttachmentsToFolder(ctx, userID, []uuid.UUID{script.ID}, "elsewhere")
	require.NoError(t, err)
	require.Zero(t, moved)
	_, err = ds.RenameGalleryFile(ctx, userID, script.ID, "renamed.py")
	require.ErrorIs(t, err, ErrFileAttachmentNotFound)

	// Replacing swaps the object; a slug change refiles the folders.
	oldKey, err := ds.ReplaceSkillFileObject(ctx, userID, skill.ID, script.ID, SkillFileObject{Key: "users/u/skills/s/v2_fill.py", FileType: "text/x-python", Size: 30})
	require.NoError(t, err)
	require.Equal(t, "users/u/skills/s/v1_fill.py", oldKey)
	_, err = ds.UpdateRitual(ctx, userID, models.Ritual{ID: skill.ID, Slug: "forms", Name: "Forms", Description: "d", Content: "c"})
	require.NoError(t, err)
	files, err = ds.ListSkillFiles(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Equal(t, "skills/forms/references", files[0].Folder)
	require.Equal(t, "skills/forms/scripts", files[1].Folder)
	require.Equal(t, "users/u/skills/s/v2_fill.py", files[1].S3Key)

	removed, err := ds.DeleteSkillFiles(ctx, userID, skill.ID, []uuid.UUID{script.ID})
	require.NoError(t, err)
	require.Len(t, removed, 1)
	removed, err = ds.DeleteSkillFiles(ctx, userID, skill.ID, nil)
	require.NoError(t, err)
	require.Len(t, removed, 1)
	files, err = ds.ListSkillFiles(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Empty(t, files)

	// Deleting the skill cascades to its file rows.
	folder, name, _ = SkillFileFolder("forms", "assets/logo.png")
	_, err = ds.CreateSkillFile(ctx, userID, skill.ID, folder, name, SkillFileObject{Key: "users/u/skills/s/logo.png", FileType: "image/png", Size: 5})
	require.NoError(t, err)
	deleted, err := ds.DeleteRitualWithFiles(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Len(t, deleted, 1, "the delete returns the bundle rows so their objects can be released")
	require.Equal(t, "users/u/skills/s/logo.png", deleted[0].S3Key)
	leftover, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{Origin: models.FileOriginSkill})
	require.NoError(t, err)
	require.Equal(t, 0, leftover.TotalCount)
}

func TestSkillFiles_LimitsUniquePathsAndAtomicReplace(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)
	skill, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "Big", Description: "d", Content: "c"})
	require.NoError(t, err)
	obj := func(key string, size int64) SkillFileObject {
		return SkillFileObject{Key: key, FileType: "application/octet-stream", Size: size}
	}

	// The byte limit counts what the bundle already holds.
	_, _, err = ds.PutSkillFile(ctx, userID, skill.ID, "skills/big", "a.bin", obj("a1", skills.MaxBundleBytes-10), false)
	require.NoError(t, err)
	_, _, err = ds.PutSkillFile(ctx, userID, skill.ID, "skills/big", "b.bin", obj("b1", 11), false)
	require.ErrorIs(t, err, skills.ErrInvalid)
	require.ErrorContains(t, err, "30 MB")
	// Replacing a file counts its new size instead of its old one.
	file, oldKey, err := ds.PutSkillFile(ctx, userID, skill.ID, "skills/big", "A.BIN", obj("a2", skills.MaxBundleBytes), true)
	require.NoError(t, err)
	require.Equal(t, "a1", oldKey)
	require.EqualValues(t, skills.MaxBundleBytes, *file.SizeBytes)

	// One file per path: without replace a second write is refused, and the index backs it.
	_, _, err = ds.PutSkillFile(ctx, userID, skill.ID, "skills/big", "a.bin", obj("a3", 1), false)
	require.ErrorIs(t, err, skills.ErrInvalid)
	_, err = ds.dbClient.FileAttachment.Create().SetOwnerID(userID).SetName("a.bin").SetFileType("x").
		SetFolder("skills/big").SetOrigin("skill").SetSkillID(skill.ID).Save(ctx)
	require.Error(t, err, "the unique index refuses a duplicate path that skipped the check")

	// ReplaceSkillFiles swaps the whole bundle, or nothing when the new set is over a limit.
	_, err = ds.ReplaceSkillFiles(ctx, userID, skill.ID, []SkillFileRecord{
		{Folder: "skills/big", Name: "x.bin", Object: obj("x1", skills.MaxBundleBytes)},
		{Folder: "skills/big", Name: "y.bin", Object: obj("y1", 1)},
	})
	require.ErrorIs(t, err, skills.ErrInvalid)
	files, err := ds.ListSkillFiles(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "a2", files[0].S3Key, "a refused replace leaves the bundle as it was")

	// Paths that differ only in case are one path.
	_, err = ds.ReplaceSkillFiles(ctx, userID, skill.ID, []SkillFileRecord{
		{Folder: "skills/big/scripts", Name: "Run.py", Object: obj("r0", 3)},
		{Folder: "skills/big/scripts", Name: "run.py", Object: obj("r0b", 3)},
	})
	require.ErrorIs(t, err, skills.ErrInvalid)

	removed, err := ds.ReplaceSkillFiles(ctx, userID, skill.ID, []SkillFileRecord{
		{Folder: "skills/big/scripts", Name: "run.py", Object: obj("r1", 3)},
	})
	require.NoError(t, err)
	require.Len(t, removed, 1)
	require.Equal(t, "a2", removed[0].S3Key)
	removed, err = ds.ReplaceSkillFiles(ctx, userID, skill.ID, nil)
	require.NoError(t, err)
	require.Len(t, removed, 1, "an empty bundle leaves the skill with no files")
}

func TestReservedSkillsFolder_RefusedForEveryOtherWriter(t *testing.T) {
	ds, cleanup := newTestDatastore(t, createGeneratedTestSchema)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)

	_, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "img.png", FileType: "image/png", Folder: "Skills/pdf-forms"})
	require.ErrorIs(t, err, models.ErrReservedFolder)

	file, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "img.png", FileType: "image/png", Folder: "charts"})
	require.NoError(t, err)
	_, err = ds.MoveFileAttachmentsToFolder(ctx, userID, []uuid.UUID{file.ID}, "skills/pdf-forms")
	require.ErrorIs(t, err, models.ErrReservedFolder)
	_, err = ds.MoveGalleryFolder(ctx, userID, "charts", "skills")
	require.ErrorIs(t, err, models.ErrReservedFolder)
}
