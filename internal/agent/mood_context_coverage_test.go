package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// --- resolveActiveMood ---

func TestResolveActiveMood_NoActiveMoodManualPolicyReturnsNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{chat: &models.Chat{IsAutoMood: false}}
	got := a.resolveActiveMood(context.Background(), uuid.New(), chatCtx, "hi", uuid.New())
	require.Nil(t, got)
}

func TestResolveActiveMood_AutoPolicyNoPersonalityReturnsNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{chat: &models.Chat{IsAutoMood: true, PersonalityID: uuid.Nil}}
	got := a.resolveActiveMood(context.Background(), uuid.New(), chatCtx, "hi", uuid.New())
	require.Nil(t, got)
}

func TestResolveActiveMood_AutoPolicyGetMoodsErrorReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{IsAutoMood: true, PersonalityID: uuid.New()}}
	got := a.resolveActiveMood(context.Background(), uuid.New(), chatCtx, "hi", uuid.New())
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveActiveMood_AutoPolicyNoMoodsReturnsNil(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{IsAutoMood: true, PersonalityID: uuid.New()}}
	got := a.resolveActiveMood(context.Background(), uuid.New(), chatCtx, "hi", uuid.New())
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResolveActiveMood_ActiveMoodGetErrorClearFailsReturnsNilAfterLoggingBoth(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	moodID := uuid.New()

	// GetMood fails (first SELECT), then SetChatActiveMood's chat lookup also fails
	// (second SELECT), so the stale active mood cannot be cleared; chat.ActiveMoodID
	// stays set but autoPolicy is false, so resolveActiveMood returns nil.
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{IsAutoMood: false, ActiveMoodID: &moodID}}
	got := a.resolveActiveMood(context.Background(), uuid.New(), chatCtx, "hi", uuid.New())
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- isFirstUserMessageInChat ---

func TestIsFirstUserMessageInChat_ListErrorReturnsFalse(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	got := a.isFirstUserMessageInChat(context.Background(), uuid.New(), uuid.New(), uuid.New())
	require.False(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- autoSelectMood ---

func TestAutoSelectMood_NilProviderReturnsNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	got := a.autoSelectMood(context.Background(), []*models.Mood{{ID: uuid.New()}}, "hi")
	require.Nil(t, got)
}

func TestAutoSelectMood_NonVendorLLMReturnsNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("provider must not be called in mock mode")
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), mockLLM: true, OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got := a.autoSelectMood(context.Background(), []*models.Mood{{ID: uuid.New()}}, "hi")
	require.Nil(t, got)
}

func TestAutoSelectMood_ProviderErrorReturnsNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got := a.autoSelectMood(context.Background(), []*models.Mood{{ID: uuid.New()}}, "hi")
	require.Nil(t, got)
}

func TestAutoSelectMood_ParseErrorReturnsNil(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", "not json"))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got := a.autoSelectMood(context.Background(), []*models.Mood{{ID: uuid.New()}}, "hi")
	require.Nil(t, got)
}

func TestAutoSelectMood_SuccessReturnsMatchingMood(t *testing.T) {
	t.Parallel()
	moodID := uuid.New()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", `{\"mood_id\":\"`+moodID.String()+`\"}`))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	moods := []*models.Mood{{ID: uuid.New()}, {ID: moodID, Name: "Match"}}
	got := a.autoSelectMood(context.Background(), moods, "hi")
	require.NotNil(t, got)
	require.Equal(t, "Match", got.Name)
}

func TestAutoSelectMood_SuccessNoMatchingMoodReturnsNil(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", `{\"mood_id\":\"`+uuid.New().String()+`\"}`))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got := a.autoSelectMood(context.Background(), []*models.Mood{{ID: uuid.New()}}, "hi")
	require.Nil(t, got)
}

// --- persistActiveMood ---

func TestPersistActiveMood_ErrorIsLoggedNotPanicked(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	require.NotPanics(t, func() {
		a.persistActiveMood(context.Background(), uuid.New(), uuid.New(), &models.Mood{ID: uuid.New()})
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- listMoodsTool ---

func TestListMoodsTool_ErrorReturnsErrorJSON(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{UserID: uuid.New(), PersonalityID: uuid.New()}}
	out, err := a.listMoodsTool(context.Background(), chatCtx, nil)
	require.NoError(t, err)
	var result listMoodsResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "failed to list modes")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- changeMoodTool ---

func TestChangeMoodTool_InvalidArgsJSONReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{chat: &models.Chat{}}
	out, err := a.changeMoodTool(context.Background(), chatCtx, []byte("not json"))
	require.NoError(t, err)
	var result changeMoodResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "invalid arguments")
}

func TestChangeMoodTool_MissingModeIDReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{chat: &models.Chat{}}
	args, err := json.Marshal(changeMoodArgs{})
	require.NoError(t, err)
	out, err := a.changeMoodTool(context.Background(), chatCtx, args)
	require.NoError(t, err)
	var result changeMoodResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Equal(t, "mode_id is required", result.Error)
}

func TestChangeMoodTool_LegacyMoodIDFallback(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{UserID: uuid.New()}}
	args, err := json.Marshal(changeMoodArgs{MoodID: uuid.New().String()})
	require.NoError(t, err)
	out, err := a.changeMoodTool(context.Background(), chatCtx, args)
	require.NoError(t, err)
	var result changeMoodResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "mode not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChangeMoodTool_InvalidModeIDReturnsErrorResult(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatCtx := &chatContext{chat: &models.Chat{}}
	args, err := json.Marshal(changeMoodArgs{ModeID: "not-a-uuid"})
	require.NoError(t, err)
	out, err := a.changeMoodTool(context.Background(), chatCtx, args)
	require.NoError(t, err)
	var result changeMoodResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "invalid mode_id")
}

func TestChangeMoodTool_ModeNotFoundReturnsErrorResult(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	chatCtx := &chatContext{chat: &models.Chat{UserID: uuid.New()}}
	args, err := json.Marshal(changeMoodArgs{ModeID: uuid.New().String()})
	require.NoError(t, err)
	out, err := a.changeMoodTool(context.Background(), chatCtx, args)
	require.NoError(t, err)
	var result changeMoodResult
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.False(t, result.Success)
	require.Contains(t, result.Error, "mode not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- activeMoodID ---

func TestActiveMoodID_NonNilMoodReturnsPointerToID(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	got := activeMoodID(&models.Mood{ID: id})
	require.NotNil(t, got)
	require.Equal(t, id, *got)
}
