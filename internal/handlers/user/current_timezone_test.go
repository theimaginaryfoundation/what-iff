package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestSetCurrentTimezone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantCode int
		wantTZ   string
	}{
		{name: "stores a valid timezone", body: `{"timezone":"America/Los_Angeles"}`, wantCode: http.StatusOK, wantTZ: "America/Los_Angeles"},
		{name: "rejects an unknown timezone", body: `{"timezone":"Mars/Olympus_Mons"}`, wantCode: http.StatusBadRequest},
		{name: "rejects an empty timezone", body: `{"timezone":"  "}`, wantCode: http.StatusBadRequest},
		{name: "rejects a bad body", body: `{`, wantCode: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockUserStore{}
			h := NewHandler(store, zap.NewNop(), nil, "test")

			req := httptest.NewRequest(http.MethodPut, "/user/current-timezone", strings.NewReader(tc.body))
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
			w := httptest.NewRecorder()
			h.SetCurrentTimezone(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantCode, w.Body.String())
			}
			if store.currentTimezone != tc.wantTZ {
				t.Fatalf("stored timezone = %q, want %q", store.currentTimezone, tc.wantTZ)
			}
		})
	}
}

func TestSetCurrentTimezone_RequiresAUser(t *testing.T) {
	h := NewHandler(&mockUserStore{}, zap.NewNop(), nil, "test")
	req := httptest.NewRequest(http.MethodPut, "/user/current-timezone", strings.NewReader(`{"timezone":"UTC"}`))
	w := httptest.NewRecorder()
	h.SetCurrentTimezone(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestSetCurrentTimezone_SaysWhenItPausedFollowing(t *testing.T) {
	store := &mockUserStore{pauseOnReport: true}
	h := NewHandler(store, zap.NewNop(), nil, "test")
	req := httptest.NewRequest(http.MethodPut, "/user/current-timezone", strings.NewReader(`{"timezone":"America/New_York"}`))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	h.SetCurrentTimezone(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var report models.CurrentTimezoneReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if !report.PausedNow || report.FollowingPausedAt == nil {
		t.Fatalf("report = %+v, want paused now", report)
	}
}

func TestResumeTimezoneFollowing(t *testing.T) {
	store := &mockUserStore{}
	h := NewHandler(store, zap.NewNop(), nil, "test")
	req := httptest.NewRequest(http.MethodPost, "/user/current-timezone/resume", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	h.ResumeTimezoneFollowing(w, req)
	if w.Code != http.StatusNoContent || !store.resumed {
		t.Fatalf("status = %d, resumed = %v; want 204 and resumed", w.Code, store.resumed)
	}
}
