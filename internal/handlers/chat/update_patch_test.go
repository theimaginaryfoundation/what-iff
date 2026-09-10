package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func boolPtr(b bool) *bool { return &b }

type fakeStore struct {
	getChatFn              func(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error)
	getChatContextFn       func(ctx context.Context, userID, chatID uuid.UUID) (*models.ChatContext, error)
	updateScratchpadFn     func(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
	listChatsFn            func(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error)
	updateChatFn           func(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error)
	markChatMessagesReadFn func(ctx context.Context, userID, chatID uuid.UUID) (int, error)
	getModelByNameFn       func(ctx context.Context, name string) (*models.Model, error)
	isFirstChatFn          func(ctx context.Context, userID, chatID uuid.UUID) (bool, error)
	countAllMessagesFn     func(ctx context.Context, userID uuid.UUID, cap int) (int, error)
}

func (f *fakeStore) CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ListChats(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
	if f.listChatsFn != nil {
		return f.listChatsFn(ctx, userID, pageNum, pageSize, filters)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error) {
	return f.getChatFn(ctx, userID, id)
}
func (f *fakeStore) UpdateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
	return f.updateChatFn(ctx, userID, chat)
}
func (f *fakeStore) DeleteChat(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) ListChatMessages(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetChatMessage(ctx context.Context, userID, messageID uuid.UUID) (*models.ChatMessage, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) MarkChatMessagesRead(ctx context.Context, userID, chatID uuid.UUID) (int, error) {
	if f.markChatMessagesReadFn != nil {
		return f.markChatMessagesReadFn(ctx, userID, chatID)
	}
	return 0, errors.New("not implemented")
}
func (f *fakeStore) CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error {
	return errors.New("not implemented")
}
func (f *fakeStore) GetAvailableRituals(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.RitualFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetSystemBindingsForUser(ctx context.Context, userID uuid.UUID) ([]*models.SystemRitualBinding, error) {
	return nil, nil
}
func (f *fakeStore) ListChatMCPServers(ctx context.Context, userID, chatID uuid.UUID) ([]*models.MCPServer, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ListAvailableChatMCPServers(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.MCPServerFilters) (*models.PaginatedResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) AddMCPServerToChat(ctx context.Context, userID, chatID, mcpServerID uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) RemoveMCPServerFromChat(ctx context.Context, userID, chatID, mcpServerID uuid.UUID) error {
	return errors.New("not implemented")
}
func (f *fakeStore) ListDefaultEnabledMCPServers(ctx context.Context, userID uuid.UUID) ([]*models.MCPServer, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ExportChat(ctx context.Context, userID, chatID uuid.UUID, w io.Writer) error {
	return errors.New("not implemented")
}
func (f *fakeStore) GetModelByName(ctx context.Context, name string) (*models.Model, error) {
	if f.getModelByNameFn != nil {
		return f.getModelByNameFn(ctx, name)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) IsFirstChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
	if f.isFirstChatFn != nil {
		return f.isFirstChatFn(ctx, userID, chatID)
	}
	return false, errors.New("not implemented")
}
func (f *fakeStore) CountAllChatMessages(ctx context.Context, userID uuid.UUID, cap int) (int, error) {
	if f.countAllMessagesFn != nil {
		return f.countAllMessagesFn(ctx, userID, cap)
	}
	return 0, errors.New("not implemented")
}
func (f *fakeStore) GetChatContext(ctx context.Context, userID, chatID uuid.UUID) (*models.ChatContext, error) {
	if f.getChatContextFn != nil {
		return f.getChatContextFn(ctx, userID, chatID)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) UpdatePersonalityScratchpad(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error) {
	if f.updateScratchpadFn != nil {
		return f.updateScratchpadFn(ctx, userID, personality)
	}
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.UserResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) SetChatMessageBookmarked(ctx context.Context, userID, messageID uuid.UUID, bookmarked bool) (*models.ChatMessage, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ListChatMessageBookmarks(ctx context.Context, userID, chatID uuid.UUID) ([]*models.ChatMessage, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) FindLatestActiveChatMessageJob(ctx context.Context, userID, userMessageID uuid.UUID) (*models.Job, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ImportChats(ctx context.Context, userID uuid.UUID, convs []models.ImportConversation, onProgress func(imported, skipped int)) (*models.ImportResult, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) CreateJob(ctx context.Context, userID uuid.UUID, jobModel models.Job) (*models.Job, error) {
	jobModel.ID = uuid.New()
	return &jobModel, nil
}
func (f *fakeStore) UpdateJobStatus(ctx context.Context, userID, id uuid.UUID, status models.JobStatus, errorMsg string) (*models.Job, error) {
	return &models.Job{ID: id, Status: status, Error: errorMsg}, nil
}
func (f *fakeStore) UpdateJobProgress(ctx context.Context, userID, id uuid.UUID, progress string) error {
	return nil
}

func TestListChats_TrimsSearchFilter(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	var gotFilters models.ChatFilters

	store := &fakeStore{
		listChatsFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
			if uid != userID {
				t.Fatalf("unexpected userID: got %s want %s", uid, userID)
			}
			gotFilters = filters
			return &models.PaginatedResponse{
				Results:    []any{},
				TotalCount: 0,
				Page:       pageNum,
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/chat?search=%20%20checkpoint%20%20", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotFilters.Query == nil || *gotFilters.Query != "checkpoint" {
		t.Fatalf("expected trimmed search filter, got %#v", gotFilters.Query)
	}
}

func TestListChats_ParsesAdvancedFilters(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	var gotFilters models.ChatFilters

	store := &fakeStore{
		listChatsFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
			gotFilters = filters
			return &models.PaginatedResponse{
				Results:    []any{},
				TotalCount: 0,
				Page:       pageNum,
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/chat?personality_id="+personalityID.String()+"&is_favorite=true&tag=important", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotFilters.PersonalityID == nil || *gotFilters.PersonalityID != personalityID {
		t.Fatalf("expected parsed personality_id filter, got %#v", gotFilters.PersonalityID)
	}
	if gotFilters.IsFavorite == nil || !*gotFilters.IsFavorite {
		t.Fatalf("expected parsed is_favorite filter, got %#v", gotFilters.IsFavorite)
	}
	if gotFilters.Tag == nil || *gotFilters.Tag != "important" {
		t.Fatalf("expected parsed tag filter, got %#v", gotFilters.Tag)
	}
}

func TestListChats_ParsesArchivedFilter(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	var gotFilters models.ChatFilters

	store := &fakeStore{
		listChatsFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
			gotFilters = filters
			return &models.PaginatedResponse{
				Results:    []any{},
				TotalCount: 0,
				Page:       pageNum,
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/chat?archived=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotFilters.Archived == nil || !*gotFilters.Archived {
		t.Fatalf("expected archived=true filter, got %#v", gotFilters.Archived)
	}
}

func TestListChats_InvalidArchivedQuery(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	store := &fakeStore{
		listChatsFn: func(ctx context.Context, uid uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
			t.Fatal("ListChats should not be called when archived param is invalid")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/chat?archived=maybe", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestPatchChat_UpdatesArchived(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Original", Archived: boolPtr(false)}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			out := chat
			out.Archived = boolPtr(true)
			return &out, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"archived":true}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotUpdate.Archived == nil || !*gotUpdate.Archived {
		t.Fatalf("expected UpdateChat with Archived=true, got %#v", gotUpdate.Archived)
	}
}

func TestPatchChat_OmittedArchivedPreservesInUpdate(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Old", Archived: boolPtr(true)}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"name":"New Name"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotUpdate.Archived == nil || !*gotUpdate.Archived {
		t.Fatalf("expected archived to remain true when PATCH omits archived, got %#v", gotUpdate.Archived)
	}
	if gotUpdate.Name != "New Name" {
		t.Fatalf("expected name updated, got %q", gotUpdate.Name)
	}
}

func TestUpdateChat_PreservesPersonalityWhenOmitted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()

	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: personalityID,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			if uid != userID || id != chatID {
				t.Fatalf("unexpected GetChat args: uid=%s id=%s", uid, id)
			}
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/chat/"+chatID.String(), strings.NewReader(`{"name":"Renamed"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if gotUpdate.PersonalityID != personalityID {
		t.Fatalf("expected personality_id to be preserved (%s), got %s", personalityID, gotUpdate.PersonalityID)
	}
	if gotUpdate.IsFavorite != nil {
		t.Fatalf("expected is_favorite to be omitted for datastore when not in request, got %#v", gotUpdate.IsFavorite)
	}

	var resp models.Chat
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.PersonalityID != personalityID {
		t.Fatalf("expected response personality_id to be preserved (%s), got %s", personalityID, resp.PersonalityID)
	}
}

func TestPatchChat_PreservesPersonalityWhenOmitted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()

	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: personalityID,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"name":"Renamed"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if gotUpdate.PersonalityID != personalityID {
		t.Fatalf("expected personality_id to be preserved (%s), got %s", personalityID, gotUpdate.PersonalityID)
	}
	if gotUpdate.IsFavorite != nil {
		t.Fatalf("expected is_favorite to be omitted for datastore when not in request, got %#v", gotUpdate.IsFavorite)
	}
}

func TestPatchChat_CanClearPersonality(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()

	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: personalityID,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"personality_id":"00000000-0000-0000-0000-000000000000"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if gotUpdate.PersonalityID != uuid.Nil {
		t.Fatalf("expected personality_id to be cleared (nil UUID), got %s", gotUpdate.PersonalityID)
	}
}

func TestPatchChat_ActiveMoodPatchPreservesAutoPolicyWhenOmitted(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	moodID := uuid.New()

	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: personalityID,
		IsAutoMood:    true,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"active_mood_id":"`+moodID.String()+`"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if gotUpdate.ActiveMoodID == nil || *gotUpdate.ActiveMoodID != moodID {
		t.Fatalf("expected active_mood_id to be set to %s", moodID)
	}
	if !gotUpdate.IsAutoMood {
		t.Fatalf("expected is_auto_mood to remain true when omitted")
	}
}

func TestPatchChat_ClearActiveMoodForcesAutoPolicy(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	moodID := uuid.New()

	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: personalityID,
		ActiveMoodID:  &moodID,
		IsAutoMood:    false,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"clear_active_mood":true}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	if gotUpdate.ActiveMoodID != nil {
		t.Fatalf("expected active_mood_id to be cleared")
	}
	if !gotUpdate.IsAutoMood {
		t.Fatalf("expected clear_active_mood to force is_auto_mood=true")
	}
}

func TestPatchChat_PersonalityChangeResetsPinnedMood(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	oldPersonalityID := uuid.New()
	newPersonalityID := uuid.New()
	moodID := uuid.New()

	// Existing thread has a mood pinned under the previous personality.
	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: oldPersonalityID,
		ActiveMoodID:  &moodID,
		IsAutoMood:    false,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"personality_id":"`+newPersonalityID.String()+`"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotUpdate.PersonalityID != newPersonalityID {
		t.Fatalf("expected personality switched to %s, got %s", newPersonalityID, gotUpdate.PersonalityID)
	}
	if gotUpdate.ActiveMoodID != nil {
		t.Fatalf("expected pinned mood cleared when personality changes, got %s", gotUpdate.ActiveMoodID)
	}
	if !gotUpdate.IsAutoMood {
		t.Fatalf("expected is_auto_mood forced true when personality changes")
	}
}

func TestPatchChat_PersonalityChangeKeepsExplicitMood(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	oldPersonalityID := uuid.New()
	newPersonalityID := uuid.New()
	oldMoodID := uuid.New()
	newMoodID := uuid.New()

	existing := &models.Chat{
		ID:            chatID,
		UserID:        userID,
		Name:          "Original",
		PersonalityID: oldPersonalityID,
		ActiveMoodID:  &oldMoodID,
		IsAutoMood:    false,
	}

	var gotUpdate models.Chat
	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			gotUpdate = chat
			return &chat, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	body := `{"personality_id":"` + newPersonalityID.String() + `","active_mood_id":"` + newMoodID.String() + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotUpdate.ActiveMoodID == nil || *gotUpdate.ActiveMoodID != newMoodID {
		t.Fatalf("expected explicit mood %s preserved on personality change, got %v", newMoodID, gotUpdate.ActiveMoodID)
	}
}

func TestPatchChatContext_Success(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()
	expectedScratchpad := "context marker"

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: chatID, UserID: userID, PersonalityID: personalityID}, nil
		},
		updateScratchpadFn: func(ctx context.Context, uid uuid.UUID, personality models.Personality) (*models.Personality, error) {
			if personality.ID != personalityID {
				t.Fatalf("unexpected personality id: %s", personality.ID)
			}
			if personality.Scratchpad != expectedScratchpad {
				t.Fatalf("unexpected scratchpad: %q", personality.Scratchpad)
			}
			return &models.Personality{ID: personalityID, Scratchpad: personality.Scratchpad}, nil
		},
		getChatContextFn: func(ctx context.Context, uid, id uuid.UUID) (*models.ChatContext, error) {
			return &models.ChatContext{
				ChatID:           chatID,
				ActiveScratchpad: expectedScratchpad,
				Summary:          "summary",
			}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String()+"/context", strings.NewReader(`{"active_scratchpad":"`+expectedScratchpad+`"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp models.ChatContext
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ActiveScratchpad != expectedScratchpad {
		t.Fatalf("unexpected active_scratchpad: %q", resp.ActiveScratchpad)
	}
}

func TestPatchChatContext_CanClearScratchpad(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	personalityID := uuid.New()

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: chatID, UserID: userID, PersonalityID: personalityID}, nil
		},
		updateScratchpadFn: func(ctx context.Context, uid uuid.UUID, personality models.Personality) (*models.Personality, error) {
			if personality.Scratchpad != "" {
				t.Fatalf("expected empty scratchpad for clear flow, got %q", personality.Scratchpad)
			}
			return &models.Personality{ID: personalityID, Scratchpad: ""}, nil
		},
		getChatContextFn: func(ctx context.Context, uid, id uuid.UUID) (*models.ChatContext, error) {
			return &models.ChatContext{ChatID: chatID, ActiveScratchpad: "", Summary: "summary"}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String()+"/context", strings.NewReader(`{"active_scratchpad":""}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestPatchChat_RejectsTooManyTags(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Original"}

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			t.Fatal("update should not be called for invalid tags")
			return nil, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"tags":["1","2","3","4","5","6","7","8","9","10","11"]}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestPatchChat_FavoriteLimitExceededReturnsBadRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()
	existing := &models.Chat{ID: chatID, UserID: userID, Name: "Original"}

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return existing, nil
		},
		updateChatFn: func(ctx context.Context, uid uuid.UUID, chat models.Chat) (*models.Chat, error) {
			return nil, datastore.ErrFavoriteLimitExceeded
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String(), strings.NewReader(`{"is_favorite":true}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestPatchChatContext_ValidationAndNotFound(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return nil, datastore.ErrChatNotFound
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	invalidReq := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String()+"/context", strings.NewReader(`{}`))
	invalidReq = mux.SetURLVars(invalidReq, map[string]string{"id": chatID.String()})
	invalidReq = invalidReq.WithContext(context.WithValue(invalidReq.Context(), middleware.UserIDKey, userID))
	invalidResp := httptest.NewRecorder()
	router.ServeHTTP(invalidResp, invalidReq)
	if invalidResp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d for invalid request, got %d", http.StatusBadRequest, invalidResp.Code)
	}

	notFoundReq := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String()+"/context", strings.NewReader(`{"active_scratchpad":"x"}`))
	notFoundReq = mux.SetURLVars(notFoundReq, map[string]string{"id": chatID.String()})
	notFoundReq = notFoundReq.WithContext(context.WithValue(notFoundReq.Context(), middleware.UserIDKey, userID))
	notFoundResp := httptest.NewRecorder()
	router.ServeHTTP(notFoundResp, notFoundReq)
	if notFoundResp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d for missing chat, got %d", http.StatusNotFound, notFoundResp.Code)
	}
}

func TestPatchChatContext_Unauthorized(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	chatID := uuid.New()
	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String()+"/context", strings.NewReader(`{"active_scratchpad":"x"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestPatchChatContext_MissingActivePersonality(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	chatID := uuid.New()

	store := &fakeStore{
		getChatFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: chatID, UserID: userID, PersonalityID: uuid.Nil}, nil
		},
	}

	h := NewHandler(store, zap.NewNop(), nil, HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPatch, "/chat/"+chatID.String()+"/context", strings.NewReader(`{"active_scratchpad":"x"}`))
	req = mux.SetURLVars(req, map[string]string{"id": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}
