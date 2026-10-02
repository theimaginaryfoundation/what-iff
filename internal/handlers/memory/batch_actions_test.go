package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
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

func TestParseBatchPatch_UnknownFieldOnlyStillSucceedsWithEmptyPatch(t *testing.T) {
	t.Parallel()

	// The "at least one field" guard counts every key in the payload, not just
	// recognized ones, so a patch containing only an unrecognized field is
	// accepted as a no-op rather than rejected.
	patch, err := parseBatchPatch(map[string]any{"totally_unknown_field": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch.Content != nil || patch.Level != nil || patch.Type != nil || patch.Starred != nil ||
		patch.Status != nil || patch.Confidence != nil || patch.SetChatID || patch.SetPinnedPersonalityID {
		t.Fatalf("expected an all-nil no-op patch, got %#v", patch)
	}
}

func TestParseBatchPatch_AllRecognizedFields(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	personalityID := uuid.New()

	patch, err := parseBatchPatch(map[string]any{
		"content":               "new content",
		"level":                 "thread",
		"type":                  "Context",
		"starred":               true,
		"status":                "active",
		"confidence":            "high",
		"chat_id":               chatID.String(),
		"pinned_personality_id": personalityID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch.Content == nil || *patch.Content != "new content" {
		t.Fatalf("unexpected content: %#v", patch.Content)
	}
	if patch.Level == nil || *patch.Level != models.MemoryLevelThread {
		t.Fatalf("unexpected level: %#v", patch.Level)
	}
	if patch.Type == nil || *patch.Type != models.MemoryTypeContext {
		t.Fatalf("unexpected type: %#v", patch.Type)
	}
	if patch.Starred == nil || !*patch.Starred {
		t.Fatalf("unexpected starred: %#v", patch.Starred)
	}
	if patch.Status == nil || *patch.Status != models.MemoryStatusActive {
		t.Fatalf("unexpected status: %#v", patch.Status)
	}
	if patch.Confidence == nil || *patch.Confidence != models.MemoryConfidenceHigh {
		t.Fatalf("unexpected confidence: %#v", patch.Confidence)
	}
	if !patch.SetChatID || patch.ChatID == nil || *patch.ChatID != chatID {
		t.Fatalf("unexpected chat_id: set=%v value=%#v", patch.SetChatID, patch.ChatID)
	}
	if !patch.SetPinnedPersonalityID || patch.PinnedPersonalityID == nil || *patch.PinnedPersonalityID != personalityID {
		t.Fatalf("unexpected pinned_personality_id: set=%v value=%#v", patch.SetPinnedPersonalityID, patch.PinnedPersonalityID)
	}
}

func TestParseBatchPatch_ChatIDNullAndEmptyStringClearWithSetFlag(t *testing.T) {
	t.Parallel()

	patch, err := parseBatchPatch(map[string]any{"chat_id": nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patch.SetChatID || patch.ChatID != nil {
		t.Fatalf("expected SetChatID=true with nil value, got set=%v value=%#v", patch.SetChatID, patch.ChatID)
	}

	patch, err = parseBatchPatch(map[string]any{"chat_id": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patch.SetChatID || patch.ChatID != nil {
		t.Fatalf("expected SetChatID=true with nil value for empty string, got set=%v value=%#v", patch.SetChatID, patch.ChatID)
	}
}

func TestParseBatchPatch_PinnedPersonalityIDNullAndEmptyStringClearWithSetFlag(t *testing.T) {
	t.Parallel()

	patch, err := parseBatchPatch(map[string]any{"pinned_personality_id": nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patch.SetPinnedPersonalityID || patch.PinnedPersonalityID != nil {
		t.Fatalf("expected SetPinnedPersonalityID=true with nil value, got set=%v value=%#v", patch.SetPinnedPersonalityID, patch.PinnedPersonalityID)
	}

	patch, err = parseBatchPatch(map[string]any{"pinned_personality_id": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patch.SetPinnedPersonalityID || patch.PinnedPersonalityID != nil {
		t.Fatalf("expected SetPinnedPersonalityID=true with nil value for empty string, got set=%v value=%#v", patch.SetPinnedPersonalityID, patch.PinnedPersonalityID)
	}
}

func TestParseBatchPatch_InvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
	}{
		{name: "invalid status enum", payload: map[string]any{"status": "bogus"}},
		{name: "invalid confidence enum", payload: map[string]any{"confidence": "bogus"}},
		{name: "content wrong type", payload: map[string]any{"content": 42}},
		{name: "level wrong type", payload: map[string]any{"level": 42}},
		{name: "type wrong type", payload: map[string]any{"type": 42}},
		{name: "starred wrong type", payload: map[string]any{"starred": "not-a-bool"}},
		{name: "chat_id invalid uuid", payload: map[string]any{"chat_id": "not-a-uuid"}},
		{name: "chat_id wrong type", payload: map[string]any{"chat_id": 42}},
		{name: "pinned_personality_id invalid uuid", payload: map[string]any{"pinned_personality_id": "not-a-uuid"}},
		{name: "pinned_personality_id wrong type", payload: map[string]any{"pinned_personality_id": 42}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseBatchPatch(tc.payload); err == nil {
				t.Fatalf("expected error for payload %#v", tc.payload)
			}
		})
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

func TestPatchMemoriesBatchUnauthorized(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body, _ := json.Marshal(batchPatchRequest{IDs: []string{uuid.New().String()}, Patch: map[string]any{"starred": true}})
	req := httptest.NewRequest(http.MethodPost, "/memory/batch/patch", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

// requestWithUserBody builds an authenticated POST request with a JSON (or raw
// string) body against the given path, for exercising validation that runs
// before any datastore access — so h.ds can stay nil.
func requestWithUserBody(t *testing.T, method, path string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	return req.WithContext(ctx)
}

func TestDeleteMemoriesBatchHandler_InvalidJSONBody(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}
	req := requestWithUserBody(t, http.MethodPost, "/memory/batch/delete", []byte("not json"))
	rr := httptest.NewRecorder()
	h.DeleteMemoriesBatch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteMemoriesBatchHandler_InvalidIDs(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}

	tests := []struct {
		name string
		req  batchDeleteRequest
	}{
		{name: "empty ids", req: batchDeleteRequest{IDs: nil}},
		{name: "non-uuid id", req: batchDeleteRequest{IDs: []string{"not-a-uuid"}}},
		{name: "too many ids", req: batchDeleteRequest{IDs: tooManyIDs()}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, _ := json.Marshal(tc.req)
			req := requestWithUserBody(t, http.MethodPost, "/memory/batch/delete", body)
			rr := httptest.NewRecorder()
			h.DeleteMemoriesBatch(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestPatchMemoriesBatchHandler_InvalidJSONBody(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}
	req := requestWithUserBody(t, http.MethodPost, "/memory/batch/patch", []byte("not json"))
	rr := httptest.NewRecorder()
	h.PatchMemoriesBatch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPatchMemoriesBatchHandler_InvalidIDs(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}

	tests := []struct {
		name string
		req  batchPatchRequest
	}{
		{name: "empty ids", req: batchPatchRequest{IDs: nil, Patch: map[string]any{"starred": true}}},
		{name: "non-uuid id", req: batchPatchRequest{IDs: []string{"not-a-uuid"}, Patch: map[string]any{"starred": true}}},
		{name: "too many ids", req: batchPatchRequest{IDs: tooManyIDs(), Patch: map[string]any{"starred": true}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, _ := json.Marshal(tc.req)
			req := requestWithUserBody(t, http.MethodPost, "/memory/batch/patch", body)
			rr := httptest.NewRecorder()
			h.PatchMemoriesBatch(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestPatchMemoriesBatchHandler_InvalidPatch(t *testing.T) {
	t.Parallel()

	h := &Handler{logger: zap.NewNop()}

	tests := []struct {
		name  string
		patch map[string]any
	}{
		{name: "nil patch", patch: nil},
		{name: "empty patch", patch: map[string]any{}},
		{name: "invalid status", patch: map[string]any{"status": "bogus"}},
		{name: "invalid confidence", patch: map[string]any{"confidence": "bogus"}},
		{name: "content wrong type", patch: map[string]any{"content": 42}},
		{name: "chat_id not a uuid", patch: map[string]any{"chat_id": "not-a-uuid"}},
		{name: "pinned_personality_id not a uuid", patch: map[string]any{"pinned_personality_id": "not-a-uuid"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, _ := json.Marshal(batchPatchRequest{IDs: []string{uuid.New().String()}, Patch: tc.patch})
			req := requestWithUserBody(t, http.MethodPost, "/memory/batch/patch", body)
			rr := httptest.NewRecorder()
			h.PatchMemoriesBatch(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func tooManyIDs() []string {
	ids := make([]string, models.MaxMemoryBatchIDs+1)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	return ids
}
