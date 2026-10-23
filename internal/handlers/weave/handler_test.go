package weave

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeStore struct {
	Store
	created []uuid.UUID
	err     error
}

func (f *fakeStore) CreateWeave(_ context.Context, _ uuid.UUID, name string, ids []uuid.UUID) (*models.Weave, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.created = ids
	w := &models.Weave{ID: uuid.New(), Name: name}
	for _, id := range ids {
		w.Chats = append(w.Chats, models.ChatRef{ID: id})
	}
	return w, nil
}

func (f *fakeStore) DeleteWeave(context.Context, uuid.UUID, uuid.UUID) error { return f.err }

func (f *fakeStore) RemoveChatFromWeave(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.Weave, error) {
	return nil, f.err
}

func serve(t *testing.T, store Store, method, path string, body any) (int, map[string]any) {
	t.Helper()
	r := mux.NewRouter()
	NewHandler(store, zap.NewNop()).RegisterRoutes(r)
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestCreateWeave(t *testing.T) {
	store := &fakeStore{}
	a, b := uuid.New(), uuid.New()
	code, out := serve(t, store, http.MethodPost, "/weaves", map[string]any{"name": "rooms", "chat_ids": []uuid.UUID{a, b}})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "rooms", out["name"])
	assert.Equal(t, []uuid.UUID{a, b}, store.created)
}

func TestWeaveErrorsMapToStatuses(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		msg  string
	}{
		{fmt.Errorf("%w: %q is not sandboxed (only sandboxed threads can be woven)", datastore.ErrWeaveChat, "Everyday"), http.StatusBadRequest, `"Everyday" is not sandboxed (only sandboxed threads can be woven)`},
		{datastore.ErrChatNotFound, http.StatusNotFound, "Not found"},
		{datastore.ErrWeaveNotFound, http.StatusNotFound, "Not found"},
	} {
		code, out := serve(t, &fakeStore{err: tc.err}, http.MethodPost, "/weaves", map[string]any{"name": "x", "chat_ids": []uuid.UUID{uuid.New(), uuid.New()}})
		assert.Equal(t, tc.code, code)
		assert.Equal(t, tc.msg, out["message"])
	}
	code, _ := serve(t, &fakeStore{err: datastore.ErrWeaveNotFound}, http.MethodDelete, "/weaves/"+uuid.NewString(), nil)
	assert.Equal(t, http.StatusNotFound, code)
	code, _ = serve(t, &fakeStore{}, http.MethodDelete, "/weaves/"+uuid.NewString()+"/chats/not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, code)
}
