package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeBranchAgent struct {
	mu      sync.Mutex
	chatIDs []uuid.UUID
}

func (f *fakeBranchAgent) EnqueueBranchRehydration(_ context.Context, _, chatID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatIDs = append(f.chatIDs, chatID)
}

func serveChatRoute(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestForkChat_CreatesBranchWithDefaults(t *testing.T) {
	t.Parallel()
	parentID, messageID, branchID := uuid.New(), uuid.New(), uuid.New()

	var got models.ForkChatParams
	store := &fakeStore{
		forkChatFn: func(_ context.Context, _ uuid.UUID, params models.ForkChatParams) (*models.ForkChatResult, error) {
			got = params
			return &models.ForkChatResult{Chat: &models.Chat{ID: branchID, Name: "What if: Plans"}, CopiedMessages: 4}, nil
		},
	}
	agent := &fakeBranchAgent{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.branchAgent = agent

	rec := serveChatRoute(h, http.MethodPost, "/chat/"+parentID.String()+"/fork", `{"message_id":"`+messageID.String()+`"}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, parentID, got.ParentChatID)
	require.Equal(t, messageID, got.MessageID)
	require.True(t, got.IncludeMessage, "include_message defaults to true")
	require.Empty(t, got.Name)

	var resp forkChatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, branchID, resp.Chat.ID)
	require.Equal(t, 4, resp.CopiedMessages)
	require.Empty(t, agent.chatIDs, "no rehydration when the parent checkpoint is reusable")
}

func TestForkChat_ExcludeMessageAndNameAndRehydration(t *testing.T) {
	t.Parallel()
	branchID := uuid.New()
	var got models.ForkChatParams
	store := &fakeStore{
		forkChatFn: func(_ context.Context, _ uuid.UUID, params models.ForkChatParams) (*models.ForkChatResult, error) {
			got = params
			return &models.ForkChatResult{Chat: &models.Chat{ID: branchID}, NeedsRehydration: true}, nil
		},
	}
	agent := &fakeBranchAgent{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	h.branchAgent = agent

	body := `{"message_id":"` + uuid.NewString() + `","include_message":false,"name":"  Plan B  "}`
	rec := serveChatRoute(h, http.MethodPost, "/chat/"+uuid.NewString()+"/fork", body)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.False(t, got.IncludeMessage)
	require.Equal(t, "Plan B", got.Name)
	require.Equal(t, []uuid.UUID{branchID}, agent.chatIDs)
}

func TestForkChat_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    string
		body    string
		forkErr error
		want    int
	}{
		{"bad chat id", "/chat/nope/fork", `{"message_id":"` + uuid.NewString() + `"}`, nil, http.StatusBadRequest},
		{"bad body", "/chat/" + uuid.NewString() + "/fork", `{`, nil, http.StatusBadRequest},
		{"bad message id", "/chat/" + uuid.NewString() + "/fork", `{"message_id":"x"}`, nil, http.StatusBadRequest},
		{"name too long", "/chat/" + uuid.NewString() + "/fork", `{"message_id":"` + uuid.NewString() + `","name":"` + strings.Repeat("a", 201) + `"}`, nil, http.StatusBadRequest},
		{"chat missing", "/chat/" + uuid.NewString() + "/fork", `{"message_id":"` + uuid.NewString() + `"}`, datastore.ErrChatNotFound, http.StatusNotFound},
		{"message missing", "/chat/" + uuid.NewString() + "/fork", `{"message_id":"` + uuid.NewString() + `"}`, datastore.ErrForkMessageNotFound, http.StatusNotFound},
		{"db error", "/chat/" + uuid.NewString() + "/fork", `{"message_id":"` + uuid.NewString() + `"}`, errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{
				forkChatFn: func(context.Context, uuid.UUID, models.ForkChatParams) (*models.ForkChatResult, error) {
					if tc.forkErr == nil {
						t.Fatal("ForkChat must not be called on invalid input")
					}
					return nil, tc.forkErr
				},
			}
			h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
			rec := serveChatRoute(h, http.MethodPost, tc.path, tc.body)
			require.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestGetChatLineage_BranchWithParentAndChildren(t *testing.T) {
	t.Parallel()
	chatID, parentID, pivotID, childID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: chatID, ForkedFromChatID: &parentID, ForkedFromMessageID: &pivotID}, nil
		},
		getChatNameFn: func(_ context.Context, _, id uuid.UUID) (string, error) {
			require.Equal(t, parentID, id)
			return "Original plans", nil
		},
		listChatBranchesFn: func(_ context.Context, _, id uuid.UUID) ([]models.ChatBranchSummary, error) {
			require.Equal(t, chatID, id)
			return []models.ChatBranchSummary{{ID: childID, Name: "What if: deeper"}}, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	rec := serveChatRoute(h, http.MethodGet, "/chat/"+chatID.String()+"/lineage", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got models.ChatLineage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.Parent)
	require.Equal(t, parentID, got.Parent.ID)
	require.Equal(t, pivotID, *got.Parent.MessageID)
	require.Equal(t, "Original plans", got.Parent.Name)
	require.False(t, got.Parent.Deleted)
	require.Len(t, got.Branches, 1)
	require.Equal(t, childID, got.Branches[0].ID)
}

func TestGetChatLineage_DeletedParentAndNoBranches(t *testing.T) {
	t.Parallel()
	parentID := uuid.New()
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: uuid.New(), ForkedFromChatID: &parentID}, nil
		},
		getChatNameFn: func(context.Context, uuid.UUID, uuid.UUID) (string, error) {
			return "", datastore.ErrChatNotFound
		},
		listChatBranchesFn: func(context.Context, uuid.UUID, uuid.UUID) ([]models.ChatBranchSummary, error) {
			return nil, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	rec := serveChatRoute(h, http.MethodGet, "/chat/"+uuid.NewString()+"/lineage", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"branches":[]`, "branches is always an array")
	var got models.ChatLineage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.True(t, got.Parent.Deleted)
	require.Empty(t, got.Parent.Name)
}

func TestGetChatLineage_RootThreadHasNullParent(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: uuid.New()}, nil
		},
		listChatBranchesFn: func(context.Context, uuid.UUID, uuid.UUID) ([]models.ChatBranchSummary, error) {
			return nil, nil
		},
	}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	rec := serveChatRoute(h, http.MethodGet, "/chat/"+uuid.NewString()+"/lineage", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"parent":null`)
}

func TestGetChatLineage_ChatNotFound(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
			return nil, datastore.ErrChatNotFound
		},
	}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	rec := serveChatRoute(h, http.MethodGet, "/chat/"+uuid.NewString()+"/lineage", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}
