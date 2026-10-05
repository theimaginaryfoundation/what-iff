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
