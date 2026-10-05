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

func TestParseProvenanceValue(t *testing.T) {
	got, err := parseProvenanceValue(" External ")
	require.NoError(t, err)
	require.Equal(t, models.MemoryProvenanceExternal, got)
	got, err = parseProvenanceValue("user")
	require.NoError(t, err)
	require.Equal(t, models.MemoryProvenanceUser, got)
	for _, bad := range []string{"", "discord", "verified"} {
		_, err := parseProvenanceValue(bad)
		require.Error(t, err, bad)
	}
}

func TestParseBatchPatch_Provenance(t *testing.T) {
	patch, err := parseBatchPatch(map[string]any{"provenance": "user"})
	require.NoError(t, err)
	require.NotNil(t, patch.Provenance)
	require.Equal(t, models.MemoryProvenanceUser, *patch.Provenance)

	_, err = parseBatchPatch(map[string]any{"provenance": "owner"})
	require.Error(t, err)
}

func TestListMemoriesHandler_RejectsInvalidProvenanceFilter(t *testing.T) {
	h := &Handler{logger: zap.NewNop()}
	req := httptest.NewRequest(http.MethodGet, "/memory?provenance=discord", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	h.ListMemories(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
