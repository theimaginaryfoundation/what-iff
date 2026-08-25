package ritual

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeBindingStore implements bindingStore for tests.
type fakeBindingStore struct {
	getBindingsFn   func(ctx context.Context, userID uuid.UUID) ([]*models.SystemRitualBinding, error)
	upsertBindingFn func(ctx context.Context, userID, ritualID uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error)
	deleteBindingFn func(ctx context.Context, userID, ritualID uuid.UUID) error
}

func (f *fakeBindingStore) GetSystemBindingsForUser(ctx context.Context, userID uuid.UUID) ([]*models.SystemRitualBinding, error) {
	if f.getBindingsFn != nil {
		return f.getBindingsFn(ctx, userID)
	}
	return nil, nil
}

func (f *fakeBindingStore) UpsertSystemBinding(ctx context.Context, userID, ritualID uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error) {
	if f.upsertBindingFn != nil {
		return f.upsertBindingFn(ctx, userID, ritualID, hotkeys)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeBindingStore) DeleteSystemBinding(ctx context.Context, userID, ritualID uuid.UUID) error {
	if f.deleteBindingFn != nil {
		return f.deleteBindingFn(ctx, userID, ritualID)
	}
	return errors.New("not implemented")
}

func TestUpsertBinding_Unauthorized(t *testing.T) {
	t.Parallel()

	h := &Handler{ds: nil, binding: &fakeBindingStore{}, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", bytes.NewReader([]byte(`{"hotkeys":"Ctrl+A"}`)))
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestUpsertBinding_InvalidRitualID(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	h := &Handler{ds: nil, binding: &fakeBindingStore{}, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/invalid-id/binding", bytes.NewReader([]byte(`{"hotkeys":"Ctrl+A"}`)))
	req = mux.SetURLVars(req, map[string]string{"id": "invalid-id"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestUpsertBinding_NonSystemRitual(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	nonSystemID := uuid.New()
	h := &Handler{ds: nil, binding: &fakeBindingStore{}, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/"+nonSystemID.String()+"/binding", bytes.NewReader([]byte(`{"hotkeys":"Ctrl+A"}`)))
	req = mux.SetURLVars(req, map[string]string{"id": nonSystemID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestUpsertBinding_InvalidHotkeyFormat(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	h := &Handler{ds: nil, binding: &fakeBindingStore{}, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", bytes.NewReader([]byte(`{"hotkeys":"Shift+A"}`)))
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid hotkey (Shift+A), got %d", rr.Code)
	}
}

func TestUpsertBinding_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wantBinding := &models.SystemRitualBinding{
		ID:       uuid.New(),
		UserID:   userID,
		RitualID: agent.SystemRitualIDImageGenerate,
		Hotkeys:  "Ctrl+A",
	}

	store := &fakeBindingStore{
		upsertBindingFn: func(ctx context.Context, uid, rid uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error) {
			if uid != userID || rid != agent.SystemRitualIDImageGenerate || hotkeys != "Ctrl+A" {
				t.Errorf("unexpected args: uid=%s rid=%s hotkeys=%q", uid, rid, hotkeys)
			}
			return wantBinding, nil
		},
	}

	h := &Handler{ds: nil, binding: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", bytes.NewReader([]byte(`{"hotkeys":"Ctrl+A"}`)))
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var got models.SystemRitualBinding
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Hotkeys != wantBinding.Hotkeys {
		t.Errorf("hotkeys: got %q, want %q", got.Hotkeys, wantBinding.Hotkeys)
	}
}

func TestUpsertBinding_HotkeyConflict(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeBindingStore{
		upsertBindingFn: func(ctx context.Context, uid, rid uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error) {
			return nil, datastore.ErrHotkeyConflict
		},
	}

	h := &Handler{ds: nil, binding: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", bytes.NewReader([]byte(`{"hotkeys":"Ctrl+A"}`)))
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 for hotkey conflict, got %d", rr.Code)
	}
}

func TestUpsertBinding_EmptyHotkeysDeletes(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	deleted := false
	store := &fakeBindingStore{
		deleteBindingFn: func(ctx context.Context, uid, rid uuid.UUID) error {
			if uid == userID && rid == agent.SystemRitualIDImageGenerate {
				deleted = true
			}
			return nil
		},
	}

	h := &Handler{ds: nil, binding: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", bytes.NewReader([]byte(`{"hotkeys":""}`)))
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if !deleted {
		t.Error("expected DeleteSystemBinding to be called")
	}
}

func TestDeleteBinding_Unauthorized(t *testing.T) {
	t.Parallel()

	h := &Handler{ds: nil, binding: &fakeBindingStore{}, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodDelete, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", nil)
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestDeleteBinding_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	deleted := false
	store := &fakeBindingStore{
		deleteBindingFn: func(ctx context.Context, uid, rid uuid.UUID) error {
			if uid == userID && rid == agent.SystemRitualIDImageGenerate {
				deleted = true
			}
			return nil
		},
	}

	h := &Handler{ds: nil, binding: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodDelete, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", nil)
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if !deleted {
		t.Error("expected DeleteSystemBinding to be called")
	}
}

func TestGetBinding_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	wantBinding := &models.SystemRitualBinding{
		ID:       uuid.New(),
		UserID:   userID,
		RitualID: agent.SystemRitualIDImageGenerate,
		Hotkeys:  "Ctrl+B",
	}

	store := &fakeBindingStore{
		getBindingsFn: func(ctx context.Context, uid uuid.UUID) ([]*models.SystemRitualBinding, error) {
			if uid != userID {
				return nil, nil
			}
			return []*models.SystemRitualBinding{wantBinding}, nil
		},
	}

	h := &Handler{ds: nil, binding: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", nil)
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var got models.SystemRitualBinding
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Hotkeys != wantBinding.Hotkeys {
		t.Errorf("hotkeys: got %q, want %q", got.Hotkeys, wantBinding.Hotkeys)
	}
}

func TestGetBinding_NotFoundReturnsEmpty(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeBindingStore{
		getBindingsFn: func(ctx context.Context, uid uuid.UUID) ([]*models.SystemRitualBinding, error) {
			return nil, nil
		},
	}

	h := &Handler{ds: nil, binding: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/ritual/"+agent.SystemRitualIDImageGenerate.String()+"/binding", nil)
	req = mux.SetURLVars(req, map[string]string{"id": agent.SystemRitualIDImageGenerate.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var got map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty object when no binding, got %v", got)
	}
}
