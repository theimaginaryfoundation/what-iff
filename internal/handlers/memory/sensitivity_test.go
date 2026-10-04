package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestParseSensitivityValue(t *testing.T) {
	for in, want := range map[string]models.MemorySensitivity{
		"public": models.MemorySensitivityPublic, "personal": models.MemorySensitivityPersonal,
		"sensitive": models.MemorySensitivitySensitive, " Sensitive ": models.MemorySensitivitySensitive,
	} {
		got, err := parseSensitivityValue(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got)
	}
	for _, bad := range []string{"", "secret", "private"} {
		_, err := parseSensitivityValue(bad)
		require.Error(t, err, bad)
	}
}

func TestToCreateMemoryInput_Sensitivity(t *testing.T) {
	in, err := toCreateMemoryInput(createMemoryRequest{Content: "x", Level: models.MemoryLevelGlobal})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivity(""), in.Sensitivity, "omitted stays empty; the datastore applies the personal default")

	in, err = toCreateMemoryInput(createMemoryRequest{Content: "x", Level: models.MemoryLevelGlobal, Sensitivity: "sensitive"})
	require.NoError(t, err)
	require.Equal(t, models.MemorySensitivitySensitive, in.Sensitivity)

	_, err = toCreateMemoryInput(createMemoryRequest{Content: "x", Level: models.MemoryLevelGlobal, Sensitivity: "secret"})
	require.Error(t, err)
}

func TestCreateMemoryHandler_Sensitivity(t *testing.T) {
	store := newFakeMemoryStore()
	_, router := newEmbeddingTestHandler(store, nil)

	rec := serveAuthed(t, router, http.MethodPost, "/memory", createMemoryRequest{Content: "default", Level: models.MemoryLevelGlobal})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, models.MemorySensitivityPersonal, decodeMemory(t, rec).Sensitivity)

	rec = serveAuthed(t, router, http.MethodPost, "/memory", createMemoryRequest{Content: "delicate", Level: models.MemoryLevelGlobal, Sensitivity: "sensitive"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeMemory(t, rec)
	require.Equal(t, models.MemorySensitivitySensitive, created.Sensitivity)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Equal(t, "sensitive", raw["sensitivity"], "the response carries the JSON field")

	rec = serveAuthed(t, router, http.MethodPost, "/memory", createMemoryRequest{Content: "bad", Level: models.MemoryLevelGlobal, Sensitivity: "secret"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPatchMemoryHandler_Sensitivity(t *testing.T) {
	store := newFakeMemoryStore()
	id := store.seed("a memory", false)
	_, router := newEmbeddingTestHandler(store, nil)

	rec := serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"sensitivity": "public"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, models.MemorySensitivityPublic, decodeMemory(t, rec).Sensitivity)

	rec = serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"sensitivity": "secret"})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"sensitivity": 3})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = serveAuthed(t, router, http.MethodPatch, "/memory/"+id.String(), map[string]any{"starred": true})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, models.MemorySensitivityPublic, decodeMemory(t, rec).Sensitivity, "an unrelated patch leaves it alone")
}

func TestParseBatchPatch_Sensitivity(t *testing.T) {
	patch, err := parseBatchPatch(map[string]any{"sensitivity": "sensitive"})
	require.NoError(t, err)
	require.NotNil(t, patch.Sensitivity)
	require.Equal(t, models.MemorySensitivitySensitive, *patch.Sensitivity)

	_, err = parseBatchPatch(map[string]any{"sensitivity": "secret"})
	require.Error(t, err)
	_, err = parseBatchPatch(map[string]any{"sensitivity": 7})
	require.Error(t, err)
}

func TestPatchMemoriesBatchHandler_SensitivityReachesTheStore(t *testing.T) {
	store := newFakeMemoryStore()
	a, b := store.seed("a", false), store.seed("b", false)
	_, router := newEmbeddingTestHandler(store, nil)

	rec := serveAuthed(t, router, http.MethodPost, "/memory/batch/patch", map[string]any{
		"ids":   []string{a.String(), b.String()},
		"patch": map[string]any{"sensitivity": "sensitive"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res models.BatchPatchMemoryResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.Equal(t, 2, res.UpdatedCount)
	for _, m := range res.Results {
		require.Equal(t, models.MemorySensitivitySensitive, m.Sensitivity)
	}

	rec = serveAuthed(t, router, http.MethodPost, "/memory/batch/patch", map[string]any{
		"ids": []string{uuid.NewString()}, "patch": map[string]any{"sensitivity": "bogus"},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListMemoriesHandler_RejectsInvalidSensitivityFilter(t *testing.T) {
	// The filter is validated before the datastore is touched, so no store is needed.
	h := &Handler{logger: zap.NewNop()}
	req := httptest.NewRequest(http.MethodGet, "/memory?sensitivity=secret", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	h.ListMemories(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
