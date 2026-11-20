package agent

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestRestoreGuidance_ReadyGateAndPrompt(t *testing.T) {
	require.Empty(t, restoreGuidanceContext(" \n "))
	require.Equal(t, "base", summaryInstructionsWithRestoreGuidance("base", ""))
	guidance := "Keep decisions. Omit abandoned plans."
	prompt := summaryInstructionsWithRestoreGuidance("base", guidance)
	require.Contains(t, prompt, guidance)
	require.Contains(t, prompt, "not facts from the transcript")
	require.Contains(t, prompt, "do not repeat omitted details")
	for _, state := range []string{"", models.RehydrationStateStale, models.RehydrationStatePending, models.RehydrationStateProcessing, models.RehydrationStateFailed} {
		require.Error(t, requireGuidedRestoreReady(&models.Chat{RestoreGuidance: guidance, RehydrationState: state}), state)
	}
	require.NoError(t, requireGuidedRestoreReady(&models.Chat{RestoreGuidance: guidance, RehydrationState: models.RehydrationStateReady}))
	require.NoError(t, requireGuidedRestoreReady(&models.Chat{RehydrationState: models.RehydrationStateFailed}))
}

func TestRestoredWindowStart_GuidedOmitsAllOldRawTurns(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	msgs := []models.ChatMessage{{SentAt: at}, {SentAt: at.Add(time.Second)}}
	require.Equal(t, msgs[0].SentAt, restoredWindowStart(msgs, 0, false))
	cutoff := restoredWindowStart(msgs, 0, true)
	require.True(t, cutoff.After(msgs[len(msgs)-1].SentAt))
	require.Empty(t, messagesFrom(msgs, cutoff), "even recent raw turns must not bypass the guided summary")
}

// A short native conversation must still be rebuilt when the user supplied guidance, even if
// an older checkpoint exists. No recent raw turns or old summary are retained in live context.
func TestRunThreadRehydration_GuidedShortThreadReplacesOldWindow(t *testing.T) {
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	msgs := turnMsgs(2)
	expectJobStatusUpdateNotFound(mock)
	mock.ExpectExec("UPDATE .*chats.*").WillReturnResult(sqlmock.NewResult(0, 1))
	expectGetChatMessagesForSummarySuccess(mock, msgs)
	mock.ExpectQuery("SELECT .*").WillReturnRows(sqlmock.NewRows([]string{"checkpoint_summary", "last_checkpoint_at", "restore_guidance"}).AddRow("stale old summary", msgs[0].SentAt, "Keep decisions. Omit drafts."))
	mock.ExpectExec("UPDATE .*chats.*").WithArgs(
		sqlmock.AnyArg(), "Mock rehydration summary of 4 imported messages.", 2,
		msgs[len(msgs)-1].SentAt.Add(time.Microsecond), "ready", sqlmock.AnyArg(), sqlmock.AnyArg(),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	expectJobStatusUpdateNotFound(mock)
	a := newTestAgent(ds)
	a.mockLLM = true
	require.Equal(t, telemetry.JobOutcomeSuccess, a.runThreadRehydration(context.Background(), uuid.New(), uuid.New(), uuid.New()))
	require.NoError(t, mock.ExpectationsWereMet())
}
