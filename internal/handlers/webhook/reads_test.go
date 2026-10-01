package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

var (
	readScopes  = []models.WebhookScope{models.WebhookScopeChatRead}
	writeScopes = []models.WebhookScope{models.WebhookScopeMessagesWrite}
	testOwner   = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testTokenID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// readRouter mounts the webhook routes the way the server does. The stand-in auth middleware plays
// WebhookAuthMiddleware: no Authorization header is a 401; otherwise it records the owner and the
// token's scopes, which is all the real one adds to the context.
func readRouter(h *Handler, scopes []models.WebhookScope) *mux.Router {
	router := mux.NewRouter()
	sub := router.PathPrefix("/webhooks").Subrouter()
	sub.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), middleware.UserIDKey, testOwner)
			ctx = context.WithValue(ctx, middleware.WebhookTokenIDKey, testTokenID)
			ctx = context.WithValue(ctx, middleware.WebhookScopesKey, scopes)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	h.RegisterWebhookRoutes(sub)
	return router
}

func get(t *testing.T, router http.Handler, path string, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authed {
		req.Header.Set("Authorization", "Bearer wht_test")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

// untouched is a provider whose every read fails the test: proof that a refused request never
// reached the datastore.
func untouched(t *testing.T) *mockProvider {
	t.Helper()
	fail := func(name string) { t.Fatalf("%s must not be called", name) }
	return &mockProvider{
		listChatsFn: func(context.Context, uuid.UUID, int, int, models.ChatFilters) (*models.PaginatedResponse, error) {
			fail("ListChats")
			return nil, nil
		},
		listPersonalitiesFn: func(context.Context, uuid.UUID, int, int, models.PersonalityFilters) (*models.PaginatedResponse, error) {
			fail("ListPersonalities")
			return nil, nil
		},
		listMessagesFn: func(context.Context, uuid.UUID, uuid.UUID, int, int, models.ChatMessageFilters) (*models.PaginatedResponse, error) {
			fail("ListChatMessages")
			return nil, nil
		},
		getMessageFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.ChatMessage, error) {
			fail("GetChatMessage")
			return nil, nil
		},
		getJobFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Job, error) {
			fail("GetJob")
			return nil, nil
		},
	}
}

func readPaths() map[string]string {
	chat, msg, job := uuid.NewString(), uuid.NewString(), uuid.NewString()
	return map[string]string{
		"list chats":    "/webhooks/chat",
		"list personas": "/webhooks/personality",
		"list messages": "/webhooks/chat/" + chat + "/messages",
		"get message":   "/webhooks/chat/chat-message/" + msg,
		"get job":       "/webhooks/job/" + job,
	}
}

func TestReadRoutesRequireAToken(t *testing.T) {
	router := readRouter(NewHandler(untouched(t), &mockAgent{}, zap.NewNop()), readScopes)
	for name, path := range readPaths() {
		require.Equal(t, http.StatusUnauthorized, get(t, router, path, false).Code, name)
	}
}

// TestReadRoutesRequireTheReadScope: a token minted to post must not start reading. A legacy token
// (stored with no scopes) is treated as write-only, so this is what protects every existing one.
func TestReadRoutesRequireTheReadScope(t *testing.T) {
	for _, scopes := range [][]models.WebhookScope{writeScopes, nil, {}} {
		router := readRouter(NewHandler(untouched(t), &mockAgent{}, zap.NewNop()), scopes)
		for name, path := range readPaths() {
			rec := get(t, router, path, true)
			require.Equal(t, http.StatusForbidden, rec.Code, "%s with scopes %v", name, scopes)
			require.Contains(t, rec.Body.String(), "chat:read", "the error names the missing scope")
		}
	}
}

func TestPostingRequiresTheWriteScopeAndOnlyThat(t *testing.T) {
	post := func(scopes []models.WebhookScope) int {
		router := readRouter(NewHandler(&mockProvider{}, &mockAgent{}, zap.NewNop()), scopes)
		req := httptest.NewRequest(http.MethodPost, "/webhooks/chat/"+uuid.NewString()+"/messages", strings.NewReader(`{"mode":"user","message":""}`))
		req.Header.Set("Authorization", "Bearer wht_test")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusForbidden, post(readScopes), "a read-only token cannot post")
	require.Equal(t, http.StatusBadRequest, post(writeScopes), "a write token reaches the handler (empty message is a 400)")
	require.Equal(t, http.StatusBadRequest, post(append(append([]models.WebhookScope{}, readScopes...), writeScopes...)))
}

func TestListChatMessagesReturnsTheStableSubset(t *testing.T) {
	chatID := uuid.New()
	sent := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	label, reasoning := "smiling", "thinking out loud"
	desc := "a chart"
	full := &models.ChatMessage{
		ID: uuid.New(), ChatID: chatID, Message: "hello", Origin: models.MessageOriginAssistant,
		ReadStatus: models.MessageReadStatusUnread, SentAt: sent,
		GenerationModel: "some-model", GenerationPersonality: "Vix", GenerationMoodName: "calm",
		GenerationMoodThumbnail: "BASE64-PORTRAIT", GenerationExpressionLabel: &label, ModelReasoning: &reasoning,
		ToolCalls:   []*models.ToolCall{{}},
		Attachments: []*models.FileAttachment{{ID: uuid.New(), UserID: uuid.New(), Name: "chart.png", FileType: "image/png", S3Key: "secret/key", Description: &desc}},
	}
	var gotPage, gotLimit int
	var gotChat uuid.UUID
	p := &mockProvider{listMessagesFn: func(_ context.Context, userID, cid uuid.UUID, page, limit int, _ models.ChatMessageFilters) (*models.PaginatedResponse, error) {
		require.Equal(t, testOwner, userID, "reads are for the token's owner")
		gotChat, gotPage, gotLimit = cid, page, limit
		return &models.PaginatedResponse{Results: []any{full}, TotalCount: 1, Page: page, NextCursor: "next"}, nil
	}}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)

	rec := get(t, router, "/webhooks/chat/"+chatID.String()+"/messages?page=2&limit=500", true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, chatID, gotChat)
	require.Equal(t, 2, gotPage)
	require.Equal(t, maxReadPageSize, gotLimit, "a limit above the maximum is clamped, not honoured")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	body := rec.Body.String()
	for _, want := range []string{`"id"`, `"message":"hello"`, `"origin":"Assistant"`, `"sent_at"`, `"generation_expression_label":"smiling"`, `"name":"chart.png"`, `"next_cursor":"next"`} {
		require.Contains(t, body, want)
	}
	for _, leak := range []string{"BASE64-PORTRAIT", "thinking out loud", "tool_calls", "model_reasoning", "context_breakdown", "secret/key", "s3_key", "user_id", "bookmarked", "generation_mood_thumbnail"} {
		require.NotContains(t, body, leak, "internal or heavy fields must not reach integrations")
	}
}

func TestListChatMessagesCursorWalksBackwards(t *testing.T) {
	chatID := uuid.New()
	at, id := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC), uuid.New()
	var gotAt time.Time
	var gotID uuid.UUID
	var gotLimit int
	p := &mockProvider{listMessagesBeforeFn: func(_ context.Context, _, _ uuid.UUID, beforeAt time.Time, beforeID uuid.UUID, limit int, _ models.ChatMessageFilters) (*models.PaginatedResponse, error) {
		gotAt, gotID, gotLimit = beforeAt, beforeID, limit
		return &models.PaginatedResponse{Results: []any{}, Page: 1}, nil
	}}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)

	cursor := models.EncodeMessageCursor(at, id)
	rec := get(t, router, "/webhooks/chat/"+chatID.String()+"/messages?limit=30&cursor="+cursor, true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, gotAt.Equal(at))
	require.Equal(t, id, gotID)
	require.Equal(t, 30, gotLimit)
}

func TestEmptyResultsAreAnOKEmptyList(t *testing.T) {
	p := &mockProvider{listMessagesFn: func(context.Context, uuid.UUID, uuid.UUID, int, int, models.ChatMessageFilters) (*models.PaginatedResponse, error) {
		return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
	}}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)
	rec := get(t, router, "/webhooks/chat/"+uuid.NewString()+"/messages", true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"results":[]`)
	require.NotContains(t, rec.Body.String(), "next_cursor")
}

func TestMalformedReadRequestsAreRejectedNotIgnored(t *testing.T) {
	router := readRouter(NewHandler(untouched(t), &mockAgent{}, zap.NewNop()), readScopes)
	chat := "/webhooks/chat/" + uuid.NewString() + "/messages"
	for name, path := range map[string]string{
		"chat id not a uuid":      "/webhooks/chat/not-a-uuid/messages",
		"message id not a uuid":   "/webhooks/chat/chat-message/nope",
		"job id not a uuid":       "/webhooks/job/nope",
		"limit zero":              chat + "?limit=0",
		"limit not a number":      chat + "?limit=lots",
		"limit negative":          chat + "?limit=-5",
		"page zero":               chat + "?page=0",
		"origin unknown":          chat + "?origin=robot",
		"min_date not a time":     chat + "?min_date=yesterday",
		"max_date not a time":     chat + "?max_date=2026-09-30",
		"cursor garbage":          chat + "?cursor=not-a-cursor",
		"persona id not a uuid":   "/webhooks/chat?personality_id=vix",
		"archived not a boolean":  "/webhooks/chat?archived=maybe",
		"is_favorite not boolean": "/webhooks/chat?is_favorite=sure",
		"personas limit zero":     "/webhooks/personality?limit=0",
	} {
		rec := get(t, router, path, true)
		require.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", name, rec.Body.String())
	}
}

// TestOthersRecordsAreIndistinguishableFromMissingOnes: the datastore reports a chat, message or
// job that belongs to another account exactly as it reports one that does not exist, and the
// routes must not add a tell (no 403, same wording).
func TestOthersRecordsAreIndistinguishableFromMissingOnes(t *testing.T) {
	p := &mockProvider{
		listMessagesFn: func(context.Context, uuid.UUID, uuid.UUID, int, int, models.ChatMessageFilters) (*models.PaginatedResponse, error) {
			return nil, datastore.ErrChatNotFound
		},
		getMessageFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.ChatMessage, error) {
			return nil, datastore.ErrChatMessageNotFound
		},
		getJobFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Job, error) { return nil, datastore.ErrJobNotFound },
	}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)
	for name, path := range readPaths() {
		if name == "list chats" || name == "list personas" {
			continue
		}
		rec := get(t, router, path, true)
		require.Equal(t, http.StatusNotFound, rec.Code, name)
	}

	// An authorization error from the job lookup is folded into the same 404.
	p.getJobFn = func(context.Context, uuid.UUID, uuid.UUID) (*models.Job, error) {
		return nil, datastore.ErrUnauthorized
	}
	require.Equal(t, http.StatusNotFound, get(t, router, readPaths()["get job"], true).Code)
}

func TestGetChatMessageAndJobReturnWhatAPollingClientNeeds(t *testing.T) {
	msgID, jobID, resultID := uuid.New(), uuid.New(), uuid.New()
	sent := time.Date(2026, 9, 30, 14, 5, 0, 0, time.UTC)
	errMsg := "model timed out"
	p := &mockProvider{
		getMessageFn: func(_ context.Context, userID, id uuid.UUID) (*models.ChatMessage, error) {
			require.Equal(t, testOwner, userID)
			return &models.ChatMessage{ID: id, Message: "ack", Origin: models.MessageOriginUser, SentAt: sent, LastErrorMessage: &errMsg}, nil
		},
		getJobFn: func(_ context.Context, _, id uuid.UUID) (*models.Job, error) {
			return &models.Job{ID: id, Status: "completed", ResultID: &resultID}, nil
		},
	}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)

	msg := decode(t, get(t, router, "/webhooks/chat/chat-message/"+msgID.String(), true))
	require.Equal(t, msgID.String(), msg["id"])
	require.Equal(t, "2026-09-30T14:05:00Z", msg["sent_at"], "sent_at is always present: reply matching depends on it")
	require.Equal(t, errMsg, msg["last_error_message"])

	rec := get(t, router, "/webhooks/job/"+jobID.String(), true)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "job polling must not be cached")
	job := decode(t, rec)
	require.Equal(t, "completed", job["status"])
	require.Equal(t, resultID.String(), job["result_id"])
}

func TestListChatsFiltersByPersona(t *testing.T) {
	persona := uuid.New()
	var got models.ChatFilters
	var gotLimit int
	p := &mockProvider{listChatsFn: func(_ context.Context, userID uuid.UUID, _, limit int, f models.ChatFilters) (*models.PaginatedResponse, error) {
		require.Equal(t, testOwner, userID)
		got, gotLimit = f, limit
		return &models.PaginatedResponse{Results: []any{&models.Chat{ID: uuid.New(), Name: "thread", PersonalityID: persona}}, TotalCount: 1, Page: 1}, nil
	}}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)

	rec := get(t, router, "/webhooks/chat?personality_id="+persona.String()+"&archived=true&search=budget&is_favorite=false&limit=7", true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, got.PersonalityID)
	require.Equal(t, persona, *got.PersonalityID)
	require.True(t, *got.Archived)
	require.False(t, *got.IsFavorite)
	require.Equal(t, "budget", *got.Query)
	require.Equal(t, 7, gotLimit)
	require.Contains(t, rec.Body.String(), `"personality_id":"`+persona.String()+`"`)
	require.NotContains(t, rec.Body.String(), "system_prompt")
}

func TestListPersonalitiesServesNamesNotPrivateState(t *testing.T) {
	accent := "#7a5cff"
	p := &mockProvider{listPersonalitiesFn: func(_ context.Context, userID uuid.UUID, _, _ int, f models.PersonalityFilters) (*models.PaginatedResponse, error) {
		require.Equal(t, testOwner, userID)
		require.Equal(t, "Vix", *f.Name)
		return &models.PaginatedResponse{Results: []any{&models.Personality{
			ID: uuid.New(), Name: "Vix", AccentColor: &accent,
			SystemPrompt: "SECRET SYSTEM PROMPT", Scratchpad: "SECRET SCRATCHPAD", ScratchpadHistory: []string{"OLD SCRATCHPAD"},
			MemorySearchPrompt: "SECRET MEMORY PROMPT", FileAttachments: []models.FileAttachment{{Name: "private.txt"}},
		}}, TotalCount: 1, Page: 1}, nil
	}}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.NewNop()), readScopes)

	rec := get(t, router, "/webhooks/personality?name=Vix", true)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"name":"Vix"`)
	require.Contains(t, body, `"accent_color":"#7a5cff"`)
	for _, secret := range []string{"SECRET", "OLD SCRATCHPAD", "private.txt", "system_prompt", "scratchpad", "file_attachments", "memory_search_prompt"} {
		require.NotContains(t, body, secret, "a persona's private working state must never be served here")
	}
}

func TestReadsAreAuditedByTokenWithoutContent(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	p := &mockProvider{listMessagesFn: func(context.Context, uuid.UUID, uuid.UUID, int, int, models.ChatMessageFilters) (*models.PaginatedResponse, error) {
		return &models.PaginatedResponse{Results: []any{&models.ChatMessage{Message: "TOP SECRET BODY"}}, Page: 1}, nil
	}}
	router := readRouter(NewHandler(p, &mockAgent{}, zap.New(core)), readScopes)
	chatID := uuid.New()

	require.Equal(t, http.StatusOK, get(t, router, "/webhooks/chat/"+chatID.String()+"/messages", true).Code)
	entries := logs.FilterMessage("webhook read").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, testTokenID.String(), fields["webhook_token_id"])
	require.Equal(t, "list_messages", fields["operation"])
	require.Equal(t, chatID.String(), fields["chat_id"])
	require.EqualValues(t, 1, fields["returned"])
	require.NotContains(t, entries[0].Message+toJSON(fields), "TOP SECRET BODY")
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
