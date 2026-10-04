package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeRefs answers reference questions from an in-memory set of remaining rows.
type fakeRefs struct {
	rows      []models.FileAttachment // the user's remaining rows
	otherKeys []string                // s3_keys held by rows outside the user's listing
	err       error
}

func (f *fakeRefs) ReferencedFileAttachmentKeys(_ context.Context, keys []string) (map[string]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]bool{}
	for _, k := range keys {
		for _, r := range f.rows {
			if r.S3Key == k {
				out[k] = true
			}
		}
		for _, o := range f.otherKeys {
			if o == k {
				out[k] = true
			}
		}
	}
	return out, nil
}

func (f *fakeRefs) ExistingFileAttachmentIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		for _, r := range f.rows {
			if r.ID == id {
				out[id] = true
			}
		}
	}
	return out, nil
}

func (f *fakeRefs) ListUserFileAttachmentObjectRefs(context.Context, uuid.UUID) ([]models.FileAttachment, error) {
	return f.rows, nil
}

type localTestStore struct {
	FileStore
	dir string
}

func newLocalTestStore(t *testing.T) localTestStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCAL_FILE_STORE_DIR", dir)
	fs, err := NewFileStore(context.Background(), "", "", zap.NewNop())
	require.NoError(t, err)
	return localTestStore{FileStore: fs, dir: dir}
}

// put writes key and sets its modification time to age ago.
func (s localTestStore) put(t *testing.T, key string, age time.Duration) {
	t.Helper()
	require.NoError(t, s.UploadFile(context.Background(), key, []byte(key), "application/octet-stream"))
	mtime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(filepath.Join(s.dir, filepath.FromSlash(key)), mtime, mtime))
}

func (s localTestStore) has(t *testing.T, key string) bool {
	t.Helper()
	b, err := s.DownloadFile(context.Background(), key)
	require.NoError(t, err)
	return b != nil
}

func TestReleaseAttachmentObjects(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()

	t.Run("deletes unreferenced keys and thumbnails, keeps shared keys", func(t *testing.T) {
		s := newLocalTestStore(t)
		gone := models.FileAttachment{ID: uuid.New(), Name: "a.png", FileType: "image/png"}
		gone.S3Key = FileKeyForImage(userID, gone.ID, gone.Name)
		shared := models.FileAttachment{ID: uuid.New(), Name: "b.png", FileType: "image/png"}
		shared.S3Key = FileKeyForImage(userID, shared.ID, shared.Name)
		copyRow := models.FileAttachment{ID: uuid.New(), FileType: "image/png", S3Key: shared.S3Key}
		for _, k := range []string{gone.S3Key, shared.S3Key, FileKeyForImageThumbnail(userID, gone.ID), FileKeyForImageThumbnail(userID, shared.ID)} {
			s.put(t, k, 0)
		}

		res := ReleaseAttachmentObjects(ctx, zap.NewNop(), s.FileStore, &fakeRefs{rows: []models.FileAttachment{copyRow}}, userID,
			[]models.FileAttachment{gone, shared})

		require.False(t, s.has(t, gone.S3Key))
		require.True(t, s.has(t, shared.S3Key), "still referenced by the copy")
		require.False(t, s.has(t, FileKeyForImageThumbnail(userID, gone.ID)))
		require.False(t, s.has(t, FileKeyForImageThumbnail(userID, shared.ID)))
		require.Equal(t, ReleaseResult{Deleted: 3, Kept: 1}, res)
	})

	t.Run("legacy row without s3_key releases its derived keys", func(t *testing.T) {
		s := newLocalTestStore(t)
		doc := models.FileAttachment{ID: uuid.New(), Name: "n.txt", FileType: "text/plain", ChatID: &chatID}
		chatKey := FileKeyForChat(userID, chatID, doc.ID, doc.Name)
		s.put(t, chatKey, 0)

		ReleaseAttachmentObjects(ctx, zap.NewNop(), s.FileStore, &fakeRefs{}, userID, []models.FileAttachment{doc})
		require.False(t, s.has(t, chatKey))
	})

	t.Run("reference check failure keeps shared objects", func(t *testing.T) {
		s := newLocalTestStore(t)
		img := models.FileAttachment{ID: uuid.New(), Name: "c.png", FileType: "image/png"}
		img.S3Key = FileKeyForImage(userID, img.ID, img.Name)
		s.put(t, img.S3Key, 0)

		res := ReleaseAttachmentObjects(ctx, zap.NewNop(), s.FileStore, &fakeRefs{err: errors.New("db down")}, userID, []models.FileAttachment{img})
		require.True(t, s.has(t, img.S3Key))
		require.Equal(t, 1, res.Kept)
	})

	t.Run("a row that survived the delete keeps its objects", func(t *testing.T) {
		s := newLocalTestStore(t)
		img := models.FileAttachment{ID: uuid.New(), Name: "d.png", FileType: "image/png", ChatID: &chatID}
		thumb := FileKeyForImageThumbnail(userID, img.ID)
		legacy := FileKeyForImage(userID, img.ID, img.Name) // no s3_key: derived keys only
		s.put(t, thumb, 0)
		s.put(t, legacy, 0)

		// The cascade did not fire, so the row is still there.
		res := ReleaseAttachmentObjects(ctx, zap.NewNop(), s.FileStore, &fakeRefs{rows: []models.FileAttachment{img}}, userID, []models.FileAttachment{img})
		require.True(t, s.has(t, thumb))
		require.True(t, s.has(t, legacy))
		require.Equal(t, ReleaseResult{Kept: 1}, res)
	})

	t.Run("nil store is a no-op", func(t *testing.T) {
		require.Equal(t, ReleaseResult{}, ReleaseAttachmentObjects(ctx, nil, nil, &fakeRefs{}, userID, []models.FileAttachment{{ID: uuid.New()}}))
	})
}

func TestPurgeUserObjects_RemovesUserPrefixAndExportBundles(t *testing.T) {
	s := newLocalTestStore(t)
	userID, other := uuid.New(), uuid.New()
	mine := []string{
		FileKeyForImage(userID, uuid.New(), "a.png"),
		FileKeyForChat(userID, uuid.New(), uuid.New(), "b.txt"),
		"users/" + userID.String() + "/workspace/" + uuid.NewString() + "/rev-1.md",
		"users/" + userID.String() + "/stray-object-no-row-points-at",
		// Export bundles live outside users/{id}/, at exports/{id}/ (accountexport.bundlePrefix).
		UserExportPrefix(userID) + "account-export-20260101-000000.zip",
	}
	theirs := []string{
		FileKeyForImage(other, uuid.New(), "c.png"),
		UserExportPrefix(other) + "account-export-20260101-000000.zip",
	}
	for _, k := range append(append([]string{}, mine...), theirs...) {
		s.put(t, k, 0)
	}

	res := PurgeUserObjects(context.Background(), zap.NewNop(), s.FileStore, &fakeRefs{}, userID, nil)
	require.Equal(t, len(mine), res.Deleted)
	for _, k := range mine {
		require.False(t, s.has(t, k), k)
	}
	for _, k := range theirs {
		require.True(t, s.has(t, k), "other users' objects are untouched: "+k)
	}
}

func TestSweepUserOrphans(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	chatID := uuid.New()
	old := 48 * time.Hour

	setup := func(t *testing.T) (localTestStore, *fakeRefs, map[string]string) {
		s := newLocalTestStore(t)
		img := models.FileAttachment{ID: uuid.New(), Name: "keep.png", FileType: "image/png"}
		img.S3Key = FileKeyForImage(userID, img.ID, img.Name)
		legacy := models.FileAttachment{ID: uuid.New(), Name: "legacy.txt", FileType: "text/plain", ChatID: &chatID}
		keys := map[string]string{
			"referenced":     img.S3Key,
			"thumb":          FileKeyForImageThumbnail(userID, img.ID),
			"legacy":         FileKeyForChat(userID, chatID, legacy.ID, legacy.Name),
			"otherOwnerRef":  FileKeyForImage(userID, uuid.New(), "shared-elsewhere.png"),
			"orphan":         FileKeyForImage(userID, uuid.New(), "deleted.png"),
			"orphanThumb":    FileKeyForImageThumbnail(userID, uuid.New()),
			"recentOrphan":   FileKeyForChat(userID, chatID, uuid.New(), "uploading.txt"),
			"fallbackOrphan": FileKeyFallback(userID, uuid.New(), "f.txt"),
			// Not an attachment layout: referenced from elsewhere, never swept.
			"workspace":     "users/" + userID.String() + "/workspace/" + uuid.NewString() + "/rev-1.md",
			"futureFeature": "users/" + userID.String() + "/voice-notes/" + uuid.NewString() + ".ogg",
			"topLevelNoID":  "users/" + userID.String() + "/notes.txt",
			"imagesNoID":    "users/" + userID.String() + "/images/banner.png",
			// Outside users/{id}/ entirely.
			"export":          UserExportPrefix(userID) + "account-export-20260101-000000.zip",
			"otherUserOrphan": FileKeyForImage(uuid.New(), uuid.New(), "not-mine.png"),
		}
		for name, k := range keys {
			age := old
			if name == "recentOrphan" {
				age = time.Minute
			}
			s.put(t, k, age)
		}
		refs := &fakeRefs{rows: []models.FileAttachment{img, legacy}, otherKeys: []string{keys["otherOwnerRef"]}}
		return s, refs, keys
	}

	t.Run("dry run reports orphans and deletes nothing", func(t *testing.T) {
		s, refs, keys := setup(t)
		res, err := SweepUserOrphans(ctx, zap.NewNop(), s.FileStore, refs, userID, SweepOptions{DryRun: true, GracePeriod: 24 * time.Hour})
		require.NoError(t, err)
		require.True(t, res.DryRun)
		require.ElementsMatch(t, []string{keys["orphan"], keys["orphanThumb"], keys["fallbackOrphan"]}, orphanKeys(res))
		require.Equal(t, 0, res.Deleted)
		require.Equal(t, 1, res.TooRecent)
		require.Equal(t, 4, res.SkippedUnknown, "workspace/, an unknown feature, and non-id keys are left alone")
		require.Equal(t, 4, res.Referenced)
		require.Equal(t, 12, res.Scanned, "only users/{id}/ is listed")
		for _, k := range keys {
			require.True(t, s.has(t, k), k)
		}
	})

	t.Run("real run deletes only orphans past the grace period, and is idempotent", func(t *testing.T) {
		s, refs, keys := setup(t)
		res, err := SweepUserOrphans(ctx, zap.NewNop(), s.FileStore, refs, userID, SweepOptions{GracePeriod: 24 * time.Hour})
		require.NoError(t, err)
		require.Equal(t, 3, res.Deleted)
		require.Empty(t, res.Failed)
		for name, k := range keys {
			want := name != "orphan" && name != "orphanThumb" && name != "fallbackOrphan"
			require.Equal(t, want, s.has(t, k), name)
		}

		again, err := SweepUserOrphans(ctx, zap.NewNop(), s.FileStore, refs, userID, SweepOptions{GracePeriod: 24 * time.Hour})
		require.NoError(t, err)
		require.Empty(t, again.Orphans)
		require.Equal(t, 0, again.Deleted)
	})

	t.Run("grace period decides what is old enough", func(t *testing.T) {
		s, refs, keys := setup(t)
		// With a grace period longer than every object's age, nothing is an orphan yet.
		res, err := SweepUserOrphans(ctx, zap.NewNop(), s.FileStore, refs, userID, SweepOptions{GracePeriod: 72 * time.Hour})
		require.NoError(t, err)
		require.Empty(t, res.Orphans)
		// The four unreferenced attachment objects, plus the one only another owner's row
		// references (the grace check runs before that cross-owner lookup).
		require.Equal(t, 5, res.TooRecent)

		// Moving "now" forward ages the recent upload past the grace period too.
		res, err = SweepUserOrphans(ctx, zap.NewNop(), s.FileStore, refs, userID, SweepOptions{DryRun: true, GracePeriod: 24 * time.Hour, Now: time.Now().Add(25 * time.Hour)})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{keys["orphan"], keys["orphanThumb"], keys["fallbackOrphan"], keys["recentOrphan"]}, orphanKeys(res))
	})

	t.Run("store without listing is reported", func(t *testing.T) {
		_, err := SweepUserOrphans(ctx, zap.NewNop(), nonListingStore{}, &fakeRefs{}, userID, SweepOptions{DryRun: true})
		require.ErrorIs(t, err, ErrObjectListingUnsupported)
	})

	t.Run("negative grace period is rejected", func(t *testing.T) {
		s, refs, _ := setup(t)
		_, err := SweepUserOrphans(ctx, zap.NewNop(), s.FileStore, refs, userID, SweepOptions{GracePeriod: -time.Hour})
		require.Error(t, err)
	})
}

func TestIsAttachmentObjectKey(t *testing.T) {
	id, other := uuid.NewString(), uuid.NewString()
	for rel, want := range map[string]bool{
		id:                                      true,
		id + "_notes.txt":                       true,
		"images/" + id + "_cat.png":             true,
		"images/thumbs/" + id + ".jpg":          true,
		"chats/" + other + "/" + id + "_a.txt":  true,
		"personalities/" + other + "/" + id:     true,
		"notes.txt":                             false,
		id + "notes.txt":                        false, // id must be followed by "_" or end
		"images/banner.png":                     false,
		"images/thumbs/" + id + ".png":          false,
		"images/thumbs/banner.jpg":              false,
		"chats/not-a-uuid/" + id:                false,
		"chats/" + other + "/nested/" + id:      false,
		"workspace/" + other + "/" + id:         false,
		"exports/" + id + "_account-export.zip": false,
		"voice-notes/" + id + ".ogg":            false,
	} {
		require.Equal(t, want, isAttachmentObjectKey(rel), rel)
	}
}

func orphanKeys(res *SweepResult) []string {
	out := make([]string, 0, len(res.Orphans))
	for _, o := range res.Orphans {
		out = append(out, o.Key)
	}
	return out
}

type nonListingStore struct{}

func (nonListingStore) UploadFile(context.Context, string, []byte, string) error { return nil }
func (nonListingStore) DownloadFile(context.Context, string) ([]byte, error)     { return nil, nil }
func (nonListingStore) DeleteFile(context.Context, string) error                 { return nil }
