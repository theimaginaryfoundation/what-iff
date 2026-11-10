package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// noticeStore answers the notice calls for one account's one chat.
type noticeStore struct {
	fakeStore
	owner, chat uuid.UUID
	notices     []models.BackgroundNotice
	dismissed   []uuid.UUID
}

func (s *noticeStore) ListBackgroundNotices(_ context.Context, userID, chatID uuid.UUID) ([]models.BackgroundNotice, error) {
	if userID != s.owner || chatID != s.chat {
		return []models.BackgroundNotice{}, nil
	}
	return s.notices, nil
}

func (s *noticeStore) DismissBackgroundNotice(_ context.Context, userID, chatID, id uuid.UUID) error {
	if userID != s.owner || chatID != s.chat {
		return datastore.ErrBackgroundNoticeNotFound
	}
	s.dismissed = append(s.dismissed, id)
	return nil
}

func serveNotices(t *testing.T, store *noticeStore, method, path string, user uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	h := &Handler{ds: store, logger: zap.NewNop()}
	router := mux.NewRouter()
	router.HandleFunc("/chat/{chatId}/background-notices", h.ListBackgroundNotices).Methods("GET")
	router.HandleFunc("/chat/{chatId}/background-notices/{noticeId}/dismiss", h.DismissBackgroundNotice).Methods("POST")
	req := httptest.NewRequest(method, path, nil)
	if user != uuid.Nil {
		req = req.WithContext(apicontext.WithUserID(req.Context(), user))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestBackgroundNoticeRoutes(t *testing.T) {
	t.Parallel()
	notice := models.BackgroundNotice{ID: uuid.New(), Step: models.BackgroundStepMemory, Code: models.BackgroundNoticeProviderNotConfigured, Provider: "Anthropic", Count: 2}
	store := &noticeStore{owner: uuid.New(), chat: uuid.New()}
	store.notices = []models.BackgroundNotice{notice}
	base := "/chat/" + store.chat.String() + "/background-notices"

	rec := serveNotices(t, store, http.MethodGet, base, store.owner)
	require.Equal(t, http.StatusOK, rec.Code)
	var got []models.BackgroundNotice
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []models.BackgroundNotice{notice}, got)

	rec = serveNotices(t, store, http.MethodGet, base, uuid.New())
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[]`, rec.Body.String(), "another account's chat lists nothing")

	rec = serveNotices(t, store, http.MethodPost, base+"/"+notice.ID.String()+"/dismiss", store.owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []uuid.UUID{notice.ID}, store.dismissed)

	rec = serveNotices(t, store, http.MethodPost, base+"/"+notice.ID.String()+"/dismiss", uuid.New())
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = serveNotices(t, store, http.MethodPost, base+"/not-an-id/dismiss", store.owner)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = serveNotices(t, store, http.MethodGet, base, uuid.Nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
