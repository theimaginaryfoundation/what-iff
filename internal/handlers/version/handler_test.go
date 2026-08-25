package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/buildinfo"
)

func TestGet_ReturnsBuildInfo(t *testing.T) {
	handler := NewHandler(buildinfo.Info{
		Version: "v1.2.3",
		Commit:  "abc1234",
		BuiltAt: "2026-08-21T00:00:00Z",
	}, zap.NewNop())

	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()

	handler.Get(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	var response Response
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Version != "v1.2.3" {
		t.Errorf("Expected version 'v1.2.3', got '%s'", response.Version)
	}
	if response.Commit != "abc1234" {
		t.Errorf("Expected commit 'abc1234', got '%s'", response.Commit)
	}
	if response.BuiltAt != "2026-08-21T00:00:00Z" {
		t.Errorf("Expected built_at '2026-08-21T00:00:00Z', got '%s'", response.BuiltAt)
	}
}

// The open-source build has no overlay, so the field must be absent from the
// JSON entirely — not present as an empty string a consumer might mistake for
// a real (empty) revision.
func TestGet_OmitsEmptyOverlayCommit(t *testing.T) {
	handler := NewHandler(buildinfo.Info{
		Version: "dev",
		Commit:  "unknown",
		BuiltAt: "unknown",
	}, zap.NewNop())

	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()

	handler.Get(w, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if _, present := raw["overlay_commit"]; present {
		t.Error("Expected overlay_commit to be omitted when empty")
	}
}

func TestGet_IncludesOverlayCommitWhenSet(t *testing.T) {
	handler := NewHandler(buildinfo.Info{
		Version:       "v1.2.3",
		Commit:        "abc1234",
		BuiltAt:       "2026-08-21T00:00:00Z",
		OverlayCommit: "def5678",
	}, zap.NewNop())

	req := httptest.NewRequest("GET", "/api/version", nil)
	w := httptest.NewRecorder()

	handler.Get(w, req)

	var response Response
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.OverlayCommit != "def5678" {
		t.Errorf("Expected overlay_commit 'def5678', got '%s'", response.OverlayCommit)
	}
}
