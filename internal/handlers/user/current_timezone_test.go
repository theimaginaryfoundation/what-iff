package user

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

func TestSetCurrentTimezone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantCode int
		wantTZ   string
	}{
		{name: "stores a valid timezone", body: `{"timezone":"America/Los_Angeles"}`, wantCode: http.StatusNoContent, wantTZ: "America/Los_Angeles"},
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
