package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// newRehydrationHandler wires a chat handler to a real agent over a sqlmock datastore, so a test
// can tell whether a request tried to start rehydration: starting one begins with a conditional
// UPDATE on chats, which the test expects (or, to prove none happens, expects and then finds unmet).
func newRehydrationHandler(t *testing.T, store *fakeStore) (*mux.Router, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })
	ds, err := datastore.NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	h := NewHandler(store, zap.NewNop(), agent.NewAgent(ds, zap.NewNop(), nil, "test-key", nil, "", agent.AgentConfig{}), HandlerConfig{})
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router, mock
}

func serve(router *mux.Router, userID uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// Opening a thread is the demand signal: GET /chat/{id} on an unarchived imported thread that has no
// summary tries to claim it (once, atomically); every other thread is left alone without a write.
func TestGetChat_OpeningTriggersRehydrationOnlyWhenNeeded(t *testing.T) {
	t.Parallel()
	str := func(s string) *string { return &s }

	cases := []struct {
		name      string
		chat      models.Chat
		wantClaim bool
	}{
		{"imported, unarchived, never rehydrated", models.Chat{Source: str("openai"), Archived: boolPtr(false)}, true},
		{"previous attempt failed", models.Chat{Source: str("openai"), Archived: boolPtr(false), RehydrationState: models.RehydrationStateFailed}, true},
		{"native thread", models.Chat{Archived: boolPtr(false)}, false},
		{"imported but still archived", models.Chat{Source: str("openai"), Archived: boolPtr(true)}, false},
		{"already pending", models.Chat{Source: str("openai"), Archived: boolPtr(false), RehydrationState: models.RehydrationStatePending}, false},
		{"already processing", models.Chat{Source: str("openai"), Archived: boolPtr(false), RehydrationState: models.RehydrationStateProcessing}, false},
		{"already rehydrated", models.Chat{Source: str("openai"), Archived: boolPtr(false), RehydrationState: models.RehydrationStateReady, CheckpointSummary: "s"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			userID, chatID := uuid.New(), uuid.New()
			chat := tc.chat
			chat.ID, chat.UserID = chatID, userID
			router, mock := newRehydrationHandler(t, &fakeStore{
				getChatFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) { return &chat, nil },
			})
			// Another caller wins the claim (0 rows), so no job is created: this only observes the attempt.
			mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 0))

			w := serve(router, userID, http.MethodGet, "/chat/"+chatID.String(), "")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			if tc.wantClaim {
				require.NoError(t, mock.ExpectationsWereMet(), "opening should have attempted the claim")
			} else {
				require.Error(t, mock.ExpectationsWereMet(), "opening must not touch rehydration state")
			}
		})
	}
}

// Unarchiving, singly or as a bulk restore (one PATCH per selected thread), never starts
// rehydration: the claim is only attempted when a thread is opened or a turn arrives.
func TestPatchChat_UnarchivingNeverStartsRehydration(t *testing.T) {
	t.Parallel()
	source := "openai"
	userID := uuid.New()
	router, mock := newRehydrationHandler(t, &fakeStore{
		getChatFn: func(_ context.Context, _ uuid.UUID, id uuid.UUID) (*models.Chat, error) {
			return &models.Chat{ID: id, UserID: userID, Name: "Imported", Source: &source, Archived: boolPtr(true)}, nil
		},
		updateChatFn: func(_ context.Context, _ uuid.UUID, chat models.Chat) (*models.Chat, error) {
			out := chat
			out.Archived = boolPtr(false)
			return &out, nil
		},
	})
	// If any request tried to claim a thread for rehydration, this expectation would be consumed.
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1))

	for range 5 {
		w := serve(router, userID, http.MethodPatch, "/chat/"+uuid.NewString(), `{"archived":false}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	require.Error(t, mock.ExpectationsWereMet(), "unarchive must not claim any thread for rehydration")
}
