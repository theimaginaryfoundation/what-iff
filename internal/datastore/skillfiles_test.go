package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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
	script, err := ds.CreateSkillFile(ctx, userID, skill.ID, folder, name, "text/x-python", "users/u/skills/s/v1_fill.py")
	require.NoError(t, err)
	require.Equal(t, models.FileOriginSkill, script.Origin)
	require.Equal(t, &skill.ID, script.SkillID)
	folder, name, err = SkillFileFolder(skill.Slug, "references/FORMS.md")
	require.NoError(t, err)
	_, err = ds.CreateSkillFile(ctx, userID, skill.ID, folder, name, "text/markdown", "users/u/skills/s/v1_forms.md")
	require.NoError(t, err)

	// Another user's skill id is refused.
	_, err = ds.CreateSkillFile(ctx, other, skill.ID, folder, "x.md", "text/markdown", "k")
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
	oldKey, err := ds.ReplaceSkillFileObject(ctx, userID, skill.ID, script.ID, "users/u/skills/s/v2_fill.py", "text/x-python")
	require.NoError(t, err)
	require.Equal(t, "users/u/skills/s/v1_fill.py", oldKey)
	_, err = ds.UpdateRitual(ctx, userID, models.Ritual{ID: skill.ID, Slug: "forms", Name: "Forms", Description: "d", Content: "c"})
	require.NoError(t, err)
	files, err = ds.ListSkillFiles(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Equal(t, "skills/forms/references", files[0].Folder)
	require.Equal(t, "skills/forms/scripts", files[1].Folder)
	require.Equal(t, "users/u/skills/s/v2_fill.py", files[1].S3Key)

	refs, err := ds.ListRitualFileAttachmentObjectRefs(ctx, userID, skill.ID)
	require.NoError(t, err)
	require.Len(t, refs, 2)

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
	_, err = ds.CreateSkillFile(ctx, userID, skill.ID, folder, name, "image/png", "users/u/skills/s/logo.png")
	require.NoError(t, err)
	require.NoError(t, ds.DeleteRitual(ctx, userID, skill.ID))
	leftover, err := ds.ListFileAttachments(ctx, userID, 1, 10, models.FileAttachmentFilters{Origin: models.FileOriginSkill})
	require.NoError(t, err)
	require.Equal(t, 0, leftover.TotalCount)
}
