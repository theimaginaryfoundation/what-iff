package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func sensitivityRouter(store Store) *mux.Router {
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router
}

func sendChat(router *mux.Router, userID uuid.UUID, method, path, body string, vars map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestPatchChat_MemorySensitivityLimit(t *testing.T) {
	t.Parallel()
	userID, chatID := uuid.New(), uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Thread", MemorySensitivityLimit: models.MemorySensitivitySensitive}
	var got models.Chat
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) { c := *existing; return &c, nil },
		updateChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
			got = chat
			return &chat, nil
		},
	}
	router := sensitivityRouter(store)
	path, vars := "/chat/"+chatID.String(), map[string]string{"id": chatID.String()}

	w := sendChat(router, userID, http.MethodPatch, path, `{"memory_sensitivity_limit":"public"}`, vars)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivityPublic, got.MemorySensitivityLimit, "the patch sets the limit")
	require.True(t, got.SetMemorySensitivityLimit, "and marks it as an explicit change")
	require.Contains(t, w.Body.String(), `"memory_sensitivity_limit":"public"`)

	// Normalized like every other level.
	w = sendChat(router, userID, http.MethodPatch, path, `{"memory_sensitivity_limit":" Personal "}`, vars)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivityPersonal, got.MemorySensitivityLimit)

	// A patch that omits it keeps what the chat already had.
	w = sendChat(router, userID, http.MethodPatch, path, `{"name":"Renamed"}`, vars)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivitySensitive, got.MemorySensitivityLimit, "PATCH semantics: untouched fields pass through from the loaded chat")
	require.False(t, got.SetMemorySensitivityLimit, "an unrelated patch is not a limit change")

	// Invalid values are a 400 and nothing is written.
	got = models.Chat{}
	w = sendChat(router, userID, http.MethodPatch, path, `{"memory_sensitivity_limit":"top-secret"}`, vars)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = sendChat(router, userID, http.MethodPatch, path, `{"memory_sensitivity_limit":""}`, vars)
	require.Equal(t, http.StatusBadRequest, w.Code, "an explicit empty value is not 'unrestricted'")
	require.Equal(t, models.Chat{}, got)
}

func TestUpdateChat_MemorySensitivityLimit(t *testing.T) {
	t.Parallel()
	userID, chatID := uuid.New(), uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Thread", MemorySensitivityLimit: models.MemorySensitivityPersonal}
	var got models.Chat
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) { c := *existing; return &c, nil },
		updateChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
			got = chat
			return &chat, nil
		},
	}
	router := sensitivityRouter(store)
	path, vars := "/chat/"+chatID.String(), map[string]string{"id": chatID.String()}

	w := sendChat(router, userID, http.MethodPut, path, `{"name":"Thread","memory_sensitivity_limit":"public"}`, vars)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivityPublic, got.MemorySensitivityLimit)

	w = sendChat(router, userID, http.MethodPut, path, `{"name":"Thread"}`, vars)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivityPersonal, got.MemorySensitivityLimit, "PUT without the field does not reset the limit")
	require.False(t, got.SetMemorySensitivityLimit)

	w = sendChat(router, userID, http.MethodPut, path, `{"name":"Thread","memory_sensitivity_limit":"bogus"}`, vars)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateChat_MemorySensitivityLimit(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	var captured models.Chat
	store := &createChatStore{createChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
		captured = chat
		out := chat
		out.ID = uuid.New()
		if out.MemorySensitivityLimit == "" {
			out.MemorySensitivityLimit = models.MemorySensitivitySensitive // what the datastore's default yields
		}
		return &out, nil
	}}
	router := mux.NewRouter()
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router.HandleFunc("/chat", h.CreateChat).Methods(http.MethodPost)

	w := sendChat(router, userID, http.MethodPost, "/chat", `{"name":"Discord thread","memory_sensitivity_limit":"public"}`, nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivityPublic, captured.MemorySensitivityLimit)
	require.Contains(t, w.Body.String(), `"memory_sensitivity_limit":"public"`)

	captured = models.Chat{}
	w = sendChat(router, userID, http.MethodPost, "/chat", `{"name":"Plain"}`, nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, models.MemorySensitivity(""), captured.MemorySensitivityLimit, "omitted: the datastore default (unrestricted) applies")
	require.Contains(t, w.Body.String(), `"memory_sensitivity_limit":"sensitive"`)

	w = sendChat(router, userID, http.MethodPost, "/chat", `{"name":"Bad","memory_sensitivity_limit":"secret"}`, nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
