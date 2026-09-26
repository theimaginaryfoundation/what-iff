package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

const oneAnthropicConversation = `[
	{
		"uuid": "22222222-2222-4222-8222-222222222222",
		"name": "Claude Chat",
		"created_at": "2025-08-03T03:00:06.253385Z",
		"chat_messages": [
			{"uuid":"a","sender":"human","text":"Hello","created_at":"2025-08-03T03:00:06.97Z","content":[{"type":"text","text":"Hello"}]},
			{"uuid":"b","sender":"assistant","text":"Hi","created_at":"2025-08-03T03:00:10.09Z","content":[{"type":"text","text":"Hi"}]}
		]
	}
]`

// waitForJobDuration waits for the import job's JobDuration point: the store sees the terminal
// status slightly before runChatImport returns and records it.
func waitForJobDuration(t *testing.T, tm *telemetrytest.Recorder, outcome string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return tm.HistogramCount(t, telemetry.JobDuration.Name,
			telemetry.AttrJobType.String(JobTypeChatImport), telemetry.AttrOutcome.String(outcome)) == 1
	}, 5*time.Second, 10*time.Millisecond)
}

func TestImportChats_RecordsJobAndItemMetrics(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	store := newImportStore(func(_ context.Context, _ uuid.UUID, convs []models.ImportConversation) (*models.ImportResult, error) {
		return &models.ImportResult{Imported: len(convs), Skipped: 2, Errors: []string{"one bad"}}, nil
	})
	router := setupImportRouter(store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, importRequest(t, uuid.New(), []byte(oneAnthropicConversation)))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	store.waitDone(t)
	waitForJobDuration(t, tm, telemetry.JobOutcomeSuccess)

	op := telemetry.AttrOperation.String(telemetry.FileOpChatImport)
	require.Equal(t, float64(len(oneAnthropicConversation)), tm.HistogramSum(t, telemetry.FileSize.Name, op, telemetry.AttrKind.String("text")))
	for _, stage := range []string{telemetry.FileStageParse, telemetry.FileStageInsert} {
		require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileOperationDuration.Name, op, telemetry.AttrStage.String(stage)), stage)
	}
	conv := telemetry.AttrKind.String(telemetry.FileItemConversation)
	for outcome, want := range map[string]float64{
		telemetry.FileItemImported: 1,
		telemetry.FileItemSkipped:  2,
		telemetry.FileItemFailed:   1,
	} {
		require.Equal(t, want, tm.HistogramSum(t, telemetry.FileOperationItems.Name, op, conv, telemetry.AttrOutcome.String(outcome)), outcome)
	}
	require.Equal(t, int64(0), tm.CounterValue(t, telemetry.JobsInFlight.Name, telemetry.AttrJobType.String(JobTypeChatImport)))
}

func TestImportChats_DatastoreFailureRecordsFailedJob(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	store := newImportStore(func(context.Context, uuid.UUID, []models.ImportConversation) (*models.ImportResult, error) {
		return nil, errors.New("db down")
	})
	router := setupImportRouter(store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, importRequest(t, uuid.New(), []byte(oneAnthropicConversation)))
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	store.waitDone(t)
	waitForJobDuration(t, tm, telemetry.JobOutcomeFailed)

	require.Equal(t, []string{telemetry.ErrorTypeOther}, tm.AttributeValues(t, telemetry.FileOperationDuration.Name, telemetry.AttrErrorType))
	require.Zero(t, tm.HistogramCount(t, telemetry.FileOperationItems.Name), "no result, no item counts")
}
