package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func contextFilesRequest(t *testing.T, h *Handler, chatID string, userID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/chat/"+chatID+"/context-files", nil)
	if userID != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, *userID))
	}
	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)
	return rr
}

func TestListChatContextFiles_Statuses(t *testing.T) {
	userID, chatID := uuid.New(), uuid.New()
	store := &fakeStore{getChatFn: func(_ context.Context, u, id uuid.UUID) (*models.Chat, error) {
		if u != userID || id != chatID {
			return nil, datastore.ErrChatNotFound
		}
		return &models.Chat{ID: id, UserID: u}, nil
	}}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})

	require.Equal(t, http.StatusUnauthorized, contextFilesRequest(t, h, chatID.String(), nil).Code)
	require.Equal(t, http.StatusBadRequest, contextFilesRequest(t, h, "nope", &userID).Code)
	require.Equal(t, http.StatusNotFound, contextFilesRequest(t, h, uuid.New().String(), &userID).Code)

	rr := contextFilesRequest(t, h, chatID.String(), &userID)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body ContextFilesResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.NotNil(t, body.Files, "an empty list, never null")
	require.Empty(t, body.Files)
	require.Nil(t, body.Container)

	store.getChatFn = func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) { return nil, errors.New("db down") }
	require.Equal(t, http.StatusInternalServerError, contextFilesRequest(t, h, chatID.String(), &userID).Code)
}
