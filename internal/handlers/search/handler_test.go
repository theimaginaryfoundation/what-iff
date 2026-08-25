package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// fakeStore is a per-section stub for handler tests. Each section's behaviour
// can be overridden by setting the corresponding fn; unset sections return
// empty PaginatedResponse, mirroring "user has nothing in that resource".
type fakeStore struct {
	listChats               func(filters models.ChatFilters) (*models.PaginatedResponse, error)
	listPersonalities       func(filters models.PersonalityFilters) (*models.PaginatedResponse, error)
	listRituals             func(filters models.RitualFilters) (*models.PaginatedResponse, error)
	listMemories            func(filters models.MemoryFilters) (*models.PaginatedResponse, error)
	listFileAttachments     func(filters models.FileAttachmentFilters) (*models.PaginatedResponse, error)
	latestMessagesForChats  func(chatIDs []uuid.UUID) (map[uuid.UUID]string, error)
	listChatsCalls          int
	listFileAttachmentCalls int
}

func (f *fakeStore) ListChats(_ context.Context, _ uuid.UUID, _, _ int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
	f.listChatsCalls++
	if f.listChats != nil {
		return f.listChats(filters)
	}
	return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
}

func (f *fakeStore) ListPersonalities(_ context.Context, _ uuid.UUID, _, _ int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
	if f.listPersonalities != nil {
		return f.listPersonalities(filters)
	}
	return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
}

func (f *fakeStore) ListRituals(_ context.Context, _ uuid.UUID, _, _ int, filters models.RitualFilters) (*models.PaginatedResponse, error) {
	if f.listRituals != nil {
		return f.listRituals(filters)
	}
	return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
}

func (f *fakeStore) ListMemories(_ context.Context, _ uuid.UUID, _, _ int, filters models.MemoryFilters) (*models.PaginatedResponse, error) {
	if f.listMemories != nil {
		return f.listMemories(filters)
	}
	return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
}

func (f *fakeStore) ListFileAttachments(_ context.Context, _ uuid.UUID, _, _ int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	f.listFileAttachmentCalls++
	if f.listFileAttachments != nil {
		return f.listFileAttachments(filters)
	}
	return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
}

func (f *fakeStore) GetLatestMessagesForChats(_ context.Context, _ uuid.UUID, chatIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	if f.latestMessagesForChats != nil {
		return f.latestMessagesForChats(chatIDs)
	}
	return map[uuid.UUID]string{}, nil
}

func newRequest(t *testing.T, target string) (*http.Request, uuid.UUID) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	uid := uuid.New()
	return req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uid)), uid
}

func newRouter(store Store) *mux.Router {
	r := mux.NewRouter()
	NewHandler(store, zap.NewNop()).RegisterRoutes(r)
	return r
}

func TestSearch_RejectsMissingAuth(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	r := newRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/search?query=atlas", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Zero(t, store.listChatsCalls, "no datastore calls on auth failure")
}

func TestSearch_RejectsBadQueryParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "empty", path: "/search"},
		{name: "single char", path: "/search?query=a"},
		{name: "unknown type", path: "/search?query=atlas&types=mystery"},
		{name: "limit out of range", path: "/search?query=atlas&limit_per_type=99"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := newRequest(t, tc.path)
			rr := httptest.NewRecorder()
			newRouter(&fakeStore{}).ServeHTTP(rr, req)
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		})
	}
}

func TestSearch_AggregatesAcrossSections(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	personalityID := uuid.New()
	ritualID := uuid.New()
	memoryID := uuid.New()
	imageID := uuid.New()
	now := time.Now()

	store := &fakeStore{
		listChats: func(filters models.ChatFilters) (*models.PaginatedResponse, error) {
			require.NotNil(t, filters.Query)
			require.Equal(t, "atlas", *filters.Query)
			return &models.PaginatedResponse{Results: []any{
				&models.Chat{ID: chatID, Name: "Atlas roadmap", Tags: []string{"planning"}, UpdatedAt: now},
				&models.Chat{ID: uuid.New(), Name: "Unrelated chat", UpdatedAt: now},
			}, Page: 1}, nil
		},
		latestMessagesForChats: func(ids []uuid.UUID) (map[uuid.UUID]string, error) {
			require.Len(t, ids, 2)
			return map[uuid.UUID]string{chatID: "Latest message body about atlas"}, nil
		},
		listPersonalities: func(filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
			require.NotNil(t, filters.Query)
			require.Equal(t, "atlas", *filters.Query)
			return &models.PaginatedResponse{Results: []any{
				&models.Personality{ID: personalityID, Name: "Atlas Mentor", SystemPrompt: "You are Atlas, a helpful assistant.", UpdatedAt: now},
			}, Page: 1}, nil
		},
		listRituals: func(filters models.RitualFilters) (*models.PaginatedResponse, error) {
			require.NotNil(t, filters.Query)
			return &models.PaginatedResponse{Results: []any{
				&models.Ritual{ID: ritualID, Name: "Atlas standup", Description: "Run the Atlas standup", Content: "..."},
			}, Page: 1}, nil
		},
		listMemories: func(filters models.MemoryFilters) (*models.PaginatedResponse, error) {
			require.NotNil(t, filters.Query)
			return &models.PaginatedResponse{Results: []any{
				&models.Memory{ID: memoryID, Content: "Notes about atlas project goals", Scope: "User", CreatedAt: now},
			}, Page: 1}, nil
		},
		listFileAttachments: func(filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
			require.NotNil(t, filters.Name)
			require.NotNil(t, filters.FileType)
			require.Equal(t, models.ImageMIMEPrefix, *filters.FileType)
			return &models.PaginatedResponse{Results: []any{
				&models.FileAttachment{ID: imageID, Name: "atlas-cover.png", FileType: "image/png", CreatedAt: now},
			}, Page: 1}, nil
		},
	}

	req, _ := newRequest(t, "/search?query=atlas")
	rr := httptest.NewRecorder()
	newRouter(store).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var resp SearchResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, "atlas", resp.Query)
	require.Len(t, resp.Sections, len(AllTypes))

	bySectionType := map[string]SearchSection{}
	for _, s := range resp.Sections {
		bySectionType[s.Type] = s
	}

	chatSection := bySectionType[TypeChat]
	require.Len(t, chatSection.Results, 1, "non-matching chats are filtered by score")
	require.Equal(t, "Atlas roadmap", chatSection.Results[0].Label)
	require.Equal(t, "/chat/"+chatID.String(), chatSection.Results[0].Route)
	require.Equal(t, TypeChat, chatSection.Results[0].IconType)
	require.NotEmpty(t, chatSection.Results[0].Snippet, "chat hits enrich with the latest message body")

	require.Len(t, bySectionType[TypePersonality].Results, 1)
	require.Equal(t, "/personality/"+personalityID.String(), bySectionType[TypePersonality].Results[0].Route)

	require.Len(t, bySectionType[TypeRitual].Results, 1)
	require.Equal(t, "/ritual/"+ritualID.String(), bySectionType[TypeRitual].Results[0].Route)

	require.Len(t, bySectionType[TypeMemory].Results, 1)
	require.Equal(t, "/memory/"+memoryID.String(), bySectionType[TypeMemory].Results[0].Route)

	require.Len(t, bySectionType[TypeImage].Results, 1)
	require.Equal(t, "/image-gallery", bySectionType[TypeImage].Results[0].Route)
}

func TestSearch_TypesFilterRestrictsSections(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	store := &fakeStore{
		listChats: func(_ models.ChatFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{
				&models.Chat{ID: chatID, Name: "Atlas roadmap"},
			}, Page: 1}, nil
		},
		listFileAttachments: func(_ models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
			t.Fatalf("ListFileAttachments must not be called when types=chat")
			return nil, nil
		},
	}

	q := url.Values{}
	q.Set("query", "atlas")
	q.Set("types", "chat")
	req, _ := newRequest(t, "/search?"+q.Encode())
	rr := httptest.NewRecorder()
	newRouter(store).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp SearchResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Sections, 1)
	require.Equal(t, TypeChat, resp.Sections[0].Type)
	require.Zero(t, store.listFileAttachmentCalls, "non-requested sections do not run")
}

func TestSearch_PerSectionErrorReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		listChats: func(_ models.ChatFilters) (*models.PaginatedResponse, error) {
			return nil, errors.New("simulated chat outage")
		},
		listMemories: func(_ models.MemoryFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{
				&models.Memory{ID: uuid.New(), Content: "atlas matched memory", Scope: "User"},
			}, Page: 1}, nil
		},
	}

	req, _ := newRequest(t, "/search?query=atlas")
	rr := httptest.NewRecorder()
	newRouter(store).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var resp SearchResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	bySectionType := map[string]SearchSection{}
	for _, s := range resp.Sections {
		bySectionType[s.Type] = s
	}

	require.NotNil(t, bySectionType[TypeChat].Results, "broken section still returns an empty slice, not nil")
	require.Empty(t, bySectionType[TypeChat].Results)
	require.Len(t, bySectionType[TypeMemory].Results, 1, "other sections still serve hits")
}

func TestSearch_LimitPerTypeForwardedToDatastore(t *testing.T) {
	t.Parallel()

	gotLimit := 0
	store := &fakeStore{
		listChats: func(_ models.ChatFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
		},
		listPersonalities: func(_ models.PersonalityFilters) (*models.PaginatedResponse, error) {
			return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
		},
	}
	store.listChats = func(_ models.ChatFilters) (*models.PaginatedResponse, error) {
		// We capture limit via the page we return; the simpler approach below
		// uses a function var to record it.
		return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
	}

	// Use a separate hook that records pageSize via the store wrapper.
	wrapped := &recordingStore{Store: store, recordLimit: func(n int) { gotLimit = n }}
	r := mux.NewRouter()
	NewHandler(wrapped, zap.NewNop()).RegisterRoutes(r)

	req, _ := newRequest(t, "/search?query=atlas&limit_per_type=7")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, 7, gotLimit)
}

// recordingStore wraps a Store and notes the pageSize passed to ListChats so
// the limit-forwarding test can assert it without mutating the fakeStore.
type recordingStore struct {
	Store
	recordLimit func(int)
}

func (r *recordingStore) ListChats(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
	if r.recordLimit != nil {
		r.recordLimit(pageSize)
	}
	return r.Store.ListChats(ctx, userID, pageNum, pageSize, filters)
}
