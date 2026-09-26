package agent

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

// gridPNGBase64 renders a size×size PNG with nine distinctly coloured 3×3 tiles.
func gridPNGBase64(t *testing.T, size int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cell := size / 3
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			idx := min(y/cell, 2)*3 + min(x/cell, 2)
			img.Set(x, y, color.RGBA{R: uint8(20 * idx), G: uint8(200 - 20*idx), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// fakeImageAPI is an httptest OpenAI stand-in for the likeness (Responses) and image endpoints.
// Each endpoint answers with its configured status/body and records the request bodies it saw.
type fakeImageAPI struct {
	mu sync.Mutex

	likenessText string
	editStatus   int
	editB64      string
	genStatus    int
	genB64       string

	likenessBodies []string
	editBodies     []string
	genBodies      []string
}

func (f *fakeImageAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/responses":
			f.likenessBodies = append(f.likenessBodies, string(body))
			_, _ = w.Write([]byte(responseTextJSONBody("resp_likeness", f.likenessText)))
		case "/images/edits":
			f.editBodies = append(f.editBodies, string(body))
			writeImageAnswer(w, f.editStatus, f.editB64)
		case "/images/generations":
			f.genBodies = append(f.genBodies, string(body))
			writeImageAnswer(w, f.genStatus, f.genB64)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeImageAnswer(w http.ResponseWriter, status int, b64 string) {
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"rejected by test"}}`))
		return
	}
	_, _ = w.Write([]byte(imagesSuccessBody(b64)))
}

func newGridTestAgent(srv *httptest.Server) *Agent {
	return &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
}

var customGridKeys = []string{"smug", "wry_grin", "sad", "furious", "shocked", "lost", "sleepy", "smitten", "pondering"}

func TestGenerateExpressionGridCells_PromptOnlyUsesCustomKeysAndStyle(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A fox-eared bard in a green cloak.", genB64: gridPNGBase64(t, 96)}
	a := newGridTestAgent(api.server(t))

	cells, err := a.generateExpressionGridCells(context.Background(),
		&models.Personality{SystemPrompt: "You are a bard.", ImageStyle: "watercolor"}, customGridKeys, expressionGridReference{})
	require.NoError(t, err)
	require.Len(t, cells, 9)
	for i, c := range cells {
		_, err := png.Decode(bytes.NewReader(c))
		require.NoError(t, err, "cell %d must be a PNG", i)
	}

	require.Len(t, api.likenessBodies, 1)
	require.Empty(t, api.editBodies, "no reference means no edit call")
	require.Len(t, api.genBodies, 1)
	prompt := api.genBodies[0]
	require.Contains(t, prompt, "A fox-eared bard in a green cloak.")
	require.Contains(t, prompt, "smug | wry grin | sad; furious | shocked | lost; sleepy | smitten | pondering")
	require.Contains(t, prompt, "Art style: watercolor")
	require.NotContains(t, prompt, "style and character reference")
}

func TestGenerateExpressionGridCells_AutoStyleOmitsArtStyleLine(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A robot.", genB64: gridPNGBase64(t, 60)}
	a := newGridTestAgent(api.server(t))

	_, err := a.generateExpressionGridCells(context.Background(),
		&models.Personality{SystemPrompt: "You are a robot.", ImageStyle: "auto"}, ExpressionGridKeys, expressionGridReference{})
	require.NoError(t, err)
	require.NotContains(t, api.genBodies[0], "Art style:")
}

func TestGenerateExpressionGridCells_EmptyLikenessUsesFallbackProse(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{genB64: gridPNGBase64(t, 60)}
	a := newGridTestAgent(api.server(t))

	// No system prompt and no reference: the likeness pass is skipped entirely.
	_, err := a.generateExpressionGridCells(context.Background(), &models.Personality{}, ExpressionGridKeys, expressionGridReference{})
	require.NoError(t, err)
	require.Empty(t, api.likenessBodies)
	require.Contains(t, api.genBodies[0], "A distinctive character portrait consistent with the personality")
}

func TestGenerateExpressionGridCells_ReferenceGoesToEditEndpoint(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A knight.", editB64: gridPNGBase64(t, 90)}
	a := newGridTestAgent(api.server(t))

	ref := expressionGridReference{bytes: []byte("\x89PNG-ref"), mime: "image/png", sendToImageModel: true}
	cells, err := a.generateExpressionGridCells(context.Background(), &models.Personality{SystemPrompt: "You are a knight."}, customGridKeys, ref)
	require.NoError(t, err)
	require.Len(t, cells, 9)

	require.Len(t, api.editBodies, 1)
	require.Empty(t, api.genBodies, "a successful edit must not also generate")
	require.Contains(t, api.editBodies[0], "style and character reference")
	require.Contains(t, api.editBodies[0], "reference.png")
	// The likeness pass also saw the image, as a vision input.
	require.Contains(t, api.likenessBodies[0], "input_image")
	require.Contains(t, api.likenessBodies[0], "data:image/png;base64,")
}

func TestGenerateExpressionGridCells_LikenessOnlyReferenceSkipsEdit(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A knight.", genB64: gridPNGBase64(t, 60)}
	a := newGridTestAgent(api.server(t))

	// The default-grid path grounds only the likeness pass with the cover image.
	ref := expressionGridReference{bytes: []byte("cover"), mime: "image/jpeg"}
	_, err := a.generateExpressionGridCells(context.Background(), &models.Personality{SystemPrompt: "x"}, ExpressionGridKeys, ref)
	require.NoError(t, err)
	require.Empty(t, api.editBodies)
	require.Len(t, api.genBodies, 1)
	require.Contains(t, api.likenessBodies[0], "data:image/jpeg;base64,")
}

func TestGenerateExpressionGridCells_RejectedEditFallsBackToGeneration(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A knight.", editStatus: http.StatusBadRequest, genB64: gridPNGBase64(t, 60)}
	a := newGridTestAgent(api.server(t))

	ref := expressionGridReference{bytes: []byte("ref"), mime: "image/webp", sendToImageModel: true}
	cells, err := a.generateExpressionGridCells(context.Background(), &models.Personality{SystemPrompt: "x"}, ExpressionGridKeys, ref)
	require.NoError(t, err)
	require.Len(t, cells, 9)
	require.Len(t, api.editBodies, 1)
	require.Len(t, api.genBodies, 1)
	require.NotContains(t, api.genBodies[0], "style and character reference",
		"the prompt-only fallback must not mention an image the model never received")
}

func TestGenerateExpressionGridCells_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		api     *fakeImageAPI
		wantErr string
	}{
		{"generation rejected", &fakeImageAPI{likenessText: "x", genStatus: http.StatusBadRequest}, "generate grid image"},
		{"not base64", &fakeImageAPI{likenessText: "x", genB64: "%%%not-base64%%%"}, "decode generated image"},
		{"not a png", &fakeImageAPI{likenessText: "x", genB64: base64.StdEncoding.EncodeToString([]byte("GIF89a"))}, "slice grid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newGridTestAgent(tc.api.server(t))
			_, err := a.generateExpressionGridCells(context.Background(), &models.Personality{SystemPrompt: "x"}, ExpressionGridKeys, expressionGridReference{})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestTruncateExpressionGridPrompt(t *testing.T) {
	t.Parallel()
	require.Equal(t, "short", truncateExpressionGridPrompt("short"))
	require.Len(t, truncateExpressionGridPrompt(strings.Repeat("a", 20000)), 16000)
}

func TestHumanizeExpressionKey(t *testing.T) {
	t.Parallel()
	require.Equal(t, "in love", humanizeExpressionKey(" in-love "))
	require.Equal(t, "wry grin", humanizeExpressionKey("wry_grin"))
}

// recordingFileStore is an in-memory storage.FileStore that records uploads and deletes.
type recordingFileStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	deleted   []string
	uploadErr error
}

var _ storage.FileStore = (*recordingFileStore)(nil)

func newRecordingFileStore() *recordingFileStore {
	return &recordingFileStore{objects: map[string][]byte{}}
}

func (s *recordingFileStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadErr != nil {
		return s.uploadErr
	}
	s.objects[key] = content
	return nil
}

func (s *recordingFileStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objects[key], nil
}

func (s *recordingFileStore) DeleteFile(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	return nil
}

func TestGenerateExpressionCandidates_ImageStyleNone(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	pid := uuid.New()
	expectPersonalityLookup(mock, pid, models.ImageStyleNone)

	a := &Agent{ds: ds, logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider("http://127.0.0.1:0"), fileStore: newRecordingFileStore()}
	_, err := a.GenerateExpressionCandidates(context.Background(), uuid.New(), pid, ExpressionGridKeys, nil)
	require.ErrorIs(t, err, ErrExpressionImagesDisabled)
	require.NoError(t, mock.ExpectationsWereMet())
}

// expectCreatePersonalityAttachment mocks CreateFileAttachment for a personality-pinned image
// (user check, insert, relationship reload, expression-usage lookup) returning attID.
func expectCreatePersonalityAttachment(mock sqlmock.Sqlmock, uid, pid, attID uuid.UUID) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `users`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uid.String()))
	mock.ExpectExec("INSERT INTO `file_attachments`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT .* FROM `file_attachments`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at", "name", "file_type", "user_file_attachments", "personality_file_attachments"}).
			AddRow(attID.String(), time.Now(), time.Now(), "expression.png", "image/png", uid.String(), pid.String()))
	mock.ExpectQuery("SELECT .* FROM `users`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uid.String()))
	mock.ExpectQuery("SELECT .* FROM `personalities`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(pid.String()))
	mock.ExpectQuery("SELECT .* FROM `personality_expressions`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()
}

func TestUploadExpressionCellAttachment_StoresImageAndThumbnail(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	uid, pid, attID := uuid.New(), uuid.New(), uuid.New()
	expectCreatePersonalityAttachment(mock, uid, pid, attID)
	mock.ExpectExec("UPDATE `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 1))

	store := newRecordingFileStore()
	a := &Agent{ds: ds, logger: zap.NewNop(), fileStore: store}
	raw, _ := base64.StdEncoding.DecodeString(gridPNGBase64(t, 30))
	id, err := a.uploadExpressionCellAttachment(context.Background(), uid, pid, "smug", raw)
	require.NoError(t, err)
	require.Equal(t, attID, id)
	require.Contains(t, store.objects, storage.FileKeyForPersonality(uid, pid, attID, "expression-smug.png"))
	require.Contains(t, store.objects, storage.FileKeyForImageThumbnail(uid, attID))
	require.NoError(t, mock.ExpectationsWereMet())
}

// expectDeleteFileAttachment mocks DeleteFileAttachment for an owned attachment.
func expectDeleteFileAttachment(mock sqlmock.Sqlmock, attID uuid.UUID) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `file_attachments`").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(attID.String()))
	mock.ExpectExec("DELETE FROM `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func newCandidatesTestAgent(t *testing.T, api *fakeImageAPI, store *recordingFileStore) (*Agent, sqlmock.Sqlmock) {
	t.Helper()
	ds, mock, cleanup := newTestDatastore(t)
	t.Cleanup(cleanup)
	return &Agent{ds: ds, logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(api.server(t).URL), fileStore: store}, mock
}

func TestGenerateExpressionCandidates_UploadsNineUnassignedCandidates(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A knight.", editB64: gridPNGBase64(t, 90)}
	store := newRecordingFileStore()
	a, mock := newCandidatesTestAgent(t, api, store)

	uid, pid, refID := uuid.New(), uuid.New(), uuid.New()
	store.objects[storage.FileKeyForImage(uid, refID, "ref.png")] = []byte("reference-bytes")

	expectPersonalityLookup(mock, pid, "auto")
	expectFileAttachmentLookup(mock, refID, "image/png")
	attIDs := make([]uuid.UUID, len(customGridKeys))
	for i := range customGridKeys {
		attIDs[i] = uuid.New()
		expectCreatePersonalityAttachment(mock, uid, pid, attIDs[i])
		mock.ExpectExec("UPDATE `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 1))
	}

	got, err := a.GenerateExpressionCandidates(context.Background(), uid, pid, customGridKeys, &refID)
	require.NoError(t, err)
	require.Len(t, got, 9)
	for i, c := range got {
		require.Equal(t, customGridKeys[i], c.ExpressionKey)
		require.Equal(t, attIDs[i], c.ImageID)
	}
	// The reference reached the image model; no slot was assigned (no expression upserts mocked).
	require.Len(t, api.editBodies, 1)
	require.Contains(t, api.editBodies[0], "reference-bytes")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGenerateExpressionCandidates_MidRunFailureDeletesEarlierCandidates(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "A knight.", genB64: gridPNGBase64(t, 60)}
	store := newRecordingFileStore()
	a, mock := newCandidatesTestAgent(t, api, store)

	uid, pid := uuid.New(), uuid.New()
	first, second := uuid.New(), uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	// Cell 1 uploads cleanly.
	expectCreatePersonalityAttachment(mock, uid, pid, first)
	mock.ExpectExec("UPDATE `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 1))
	// Cell 2's S3 key never persists (row vanished), so its own upload rolls back...
	expectCreatePersonalityAttachment(mock, uid, pid, second)
	mock.ExpectExec("UPDATE `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 0))
	expectDeleteFileAttachment(mock, second)
	// ...and the run then removes cell 1 so a failed run leaves nothing behind.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `file_attachments`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at", "name", "file_type", "s3_key"}).
			AddRow(first.String(), time.Now(), time.Now(), "expression-happy.png", "image/png",
				storage.FileKeyForPersonality(uid, pid, first, "expression-happy.png")))
	mock.ExpectQuery("SELECT .* FROM `personality_expressions`").WillReturnRows(sqlmock.NewRows([]string{"personality_expression_image"}))
	mock.ExpectCommit()
	expectDeleteFileAttachment(mock, first)

	got, err := a.GenerateExpressionCandidates(context.Background(), uid, pid, ExpressionGridKeys, nil)
	require.Nil(t, got)
	require.ErrorContains(t, err, `expression "content"`)
	require.NoError(t, mock.ExpectationsWereMet())

	require.Empty(t, store.objects, "every uploaded object (cells and thumbnails) must be deleted")
	require.Contains(t, store.deleted, storage.FileKeyForPersonality(uid, pid, first, "expression-happy.png"))
	require.Contains(t, store.deleted, storage.FileKeyForImageThumbnail(uid, first))
}

func TestGenerateExpressionCandidates_GridFailureUploadsNothing(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "x", genStatus: http.StatusBadRequest}
	store := newRecordingFileStore()
	a, mock := newCandidatesTestAgent(t, api, store)

	pid := uuid.New()
	expectPersonalityLookup(mock, pid, "auto")

	_, err := a.GenerateExpressionCandidates(context.Background(), uuid.New(), pid, ExpressionGridKeys, nil)
	require.ErrorContains(t, err, "generate grid image")
	require.Empty(t, store.objects)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadExpressionReferenceImage(t *testing.T) {
	t.Parallel()

	t.Run("missing attachment proceeds without a reference", func(t *testing.T) {
		t.Parallel()
		store := newRecordingFileStore()
		a, mock := newCandidatesTestAgent(t, &fakeImageAPI{}, store)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT .* FROM `file_attachments`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectRollback()

		b, mime := a.loadExpressionReferenceImage(context.Background(), uuid.New(), uuid.New())
		require.Nil(t, b)
		require.Empty(t, mime)
	})

	t.Run("oversized image falls back to the thumbnail", func(t *testing.T) {
		t.Parallel()
		store := newRecordingFileStore()
		a, mock := newCandidatesTestAgent(t, &fakeImageAPI{}, store)
		uid, refID := uuid.New(), uuid.New()
		store.objects[storage.FileKeyForImage(uid, refID, "ref.png")] = make([]byte, maxExpressionReferenceImageBytes+1)
		store.objects[storage.FileKeyForImageThumbnail(uid, refID)] = []byte("thumb")
		expectFileAttachmentLookup(mock, refID, "image/png")

		b, mime := a.loadExpressionReferenceImage(context.Background(), uid, refID)
		require.Equal(t, []byte("thumb"), b)
		require.Equal(t, "image/jpeg", mime)
	})

	t.Run("no stored bytes yields no reference", func(t *testing.T) {
		t.Parallel()
		store := newRecordingFileStore()
		a, mock := newCandidatesTestAgent(t, &fakeImageAPI{}, store)
		refID := uuid.New()
		expectFileAttachmentLookup(mock, refID, "image/png")

		b, _ := a.loadExpressionReferenceImage(context.Background(), uuid.New(), refID)
		require.Empty(t, b)
	})
}

// expectNineCandidateUploads mocks a clean upload for each key and returns the attachment IDs.
func expectNineCandidateUploads(mock sqlmock.Sqlmock, uid, pid uuid.UUID, keys []string) []uuid.UUID {
	ids := make([]uuid.UUID, len(keys))
	for i := range keys {
		ids[i] = uuid.New()
		expectCreatePersonalityAttachment(mock, uid, pid, ids[i])
		mock.ExpectExec("UPDATE `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	return ids
}

func TestRunExpressionCandidatesJob_RecordsCandidatesOnProgress(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "x", genB64: gridPNGBase64(t, 60)}
	a, mock := newCandidatesTestAgent(t, api, newRecordingFileStore())

	uid, pid, jobID := uuid.New(), uuid.New(), uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	ids := expectNineCandidateUploads(mock, uid, pid, customGridKeys)
	mock.ExpectExec("UPDATE `jobs` SET .*`progress`").
		WithArgs(sqlmock.AnyArg(), progressWithCandidate(ids[0]), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	resultID, err := a.runExpressionCandidatesJob(context.Background(), uid, jobID, pid, customGridKeys, nil)
	require.NoError(t, err)
	require.Equal(t, pid, resultID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRunExpressionCandidatesJob_UnrecordedCandidatesAreDeleted(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "x", genB64: gridPNGBase64(t, 60)}
	store := newRecordingFileStore()
	a, mock := newCandidatesTestAgent(t, api, store)

	uid, pid := uuid.New(), uuid.New()
	expectPersonalityLookup(mock, pid, "auto")
	ids := expectNineCandidateUploads(mock, uid, pid, ExpressionGridKeys)
	mock.ExpectExec("UPDATE `jobs`").WillReturnError(errors.New("db gone"))
	for _, id := range ids {
		// Lookup fails, so only the row delete is attempted for each orphan.
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT .* FROM `file_attachments`").WillReturnError(errors.New("db gone"))
		mock.ExpectRollback()
		expectDeleteFileAttachment(mock, id)
	}

	_, err := a.runExpressionCandidatesJob(context.Background(), uid, uuid.New(), pid, ExpressionGridKeys, nil)
	require.ErrorContains(t, err, "record expression candidates")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGenerateDefaultExpressionGrid_ImageStyleNoneSkipsGeneration(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{}
	a, mock := newCandidatesTestAgent(t, api, newRecordingFileStore())

	pid := uuid.New()
	expectPersonalityLookup(mock, pid, models.ImageStyleNone)

	got, err := a.GenerateDefaultExpressionGrid(context.Background(), uuid.New(), pid)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Empty(t, api.genBodies)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGenerateDefaultExpressionGrid_GridFailure(t *testing.T) {
	t.Parallel()
	api := &fakeImageAPI{likenessText: "x", genStatus: http.StatusBadRequest}
	a, mock := newCandidatesTestAgent(t, api, newRecordingFileStore())

	pid := uuid.New()
	expectPersonalityLookup(mock, pid, "auto")

	_, err := a.GenerateDefaultExpressionGrid(context.Background(), uuid.New(), pid)
	require.ErrorContains(t, err, "generate grid image")
	// The default grid never sends a reference to the image model.
	require.Empty(t, api.editBodies)
	require.NoError(t, mock.ExpectationsWereMet())
}

// progressWithCandidate matches a Job.Progress argument that records imageID as a candidate.
type progressWithCandidate uuid.UUID

func (p progressWithCandidate) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && IsExpressionCandidatesProgress(s) && strings.Contains(s, uuid.UUID(p).String())
}
