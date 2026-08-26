package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// newHTTPTestOpenAIProvider builds a real *provider.OpenAIProvider whose client talks to an
// httptest server, so summarizeText/extractMemoriesFromTranscript can be exercised end to end
// without a live OpenAI dependency.
func newHTTPTestOpenAIProvider(baseURL string) *provider.OpenAIProvider {
	client := openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(baseURL))
	return provider.NewOpenAIProvider(nil, &client, nil, nil)
}

// jsonResponsesServer starts an httptest server that always answers the Responses API with body.
func jsonResponsesServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// responseTextJSONBody builds a minimal Responses API JSON body carrying a single output_text.
func responseTextJSONBody(id, text string) string {
	return `{"id":"` + id + `","object":"response","created_at":1,"model":"test","status":"completed",` +
		`"output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"` + text + `"}]}],` +
		`"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`
}

// expectJobStatusUpdateNotFound mocks the exists-check inside UpdateJobStatus failing (job not
// found/unauthorized), which is the simplest way to make UpdateJobStatus return an error from
// this package's tests without also having to model the full update+owner-load+commit chain.
// runThreadRehydration/failRehydration only log a warning on this error, so it never changes the
// outcome under test.
//
// The exists-check goes through job.HasOwnerWith(user.ID(...)), which ent/entsql compiles to a
// query that scans the job id (UUID) column to determine whether any row matched, not a bool
// projection - zero rows is how "not found" is expressed here.
func expectJobStatusUpdateNotFound(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
}

// --- isRehydrationInFlight ---

func TestIsRehydrationInFlight(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state string
		want  bool
	}{
		{models.RehydrationStateNone, false},
		{models.RehydrationStatePending, true},
		{models.RehydrationStateProcessing, true},
		{models.RehydrationStateReady, false},
		{models.RehydrationStateFailed, false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, isRehydrationInFlight(tc.state), "state=%q", tc.state)
	}
}

// --- WaitForThreadRehydration ---

func TestWaitForThreadRehydration_GetStateErrorReturnsImmediately(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	a.WaitForThreadRehydration(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWaitForThreadRehydration_NotInFlightReturnsImmediately(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state"}).AddRow(models.RehydrationStateReady))

	a := newTestAgent(ds)
	a.WaitForThreadRehydration(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWaitForThreadRehydration_ContextCancelledReturnsPromptly(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state"}).AddRow(models.RehydrationStatePending))

	ctx, cancel := context.WithCancel(context.Background())

	a := newTestAgent(ds)
	done := make(chan struct{})
	go func() {
		a.WaitForThreadRehydration(ctx, uuid.New(), uuid.New())
		close(done)
	}()

	// Give the initial (synchronous, mock-backed) state read time to complete and enter the
	// poll/select loop before cancelling, so this exercises the ctx.Done() case rather than the
	// initial-read error path (already covered by GetStateErrorReturnsImmediately above).
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForThreadRehydration did not return promptly after context cancellation")
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWaitForThreadRehydration_PollSettlesToReady(t *testing.T) {
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state"}).AddRow(models.RehydrationStatePending))
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state"}).AddRow(models.RehydrationStateReady))

	a := newTestAgent(ds)
	start := time.Now()
	a.WaitForThreadRehydration(context.Background(), uuid.New(), uuid.New())
	require.Less(t, time.Since(start), 10*time.Second, "should settle on the first poll tick, not time out")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWaitForThreadRehydration_PollChatNotFoundReturnsPromptly(t *testing.T) {
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state"}).AddRow(models.RehydrationStatePending))
	// Second poll: zero rows -> ent.IsNotFound -> ErrChatNotFound -> gate returns immediately
	// instead of continuing to poll.
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"rehydration_state"}))

	a := newTestAgent(ds)
	start := time.Now()
	a.WaitForThreadRehydration(context.Background(), uuid.New(), uuid.New())
	require.Less(t, time.Since(start), 10*time.Second)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- EnqueueThreadRehydration ---

func ctxWithUser(userID uuid.UUID) context.Context {
	return context.WithValue(context.Background(), middleware.UserIDKey, userID)
}

func TestEnqueueThreadRehydration_MissingUserInContextIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.EnqueueThreadRehydration(context.Background(), uuid.New(), uuid.New())
	})
}

func TestEnqueueThreadRehydration_SetStateFailsReturnsWithoutCreatingJob(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*").WillReturnError(errCoverageTestSentinel)

	a := newTestAgent(ds)
	userID := uuid.New()
	require.NotPanics(t, func() {
		a.EnqueueThreadRehydration(ctxWithUser(userID), userID, uuid.New())
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- failRehydration ---

func TestFailRehydration_LogsBothFailuresWithoutPanicking(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*chats.*").WillReturnError(errCoverageTestSentinel)
	expectJobStatusUpdateNotFound(mock)

	a := newTestAgent(ds)
	require.NotPanics(t, func() {
		a.failRehydration(context.Background(), uuid.New(), uuid.New(), uuid.New(), "boom reason")
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- runThreadRehydration ---

func expectGetChatMessagesForSummarySuccess(mock sqlmock.Sqlmock, msgs []models.ChatMessage) {
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	rows := sqlmock.NewRows([]string{"message", "origin", "sent_at"})
	for _, m := range msgs {
		rows.AddRow(m.Message, string(m.Origin), m.SentAt)
	}
	mock.ExpectQuery("SELECT .*").WillReturnRows(rows)
}

func TestRunThreadRehydration_LoadMessagesFailureCallsFailRehydration(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	expectJobStatusUpdateNotFound(mock)                                           // mark processing (best-effort, ignored on failure)
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1)) // mark processing
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)        // GetChatMessagesForSummary owns-check fails
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1)) // failRehydration: mark failed
	expectJobStatusUpdateNotFound(mock)                                           // failRehydration: job status update

	a := newTestAgent(ds)
	require.NotPanics(t, func() {
		a.runThreadRehydration(context.Background(), uuid.New(), uuid.New(), uuid.New())
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRunThreadRehydration_ShortThreadMarksReadyWithoutSummarizing(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []models.ChatMessage{
		{Message: "hi", Origin: models.MessageOriginUser, SentAt: base},
		{Message: "hello", Origin: models.MessageOriginAssistant, SentAt: base.Add(time.Minute)},
	}

	expectJobStatusUpdateNotFound(mock)                                           // mark processing
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1)) // mark processing state
	expectGetChatMessagesForSummarySuccess(mock, msgs)
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1)) // mark ready (short thread)
	expectJobStatusUpdateNotFound(mock)                                           // complete job

	a := newTestAgent(ds) // OpenAIProvider/memoryTool nil -> extractAndStoreImportedMemories is a no-op
	require.NotPanics(t, func() {
		a.runThreadRehydration(context.Background(), uuid.New(), uuid.New(), uuid.New())
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRunThreadRehydration_LongThreadMockLLMUsesDeterministicSummary(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	msgs := turnMsgs(10) // 10 user turns, keepTurns default (5) -> needs summarization

	expectJobStatusUpdateNotFound(mock)                                           // mark processing
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1)) // mark processing state
	expectGetChatMessagesForSummarySuccess(mock, msgs)
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1)) // SetImportedThreadRehydrated
	expectJobStatusUpdateNotFound(mock)                                           // complete job

	a := newTestAgent(ds)
	a.mockLLM = true // nonVendorLLM() -> deterministic fake summary, no OpenAIProvider needed
	require.NotPanics(t, func() {
		a.runThreadRehydration(context.Background(), uuid.New(), uuid.New(), uuid.New())
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- summarizeText ---

func TestSummarizeText_NoProviderConfiguredReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.summarizeText(context.Background(), uuid.New(), "instructions", "input", 100)
	require.ErrorContains(t, err, "no summarization provider configured")
}

func TestSummarizeText_SuccessReturnsTrimmedOutput(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_1", "  a tidy summary  "))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got, err := a.summarizeText(context.Background(), uuid.New(), "instructions", "input", 100)
	require.NoError(t, err)
	require.Equal(t, "a tidy summary", got)
}

func TestSummarizeText_EmptyOutputReturnsError(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_2", "   "))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.summarizeText(context.Background(), uuid.New(), "instructions", "input", 100)
	require.ErrorContains(t, err, "empty summary")
}

func TestSummarizeText_ProviderErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.summarizeText(context.Background(), uuid.New(), "instructions", "input", 100)
	require.ErrorContains(t, err, "openai summarize")
}

// --- summarizeImportedThread ---

func TestSummarizeImportedThread_EmptyTranscriptReturnsError(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	_, err := a.summarizeImportedThread(context.Background(), uuid.New(), []models.ChatMessage{{Message: "   "}})
	require.ErrorContains(t, err, "empty transcript")
}

func TestSummarizeImportedThread_SinglePassUnderCharBudget(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_3", "short thread summary"))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	msgs := turnMsgs(2)
	got, err := a.summarizeImportedThread(context.Background(), uuid.New(), msgs)
	require.NoError(t, err)
	require.Equal(t, "short thread summary", got)
}

func TestSummarizeImportedThread_MapReduceForLongTranscript(t *testing.T) {
	t.Parallel()
	srv := jsonResponsesServer(t, responseTextJSONBody("resp_4", "rolled-up summary"))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	// One oversized message forces buildTranscript() past rehydrationSinglePassChars, exercising the
	// chunk map + rollup-reduce path (chunkMessagesByChars + multiple summarizeText calls).
	msgs := []models.ChatMessage{
		{Message: strings.Repeat("x", rehydrationSinglePassChars+1), Origin: models.MessageOriginUser},
	}
	got, err := a.summarizeImportedThread(context.Background(), uuid.New(), msgs)
	require.NoError(t, err)
	require.Equal(t, "rolled-up summary", got)
}

// --- extractMemoriesFromTranscript ---

func TestExtractMemoriesFromTranscript_EmptyTranscriptReturnsNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	mems, err := a.extractMemoriesFromTranscript(context.Background(), uuid.New(), "   ")
	require.NoError(t, err)
	require.Nil(t, mems)
}

func TestExtractMemoriesFromTranscript_SuccessParsesMemories(t *testing.T) {
	t.Parallel()
	body := responseTextJSONBody("resp_5", `{\"memories\":[{\"content\":\"likes Go\",\"scope\":\"User\",\"confidence\":\"high\"}]}`)
	srv := jsonResponsesServer(t, body)
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	mems, err := a.extractMemoriesFromTranscript(context.Background(), uuid.New(), "User: I like Go.")
	require.NoError(t, err)
	require.Len(t, mems, 1)
	require.Equal(t, "likes Go", mems[0].Content)
}

func TestExtractMemoriesFromTranscript_ProviderErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	_, err := a.extractMemoriesFromTranscript(context.Background(), uuid.New(), "User: hi")
	require.ErrorContains(t, err, "openai memory extraction")
}

// --- extractAndStoreImportedMemories ---

func TestExtractAndStoreImportedMemories_NilProviderOrMemoryToolIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.extractAndStoreImportedMemories(context.Background(), uuid.New(), uuid.New(), turnMsgs(2))
	})
}

func TestExtractAndStoreImportedMemories_MockLLMSkipsExtraction(t *testing.T) {
	t.Parallel()
	// memoryTool is nil, but that guard is checked before nonVendorLLM(); use a provider so the
	// nonVendorLLM() skip itself is what's under test (no HTTP call should occur since there is no
	// server to receive one).
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider("http://127.0.0.1:0"), mockLLM: true}
	require.NotPanics(t, func() {
		a.extractAndStoreImportedMemories(context.Background(), uuid.New(), uuid.New(), turnMsgs(2))
	})
}
