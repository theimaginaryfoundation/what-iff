package memory

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestParseBatchIDs(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	got, err := parseBatchIDs([]string{id.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != id {
		t.Fatalf("unexpected ids: %#v", got)
	}

	if _, err := parseBatchIDs(nil); err == nil {
		t.Fatal("expected error for empty ids")
	}
	if _, err := parseBatchIDs([]string{"not-a-uuid"}); err == nil {
		t.Fatal("expected error for invalid uuid")
	}
	tooMany := make([]string, models.MaxMemoryBatchIDs+1)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	if _, err := parseBatchIDs(tooMany); err == nil {
		t.Fatal("expected error for too many ids")
	}
}

func TestParseBatchPatchRequiresField(t *testing.T) {
	t.Parallel()

	if _, err := parseBatchPatch(nil); err == nil {
		t.Fatal("expected error for nil patch")
	}
	if _, err := parseBatchPatch(map[string]any{}); err == nil {
		t.Fatal("expected error for empty patch")
	}

	status := "inactive"
	patch, err := parseBatchPatch(map[string]any{"status": status})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch.Status == nil || string(*patch.Status) != status {
		t.Fatalf("expected status inactive, got %#v", patch.Status)
	}
}

func TestDeleteMemoriesBatchUnauthorized(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body, _ := json.Marshal(batchDeleteRequest{IDs: []string{uuid.New().String()}})
	req := httptest.NewRequest(http.MethodPost, "/memory/batch/delete", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}
