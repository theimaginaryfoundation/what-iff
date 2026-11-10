package filesave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filechange"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeStore struct {
	rows    map[uuid.UUID]*models.FileAttachment
	swapOK  bool
	swaps   []datastore.SwapFileObjectOptions
	cleared []uuid.UUID
	getErr  error
}

func (f *fakeStore) GetFileAttachment(_ context.Context, userID, id uuid.UUID) (*models.FileAttachment, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.rows[id]
	if !ok || row.UserID != userID {
		return nil, datastore.ErrFileAttachmentNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *fakeStore) SwapFileObject(_ context.Context, userID, id uuid.UUID, oldKey, newKey string, opts datastore.SwapFileObjectOptions) (bool, error) {
	f.swaps = append(f.swaps, opts)
	row := f.rows[id]
	if !f.swapOK || row.S3Key != oldKey || (opts.ExpectVersion != nil && *opts.ExpectVersion != row.ContentVersion) {
		return false, nil
	}
	row.S3Key, row.ContentVersion, row.ContentSHA256 = newKey, row.ContentVersion+1, opts.SHA256
	size := opts.SizeBytes
	row.SizeBytes = &size
	return true, nil
}

func (f *fakeStore) ClearFileChunks(_ context.Context, id uuid.UUID) error {
	f.cleared = append(f.cleared, id)
	return nil
}

func (f *fakeStore) ExistingFileAttachmentIDs(context.Context, []uuid.UUID) (map[uuid.UUID]bool, error) {
	return map[uuid.UUID]bool{}, nil
}

func (f *fakeStore) ReferencedFileAttachmentKeys(_ context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, row := range f.rows {
		for _, k := range keys {
			if row.S3Key == k {
				out[k] = true
			}
		}
	}
	return out, nil
}

type fakeFiles struct {
	objects map[string][]byte
	types   map[string]string
	deleted []string
}

func (f *fakeFiles) UploadFile(_ context.Context, key string, content []byte, contentType string) error {
	f.objects[key] = content
	f.types[key] = contentType
	return nil
}
func (f *fakeFiles) DownloadFile(_ context.Context, key string) ([]byte, error) {
	return f.objects[key], nil
}
func (f *fakeFiles) DeleteFile(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}

type fakeChunker struct{ calls []string }

func (c *fakeChunker) ProcessAndStore(_ context.Context, _ uuid.UUID, content []byte, name, _ string) error {
	c.calls = append(c.calls, name+"="+string(content))
	return nil
}

func newService(t *testing.T, rows ...*models.FileAttachment) (*Service, *fakeStore, *fakeFiles, *fakeChunker) {
	t.Helper()
	st := &fakeStore{rows: map[uuid.UUID]*models.FileAttachment{}, swapOK: true}
	for _, r := range rows {
		st.rows[r.ID] = r
	}
	files := &fakeFiles{objects: map[string][]byte{}, types: map[string]string{}}
	ch := &fakeChunker{}
	s := New(st, files, nil, zap.NewNop())
	s.chunker = ch
	s.rechunk = func(fn func()) { fn() }
	return s, st, files, ch
}

func TestReplace_SwapsReleasesReindexesAndNotifies(t *testing.T) {
	filechange.Reset()
	t.Cleanup(filechange.Reset)
	var events []filechange.Event
	filechange.Subscribe(func(_ context.Context, ev filechange.Event) { events = append(events, ev) })

	userID, chatID := uuid.New(), uuid.New()
	row := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "notes.md", FileType: "text/markdown", S3Key: "users/u/chats/c/old.md", ContentVersion: 3, ChunkStatus: "chunked"}
	s, st, files, ch := newService(t, row)
	files.objects[row.S3Key] = []byte("old")

	v3 := 3
	got, err := s.Replace(context.Background(), userID, Request{ID: row.ID, Content: []byte("new text"), ContentType: "text/markdown; charset=utf-8", IfMatch: &v3, ChatID: &chatID})
	require.NoError(t, err)
	require.Equal(t, 4, got.ContentVersion)
	require.True(t, strings.HasPrefix(got.S3Key, "users/"+userID.String()+"/files/"), got.S3Key)
	require.Equal(t, []byte("new text"), files.objects[got.S3Key])
	require.Equal(t, "text/markdown", files.types[got.S3Key])
	sum := sha256.Sum256([]byte("new text"))
	require.Equal(t, hex.EncodeToString(sum[:]), got.ContentSHA256)
	require.Equal(t, []string{"users/u/chats/c/old.md"}, files.deleted, "the replaced object is released")
	require.Len(t, st.swaps, 1)
	require.True(t, st.swaps[0].MoveSharedRows)
	require.Equal(t, []uuid.UUID{row.ID}, st.cleared)
	require.Equal(t, []string{"notes.md=new text"}, ch.calls)
	require.Len(t, events, 1)
	require.Equal(t, filechange.KindContent, events[0].Kind)
	require.Equal(t, "users/u/chats/c/old.md", events[0].OldKey)
	require.Equal(t, &chatID, events[0].ChatID)
	require.Equal(t, got.S3Key, events[0].File.S3Key)
}

func TestReplace_ConflictDropsTheUploadAndReportsTheCurrentVersion(t *testing.T) {
	userID := uuid.New()
	row := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "a.txt", FileType: "text/plain", S3Key: "k1", ContentVersion: 5}
	s, _, files, _ := newService(t, row)
	stale := 4
	_, err := s.Replace(context.Background(), userID, Request{ID: row.ID, Content: []byte("x"), IfMatch: &stale})
	var conflict *ConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, 5, conflict.Current)
	require.Empty(t, files.objects, "the unused object is deleted")
	require.Len(t, files.deleted, 1)
}

func TestReplace_Refusals(t *testing.T) {
	userID := uuid.New()
	skillID := uuid.New()
	text := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "a.txt", FileType: "text/plain", S3Key: "k", ContentVersion: 1}
	skill := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "SKILL.md", FileType: "text/markdown", S3Key: "s", Origin: models.FileOriginSkill, SkillID: &skillID}
	ctxOnly := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "c.txt", FileType: "text/plain", S3Key: "k", ContextOnly: true}
	img := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "p.png", FileType: "image/png", S3Key: "i"}
	s, _, files, _ := newService(t, text, skill, ctxOnly, img)

	_, err := s.Replace(context.Background(), uuid.New(), Request{ID: text.ID, Content: []byte("x")})
	require.ErrorIs(t, err, ErrNotFound, "another user's file")
	_, err = s.Replace(context.Background(), userID, Request{ID: ctxOnly.ID, Content: []byte("x")})
	require.ErrorIs(t, err, ErrNotFound, "a context-only row is not a gallery file")
	_, err = s.Replace(context.Background(), userID, Request{ID: skill.ID, Content: []byte("x")})
	require.ErrorIs(t, err, ErrSkillFile)
	_, err = s.Replace(context.Background(), userID, Request{ID: text.ID, Content: []byte("x"), ContentType: "application/json"})
	require.ErrorIs(t, err, ErrTypeMismatch)
	_, err = s.Replace(context.Background(), userID, Request{ID: img.ID, Content: []byte("not an image")})
	require.ErrorIs(t, err, ErrInvalidImage)
	s.MaxBytes = 3
	_, err = s.Replace(context.Background(), userID, Request{ID: text.ID, Content: []byte("too long")})
	require.ErrorIs(t, err, ErrTooLarge)
	require.Empty(t, files.objects, "nothing was uploaded")

	s.files = nil
	_, err = s.Replace(context.Background(), userID, Request{ID: text.ID, Content: []byte("x")})
	require.ErrorIs(t, err, ErrNoStore)
}

func TestReplace_AgentFileStaysUnderTheAgentPrefixAndIsNotReindexed(t *testing.T) {
	userID := uuid.New()
	row := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "journal.md", FileType: "text/markdown", S3Key: "users/u/agent/v1.md", ContentVersion: 2, Origin: models.FileOriginAgent}
	s, st, _, ch := newService(t, row)
	got, err := s.Replace(context.Background(), userID, Request{ID: row.ID, Content: []byte("v2")})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got.S3Key, "users/"+userID.String()+"/agent/"), got.S3Key)
	require.Nil(t, st.swaps[0].ExpectVersion, "no If-Match overwrites unconditionally")
	require.Empty(t, ch.calls, "agent files are never chunked")
	require.Empty(t, st.cleared)
}

func TestReplace_ImageGetsAThumbnailAndStaysInTheImageStore(t *testing.T) {
	userID := uuid.New()
	row := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "p.png", FileType: "image/png", S3Key: "users/u/images/old.png", ContentVersion: 1}
	s, _, files, _ := newService(t, row)
	got, err := s.Replace(context.Background(), userID, Request{ID: row.ID, Content: tinyPNG(t), ContentType: "image/png"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got.S3Key, "users/"+userID.String()+"/images/"), got.S3Key)
	thumbKey := "users/" + userID.String() + "/images/thumbs/" + row.ID.String() + ".jpg"
	require.NotEmpty(t, files.objects[thumbKey])
	require.Equal(t, "image/jpeg", files.types[thumbKey])
}

func TestReplace_StoreErrorsPassThrough(t *testing.T) {
	userID := uuid.New()
	row := &models.FileAttachment{ID: uuid.New(), UserID: userID, Name: "a.txt", FileType: "text/plain", S3Key: "k"}
	s, st, _, _ := newService(t, row)
	st.getErr = errors.New("db down")
	_, err := s.Replace(context.Background(), userID, Request{ID: row.ID, Content: []byte("x")})
	require.EqualError(t, err, "db down")
}
