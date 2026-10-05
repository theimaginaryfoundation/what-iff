package memory

import (
	"context"
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

func TestListMemoriesHandler_RejectsInvalidSensitivityFilter(t *testing.T) {
	// The filter is validated before the datastore is touched, so no store is needed.
	h := &Handler{logger: zap.NewNop()}
	req := httptest.NewRequest(http.MethodGet, "/memory?sensitivity=secret", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	h.ListMemories(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
