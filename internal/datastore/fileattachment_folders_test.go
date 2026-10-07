package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// folderFixture is a datastore with images spread over a few folders.
type folderFixture struct {
	ds   *Datastore
	user uuid.UUID
	ids  map[string]uuid.UUID // by name
}

func newFolderFixture(t *testing.T) *folderFixture {
	t.Helper()
	ds, cleanup := newFileAttachmentTestDatastore(t)
	t.Cleanup(cleanup)
	f := &folderFixture{ds: ds, user: createFATestUser(t, ds), ids: map[string]uuid.UUID{}}
	for name, folder := range map[string]string{
		"root.png":      "",
		"sleep.png":     "charts",
		"hrv.png":       "charts/oura",
		"readiness.png": "charts/oura",
		"old.png":       "charts-old",
		"art.png":       "art",
	} {
		f.add(t, name, "image/png", folder)
	}
	f.add(t, "notes.txt", "text/plain", "")
	return f
}

func (f *folderFixture) add(t *testing.T, name, fileType, folder string) uuid.UUID {
	t.Helper()
	att, err := f.ds.CreateFileAttachment(context.Background(), f.user, models.FileAttachment{
		Name: name, FileType: fileType, Folder: folder, S3Key: "k/" + name,
	})
	require.NoError(t, err)
	f.ids[name] = att.ID
	return att.ID
}

func (f *folderFixture) names(t *testing.T, filters models.FileAttachmentFilters) []string {
	t.Helper()
	page, err := f.ds.ListFileAttachments(context.Background(), f.user, 1, 100, filters)
	require.NoError(t, err)
	var out []string
	for _, r := range page.Results {
		out = append(out, r.(*models.FileAttachment).Name)
	}
	return out
}

func (f *folderFixture) folderOf(t *testing.T, name string) string {
	t.Helper()
	att, err := f.ds.GetFileAttachment(context.Background(), f.user, f.ids[name])
	require.NoError(t, err)
	return att.Folder
}

func strp(s string) *string { return &s }

func TestFileAttachmentFolderIsStoredNormalizedAndReturned(t *testing.T) {
	f := newFolderFixture(t)
	id := f.add(t, "x.png", "image/png", "  Charts / Oura/ ")
	att, err := f.ds.GetFileAttachment(context.Background(), f.user, id)
	require.NoError(t, err)
	assert.Equal(t, "charts/oura", att.Folder)

	_, err = f.ds.CreateFileAttachment(context.Background(), f.user, models.FileAttachment{Name: "y.png", FileType: "image/png", Folder: "../up"})
	assert.ErrorIs(t, err, models.ErrInvalidFolder)
}

func TestListFileAttachmentsByFolder(t *testing.T) {
	f := newFolderFixture(t)

	assert.ElementsMatch(t, []string{"root.png", "notes.txt"}, f.names(t, models.FileAttachmentFilters{Folder: strp("")}), "the top level holds only unfiled files")
	assert.ElementsMatch(t, []string{"sleep.png"}, f.names(t, models.FileAttachmentFilters{Folder: strp("charts")}), "an exact folder is not its children")
	assert.ElementsMatch(t, []string{"hrv.png", "readiness.png"}, f.names(t, models.FileAttachmentFilters{Folder: strp("charts/oura")}))
	assert.Len(t, f.names(t, models.FileAttachmentFilters{}), 7, "no folder filter lists everything, as before")
}

func TestListFileAttachmentsByFolderPrefix(t *testing.T) {
	f := newFolderFixture(t)

	assert.ElementsMatch(t, []string{"sleep.png", "hrv.png", "readiness.png"}, f.names(t, models.FileAttachmentFilters{FolderPrefix: strp("charts")}),
		"the folder and everything beneath it, not the sibling that shares a prefix")
	assert.ElementsMatch(t, []string{"hrv.png", "readiness.png"}, f.names(t, models.FileAttachmentFilters{FolderPrefix: strp("charts/oura")}))
	assert.Len(t, f.names(t, models.FileAttachmentFilters{FolderPrefix: strp("")}), 7, "an empty prefix is no filter")
}

func TestFolderPrefixTreatsPercentAndUnderscoreAsLiterals(t *testing.T) {
	f := newFolderFixture(t)
	f.add(t, "pct.png", "image/png", "100%")
	assert.Equal(t, []string{"pct.png"}, f.names(t, models.FileAttachmentFilters{FolderPrefix: strp("100%")}))
	assert.Empty(t, f.names(t, models.FileAttachmentFilters{FolderPrefix: strp("%")}), "a wildcard character matches nothing special")
	assert.Empty(t, f.names(t, models.FileAttachmentFilters{FolderPrefix: strp("c_arts")}))
}

func TestListGalleryFoldersCountsFilesOfTheKindDirectlyInEachFolder(t *testing.T) {
	f := newFolderFixture(t)
	f.add(t, "report.pdf", "application/pdf", "charts")
	f.add(t, "plan.md", "text/markdown", "docs")
	ctx := context.Background()

	images, err := f.ds.ListGalleryFolders(ctx, f.user, models.GalleryKindImages)
	require.NoError(t, err)
	assert.Equal(t, []models.FolderCount{
		{Path: "art", Count: 1},
		{Path: "charts", Count: 1},
		{Path: "charts-old", Count: 1},
		{Path: "charts/oura", Count: 2},
	}, images, "sorted by path; the top level and other files are not counted")

	files, err := f.ds.ListGalleryFolders(ctx, f.user, models.GalleryKindFiles)
	require.NoError(t, err)
	assert.Equal(t, []models.FolderCount{
		{Path: "charts", Count: 1},
		{Path: "docs", Count: 1},
	}, files, "only folders holding a non-image file, counting only those")

	all, err := f.ds.ListGalleryFolders(ctx, f.user, models.GalleryKindAll)
	require.NoError(t, err)
	assert.Equal(t, []models.FolderCount{
		{Path: "art", Count: 1},
		{Path: "charts", Count: 2},
		{Path: "charts-old", Count: 1},
		{Path: "charts/oura", Count: 2},
		{Path: "docs", Count: 1},
	}, all, "images and other files share one folder tree")
}

func TestListFileAttachmentsByGalleryKind(t *testing.T) {
	f := newFolderFixture(t)
	f.add(t, "report.pdf", "application/pdf", "charts")

	assert.ElementsMatch(t, []string{"notes.txt", "report.pdf"}, f.names(t, models.FileAttachmentFilters{Kind: models.GalleryKindFiles}))
	assert.Len(t, f.names(t, models.FileAttachmentFilters{Kind: models.GalleryKindImages}), 6)
	assert.Len(t, f.names(t, models.FileAttachmentFilters{Kind: models.GalleryKindAll}), 8)
	assert.ElementsMatch(t, []string{"sleep.png"}, f.names(t, models.FileAttachmentFilters{Kind: models.GalleryKindImages, Folder: strp("charts")}),
		"kind combines with the folder filter")
}

func TestFoldersAreScopedToTheirOwner(t *testing.T) {
	f := newFolderFixture(t)
	other := createFATestUser(t, f.ds)

	folders, err := f.ds.ListGalleryFolders(context.Background(), other, models.GalleryKindAll)
	require.NoError(t, err)
	assert.Empty(t, folders)

	moved, err := f.ds.MoveFileAttachmentsToFolder(context.Background(), other, []uuid.UUID{f.ids["sleep.png"]}, "stolen")
	require.NoError(t, err)
	assert.Zero(t, moved, "someone else's image is not moved")
	assert.Equal(t, "charts", f.folderOf(t, "sleep.png"))

	moved, err = f.ds.MoveGalleryFolder(context.Background(), other, "charts", "mine")
	require.NoError(t, err)
	assert.Zero(t, moved)
	assert.Equal(t, "charts", f.folderOf(t, "sleep.png"))
}

func TestMoveFileAttachmentsToFolder(t *testing.T) {
	f := newFolderFixture(t)
	ctx := context.Background()

	moved, err := f.ds.MoveFileAttachmentsToFolder(ctx, f.user, []uuid.UUID{f.ids["root.png"], f.ids["art.png"], uuid.New()}, "stash/2026")
	require.NoError(t, err)
	assert.Equal(t, 2, moved, "unknown ids are skipped")
	assert.Equal(t, "stash/2026", f.folderOf(t, "root.png"))
	assert.Equal(t, "stash/2026", f.folderOf(t, "art.png"))

	moved, err = f.ds.MoveFileAttachmentsToFolder(ctx, f.user, []uuid.UUID{f.ids["root.png"]}, "")
	require.NoError(t, err)
	assert.Equal(t, 1, moved)
	assert.Equal(t, "", f.folderOf(t, "root.png"), "moving to the top level clears the folder")

	moved, err = f.ds.MoveFileAttachmentsToFolder(ctx, f.user, []uuid.UUID{f.ids["notes.txt"]}, "charts")
	require.NoError(t, err)
	assert.Equal(t, 1, moved, "documents are filed like images")
	assert.Equal(t, "charts", f.folderOf(t, "notes.txt"))

	moved, err = f.ds.MoveFileAttachmentsToFolder(ctx, f.user, nil, "x")
	require.NoError(t, err)
	assert.Zero(t, moved)
}

func TestMoveGalleryFolderRenamesItAndEverythingBeneath(t *testing.T) {
	f := newFolderFixture(t)
	f.add(t, "report.pdf", "application/pdf", "charts/oura")

	moved, err := f.ds.MoveGalleryFolder(context.Background(), f.user, "charts", "archive/charts")

	require.NoError(t, err)
	assert.Equal(t, 4, moved)
	assert.Equal(t, "archive/charts/oura", f.folderOf(t, "report.pdf"), "a document rides along with the images")
	assert.Equal(t, "archive/charts", f.folderOf(t, "sleep.png"))
	assert.Equal(t, "archive/charts/oura", f.folderOf(t, "hrv.png"))
	assert.Equal(t, "archive/charts/oura", f.folderOf(t, "readiness.png"))
	assert.Equal(t, "charts-old", f.folderOf(t, "old.png"), "a sibling that shares a prefix is left alone")
}

func TestMoveGalleryFolderToTheTopLevelAndIntoAnExistingFolder(t *testing.T) {
	f := newFolderFixture(t)
	ctx := context.Background()

	moved, err := f.ds.MoveGalleryFolder(ctx, f.user, "charts/oura", "")
	require.NoError(t, err)
	assert.Equal(t, 2, moved)
	assert.Equal(t, "", f.folderOf(t, "hrv.png"), "its contents move up to the top level")

	moved, err = f.ds.MoveGalleryFolder(ctx, f.user, "art", "charts")
	require.NoError(t, err)
	assert.Equal(t, 1, moved)
	assert.Equal(t, "charts", f.folderOf(t, "art.png"), "moving onto an existing folder merges them")
	assert.Equal(t, "charts", f.folderOf(t, "sleep.png"))
}

func TestMoveGalleryFolderRefusesImpossibleMoves(t *testing.T) {
	f := newFolderFixture(t)
	ctx := context.Background()

	_, err := f.ds.MoveGalleryFolder(ctx, f.user, "charts", "charts/oura/deeper")
	assert.ErrorIs(t, err, ErrFolderIntoItself)
	_, err = f.ds.MoveGalleryFolder(ctx, f.user, "", "anywhere")
	assert.ErrorIs(t, err, models.ErrInvalidFolder, "the top level cannot be moved")

	moved, err := f.ds.MoveGalleryFolder(ctx, f.user, "charts", "charts")
	require.NoError(t, err)
	assert.Zero(t, moved, "moving a folder onto itself is a no-op")
	assert.Equal(t, "charts/oura", f.folderOf(t, "hrv.png"))
}

func TestReferenceCopiesNeverCarryAFolderGetCountedOrBlockAMove(t *testing.T) {
	f := newFolderFixture(t)
	ctx := context.Background()

	ref, err := f.ds.CreateFileAttachmentReference(ctx, f.user, f.ids["sleep.png"])
	require.NoError(t, err)
	assert.Equal(t, "", ref.Folder, "a chat-reuse copy starts at the top level")

	folders, err := f.ds.ListGalleryFolders(ctx, f.user, models.GalleryKindImages)
	require.NoError(t, err)
	for _, fc := range folders {
		if fc.Path == "charts" {
			assert.Equal(t, 1, fc.Count, "the copy does not double-count the image")
		}
	}

	// Moving by the copy's id moves the original it shares a stored object with.
	moved, err := f.ds.MoveFileAttachmentsToFolder(ctx, f.user, []uuid.UUID{ref.ID}, "elsewhere")
	require.NoError(t, err)
	assert.Equal(t, 1, moved)
	assert.Equal(t, "elsewhere", f.folderOf(t, "sleep.png"))

	// Naming the original and its copy together still counts the image once.
	moved, err = f.ds.MoveFileAttachmentsToFolder(ctx, f.user, []uuid.UUID{ref.ID, f.ids["sleep.png"]}, "again")
	require.NoError(t, err)
	assert.Equal(t, 1, moved)
}
