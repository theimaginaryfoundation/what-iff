package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeMemoryStore is an in-memory memoryWriteStore that mirrors the datastore
// contract the handlers rely on: UpdateMemory drops the embedding when (and
// only when) content changes, and SetMemoryEmbedding only writes while the
// memory still holds the content the vector was computed from.
type fakeMemoryStore struct {
	mu         sync.Mutex
	memories   map[uuid.UUID]*models.Memory
	embeddings map[uuid.UUID]string // memory id -> content the stored vector was computed from
}

func newFakeMemoryStore() *fakeMemoryStore {
	return &fakeMemoryStore{
		memories:   map[uuid.UUID]*models.Memory{},
		embeddings: map[uuid.UUID]string{},
	}
}

func (f *fakeMemoryStore) seed(content string, embedded bool) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.memories[id] = &models.Memory{ID: id, Content: content, Level: models.MemoryLevelGlobal}
	if embedded {
		f.embeddings[id] = content
	}
	return id
}

func (f *fakeMemoryStore) embeddedContent(id uuid.UUID) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.embeddings[id]
	return c, ok
}

func (f *fakeMemoryStore) CreateMemoryFromInput(_ context.Context, _ uuid.UUID, input models.CreateMemoryInput) (*models.Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := &models.Memory{ID: uuid.New(), Content: strings.TrimSpace(input.Content), Level: input.Level, Sensitivity: input.Sensitivity.OrDefault()}
	f.memories[m.ID] = m
	return m, nil
}

func (f *fakeMemoryStore) CreateMemoriesBatch(ctx context.Context, userID uuid.UUID, input models.BatchCreateMemoryInput) ([]*models.Memory, error) {
	out := make([]*models.Memory, 0, len(input.Items))
	for _, item := range input.Items {
		m, err := f.CreateMemoryFromInput(ctx, userID, item)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeMemoryStore) UpdateMemory(_ context.Context, _ uuid.UUID, memoryID uuid.UUID, patch models.MemoryPatch) (*models.Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.memories[memoryID]
	if !ok {
		return nil, errors.New("not found")
	}
	if patch.Content != nil {
		next := strings.TrimSpace(*patch.Content)
		if next != m.Content {
			m.Content = next
			delete(f.embeddings, memoryID)
		}
	}
	if patch.Starred != nil {
		m.Starred = *patch.Starred
	}
	if patch.Sensitivity != nil {
		m.Sensitivity = *patch.Sensitivity
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMemoryStore) PatchMemoriesBatch(ctx context.Context, userID uuid.UUID, input models.BatchPatchMemoryInput) (*models.BatchPatchMemoryResult, error) {
	out := make([]*models.Memory, 0, len(input.IDs))
	for _, id := range input.IDs {
		m, err := f.UpdateMemory(ctx, userID, id, input.Patch)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return &models.BatchPatchMemoryResult{Results: out, UpdatedCount: len(out)}, nil
}

func (f *fakeMemoryStore) OwnedMemoriesMissingEmbedding(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]models.MemoryEmbeddingCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.MemoryEmbeddingCandidate
	for _, id := range ids {
		m, ok := f.memories[id]
		if !ok {
			continue
		}
		if _, embedded := f.embeddings[id]; embedded {
			continue
		}
		out = append(out, models.MemoryEmbeddingCandidate{MemoryID: id, Content: m.Content})
	}
	return out, nil
}

func (f *fakeMemoryStore) SetMemoryEmbedding(_ context.Context, memoryID uuid.UUID, content string, vec []float32) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.memories[memoryID]
	if !ok || m.Content != content || len(vec) == 0 {
		return false, nil
	}
	f.embeddings[memoryID] = content
	return true, nil
}

// recordingEmbedder records every embeddings call and can be told to fail.
type recordingEmbedder struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (e *recordingEmbedder) embed(_ context.Context, inputs []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, append([]string(nil), inputs...))
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = []float32{float32(i) + 1, 0.5}
	}
	return out, nil
}

func (e *recordingEmbedder) allCalls() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func newEmbeddingTestHandler(store *fakeMemoryStore, embedder *recordingEmbedder) (*Handler, *mux.Router) {
	h := &Handler{logger: zap.NewNop(), store: store}
	if embedder != nil {
		h.embedTexts = embedder.embed
	}
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return h, router
}

func serveAuthed(t *testing.T, router *mux.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeMemory(t *testing.T, rec *httptest.ResponseRecorder) models.Memory {
	t.Helper()
	var m models.Memory
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

func TestCreateMemoryEmbedsContent(t *testing.T) {
	store := newFakeMemoryStore()
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPost, "/memory", createMemoryRequest{Content: "  likes green tea  ", Level: models.MemoryLevelGlobal})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeMemory(t, rec)
	require.Equal(t, [][]string{{"likes green tea"}}, embedder.allCalls())
	got, ok := store.embeddedContent(created.ID)
	require.True(t, ok, "created memory should have an embedding")
	require.Equal(t, "likes green tea", got)
}

func TestCreateMemorySucceedsWhenEmbeddingFails(t *testing.T) {
	store := newFakeMemoryStore()
	embedder := &recordingEmbedder{err: errors.New("provider down")}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPost, "/memory", createMemoryRequest{Content: "likes green tea", Level: models.MemoryLevelGlobal})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeMemory(t, rec)
	require.Equal(t, "likes green tea", created.Content)
	require.Len(t, embedder.allCalls(), 1)
	_, ok := store.embeddedContent(created.ID)
	require.False(t, ok, "failed embedding leaves the memory for backfill")
}

func TestCreateMemorySkipsEmbeddingWithoutProvider(t *testing.T) {
	store := newFakeMemoryStore()
	_, router := newEmbeddingTestHandler(store, nil) // no OpenAI key, no override

	rec := serveAuthed(t, router, http.MethodPost, "/memory", createMemoryRequest{Content: "likes green tea", Level: models.MemoryLevelGlobal})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	_, ok := store.embeddedContent(decodeMemory(t, rec).ID)
	require.False(t, ok)
}

func TestPatchMemoryContentChangedReembeds(t *testing.T) {
	store := newFakeMemoryStore()
	id := store.seed("likes green tea", true)
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"content": "prefers black coffee"})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, [][]string{{"prefers black coffee"}}, embedder.allCalls())
	got, ok := store.embeddedContent(id)
	require.True(t, ok)
	require.Equal(t, "prefers black coffee", got, "embedding must track the edited text, not the old one")
}

func TestPatchMemoryContentUnchangedDoesNotReembed(t *testing.T) {
	store := newFakeMemoryStore()
	id := store.seed("likes green tea", true)
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"content": "likes green tea", "starred": true})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, embedder.allCalls(), "unchanged content must not call the embedding provider")
	got, ok := store.embeddedContent(id)
	require.True(t, ok)
	require.Equal(t, "likes green tea", got)
}

func TestPatchMemoryWithoutContentDoesNotEmbed(t *testing.T) {
	store := newFakeMemoryStore()
	id := store.seed("likes green tea", false)
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"starred": true})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, embedder.allCalls(), "non-content patches leave embedding to the backfill")
}

func TestPatchMemoryContentChangedEmbeddingFailureStillSaves(t *testing.T) {
	store := newFakeMemoryStore()
	id := store.seed("likes green tea", true)
	embedder := &recordingEmbedder{err: errors.New("provider down")}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"content": "prefers black coffee"})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "prefers black coffee", decodeMemory(t, rec).Content)
	_, ok := store.embeddedContent(id)
	require.False(t, ok, "stale embedding is gone and the memory is left for backfill")
}

func TestCreateMemoriesBatchEmbedsAllInOneCall(t *testing.T) {
	store := newFakeMemoryStore()
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPost, "/memory/batch", createMemoriesBatchRequest{
		Items: []createMemoryRequest{
			{Content: "first fact", Level: models.MemoryLevelGlobal},
			{Content: "second fact", Level: models.MemoryLevelGlobal},
		},
	})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp createMemoriesBatchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 2)

	calls := embedder.allCalls()
	require.Len(t, calls, 1, "batch create embeds in a single provider call")
	require.ElementsMatch(t, []string{"first fact", "second fact"}, calls[0])
	for _, m := range resp.Results {
		got, ok := store.embeddedContent(m.ID)
		require.True(t, ok)
		require.Equal(t, m.Content, got)
	}
}

func TestPatchMemoriesBatchReembedsOnlyChangedContent(t *testing.T) {
	store := newFakeMemoryStore()
	changed := store.seed("old wording", true)
	alreadyCurrent := store.seed("new wording", true)
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPost, "/memory/batch/patch", batchPatchRequest{
		IDs:   []string{changed.String(), alreadyCurrent.String()},
		Patch: map[string]any{"content": "new wording"},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, [][]string{{"new wording"}}, embedder.allCalls(), "only the memory whose content changed is re-embedded")
	for _, id := range []uuid.UUID{changed, alreadyCurrent} {
		got, ok := store.embeddedContent(id)
		require.True(t, ok)
		require.Equal(t, "new wording", got)
	}
}

func TestPatchMemoriesBatchWithoutContentDoesNotEmbed(t *testing.T) {
	store := newFakeMemoryStore()
	id := store.seed("likes green tea", true)
	embedder := &recordingEmbedder{}
	_, router := newEmbeddingTestHandler(store, embedder)

	rec := serveAuthed(t, router, http.MethodPost, "/memory/batch/patch", batchPatchRequest{
		IDs:   []string{id.String()},
		Patch: map[string]any{"starred": true},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, embedder.allCalls())
}
