package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

var errCoverageTestSentinel = errors.New("boom")

// --- withCallPath ---

func TestWithCallPath_AttachesCallPath(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	ctx := a.withCallPath(context.Background(), telemetry.CallPathUserChat)
	require.Equal(t, telemetry.CallPathUserChat, telemetry.CallPathFromContext(ctx))
}

// --- registerRunningJobCancel / unregisterRunningJobCancel / CancelJob ---

func newCancelTestAgent() *Agent {
	return &Agent{
		logger:            zap.NewNop(),
		runningJobCancels: make(map[uuid.UUID]runningJobCancel),
	}
}

func TestRegisterRunningJobCancel_IgnoresZeroValues(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()

	a.registerRunningJobCancel(uuid.Nil, uuid.New(), func() {})
	require.Empty(t, a.runningJobCancels)

	a.registerRunningJobCancel(uuid.New(), uuid.Nil, func() {})
	require.Empty(t, a.runningJobCancels)

	a.registerRunningJobCancel(uuid.New(), uuid.New(), nil)
	require.Empty(t, a.runningJobCancels)
}

func TestRegisterUnregisterRunningJobCancel_RoundTrip(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()
	jobID, userID := uuid.New(), uuid.New()

	a.registerRunningJobCancel(jobID, userID, func() {})
	require.Len(t, a.runningJobCancels, 1)

	a.unregisterRunningJobCancel(uuid.Nil)
	require.Len(t, a.runningJobCancels, 1, "nil job id must be a no-op")

	a.unregisterRunningJobCancel(jobID)
	require.Empty(t, a.runningJobCancels)
}

func TestCancelJob_NilIDsReturnUnauthorized(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()

	err := a.CancelJob(context.Background(), uuid.Nil, uuid.New())
	require.ErrorIs(t, err, datastore.ErrUnauthorized)

	err = a.CancelJob(context.Background(), uuid.New(), uuid.Nil)
	require.ErrorIs(t, err, datastore.ErrUnauthorized)
}

func TestCancelJob_UnknownJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()

	err := a.CancelJob(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
}

func TestCancelJob_WrongOwnerReturnsUnauthorized(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()
	jobID, ownerID := uuid.New(), uuid.New()
	a.registerRunningJobCancel(jobID, ownerID, func() {})

	err := a.CancelJob(context.Background(), uuid.New(), jobID)
	require.ErrorIs(t, err, datastore.ErrUnauthorized)
}

func TestCancelJob_OwnerCancelsAndRemainsRegistered(t *testing.T) {
	t.Parallel()
	a := newCancelTestAgent()
	jobID, ownerID := uuid.New(), uuid.New()
	called := false
	a.registerRunningJobCancel(jobID, ownerID, func() { called = true })

	err := a.CancelJob(context.Background(), ownerID, jobID)
	require.NoError(t, err)
	require.True(t, called, "cancel func must be invoked")
}

// --- ChunkPipeline / FileStore trivial accessors ---

func TestChunkPipelineAndFileStore_ReturnConfiguredFields(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.Nil(t, a.ChunkPipeline())
	require.Nil(t, a.FileStore())
}

// --- RecordFileUpload / recordCounter / recordTime / recordCountHistogram no-telemetry guards ---

func TestRecordFileUpload_NoopWithNilTelemetry(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.RecordFileUpload(context.Background(), "image/png", "success")
	})
}

func TestRecordTime_NoopWithNilTelemetry(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.NotPanics(t, func() {
		a.recordTime(context.Background(), "some_metric", 0)
	})
}

func TestRecordCounter_NoopWithNilTelemetry(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.NotPanics(t, func() {
		a.recordCounter(context.Background(), "some_counter", 1)
	})
}

func TestRecordCountHistogram_NoopWithNilTelemetry(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.NotPanics(t, func() {
		a.recordCountHistogram(context.Background(), "some_histogram", 1)
	})
}

func TestRecordTime_NoopWithNilMetrics(t *testing.T) {
	t.Parallel()
	a := &Agent{telemetry: &telemetry.Telemetry{}}
	require.NotPanics(t, func() {
		a.recordTime(context.Background(), "some_metric", 0)
		a.recordCounter(context.Background(), "some_counter", 1)
		a.recordCountHistogram(context.Background(), "some_histogram", 1)
	})
}

// --- mergedRitualIDsForTools / mergeRitualSets ---

func TestMergedRitualIDsForTools_NilChatMessageUsesMoodOnly(t *testing.T) {
	t.Parallel()
	moodRitual := uuid.New()
	mood := &models.Mood{RitualIDs: []uuid.UUID{moodRitual}}

	got := mergedRitualIDsForTools(nil, mood)
	require.Equal(t, []uuid.UUID{moodRitual}, got)
}

func TestMergedRitualIDsForTools_CombinesMessageAndMoodRituals(t *testing.T) {
	t.Parallel()
	msgRitualID := uuid.New()
	moodRitualID := uuid.New()
	chatMessage := &models.ChatMessage{
		Rituals: []*models.Ritual{{ID: msgRitualID}},
	}
	mood := &models.Mood{RitualIDs: []uuid.UUID{moodRitualID}}

	got := mergedRitualIDsForTools(chatMessage, mood)
	require.ElementsMatch(t, []uuid.UUID{msgRitualID, moodRitualID}, got)
}

func TestMergeRitualSets_NoExtraReturnsBase(t *testing.T) {
	t.Parallel()
	base := []*models.Ritual{{ID: uuid.New()}}
	got := mergeRitualSets(base, nil)
	require.Equal(t, base, got)
}

func TestMergeRitualSets_DedupesAndSkipsNils(t *testing.T) {
	t.Parallel()
	shared := uuid.New()
	onlyExtra := uuid.New()
	base := []*models.Ritual{{ID: shared}, nil}
	extra := []*models.Ritual{{ID: shared}, {ID: onlyExtra}, nil}

	got := mergeRitualSets(base, extra)
	require.Len(t, got, 2)
	ids := []uuid.UUID{got[0].ID, got[1].ID}
	require.ElementsMatch(t, []uuid.UUID{shared, onlyExtra}, ids)
}

// --- hasImageAttachmentsWithoutFileID ---

func TestHasImageAttachmentsWithoutFileID(t *testing.T) {
	t.Parallel()

	fid := "file-1"
	blank := ""
	cases := []struct {
		name string
		atts []*models.FileAttachment
		want bool
	}{
		{"empty", nil, false},
		{"nil entry skipped", []*models.FileAttachment{nil}, false},
		{"non-image ignored", []*models.FileAttachment{{FileType: "application/pdf"}}, false},
		{"image with file id", []*models.FileAttachment{{FileType: "image/png", FileID: &fid}}, false},
		{"image without file id", []*models.FileAttachment{{FileType: "image/png"}}, true},
		{"image with blank file id", []*models.FileAttachment{{FileType: "image/png", FileID: &blank}}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, hasImageAttachmentsWithoutFileID(tc.atts))
		})
	}
}

// --- isFirstCheckpoint / maxTurnsBeforeCheckpoint ---

func TestIsFirstCheckpoint(t *testing.T) {
	t.Parallel()
	require.True(t, isFirstCheckpoint(&chatContext{chat: &models.Chat{CheckpointSummary: ""}}))
	require.False(t, isFirstCheckpoint(&chatContext{chat: &models.Chat{CheckpointSummary: "summary so far"}}))
}

func TestMaxTurnsBeforeCheckpoint(t *testing.T) {
	t.Parallel()
	require.Equal(t, checkpointMaxAssistantMessagesSinceStart,
		maxTurnsBeforeCheckpoint(&chatContext{chat: &models.Chat{}}))
	require.Equal(t, checkpointMaxAssistantMessagesSinceSummary,
		maxTurnsBeforeCheckpoint(&chatContext{chat: &models.Chat{CheckpointSummary: "x"}}))
}

// --- newJobDraftDeltaBuffer / HandleDelta / Flush / persist ---

func TestNewJobDraftDeltaBuffer_NilInputsReturnEmptyBuffer(t *testing.T) {
	t.Parallel()

	b := newJobDraftDeltaBuffer(nil, nil, zap.NewNop(), nil, 96, time.Second)
	require.Nil(t, b.ds)

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b2 := newJobDraftDeltaBuffer(nil, nil, zap.NewNop(), job, 96, time.Second)
	require.Nil(t, b2.ds, "nil datastore keeps buffer inert even with a valid job")
}

func TestNewJobDraftDeltaBuffer_DefaultsInvalidConfig(t *testing.T) {
	t.Parallel()

	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(nil, ds, zap.NewNop(), job, 0, 0)
	require.NotNil(t, b.ds)
	require.Equal(t, 1, b.minChunkChars)
	require.Equal(t, 250*time.Millisecond, b.maxWait)
	require.NotNil(t, b.persistParent, "nil persistParent should default to Background")
}

func TestJobDraftDeltaBuffer_HandleDelta_NilOrEmptyDeltaIsNoOp(t *testing.T) {
	t.Parallel()

	var nilBuf *jobDraftDeltaBuffer
	require.NotPanics(t, func() { nilBuf.HandleDelta("hi") })
	require.NotPanics(t, func() { nilBuf.Flush() })

	emptyDSBuf := &jobDraftDeltaBuffer{}
	require.NotPanics(t, func() { emptyDSBuf.HandleDelta("hi") })
	require.NotPanics(t, func() { emptyDSBuf.Flush() })

	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()
	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(context.Background(), ds, zap.NewNop(), job, 96, time.Hour)
	b.HandleDelta("")
	require.Empty(t, b.allText)
}

func TestJobDraftDeltaBuffer_HandleDelta_FlushesOnMinChars(t *testing.T) {
	t.Parallel()

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*").
		WillReturnResult(sqlmock.NewResult(0, 1))

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(context.Background(), ds, zap.NewNop(), job, 3, time.Hour)

	b.HandleDelta("hi") // below threshold, no flush yet
	require.Equal(t, "hi", b.pending)

	b.HandleDelta("!") // now >= 3 chars, triggers flush
	require.Empty(t, b.pending)
	require.Equal(t, "hi!", b.allText)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJobDraftDeltaBuffer_HandleDelta_FlushesOnMaxWait(t *testing.T) {
	t.Parallel()

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*").
		WillReturnResult(sqlmock.NewResult(0, 1))

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(context.Background(), ds, zap.NewNop(), job, 999, time.Millisecond)
	b.lastFlush = time.Now().Add(-time.Hour) // force maxWait to have elapsed

	b.HandleDelta("x")
	require.Empty(t, b.pending, "elapsed maxWait should force a flush even under minChunkChars")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJobDraftDeltaBuffer_Flush_NoOpWhenPendingEmpty(t *testing.T) {
	t.Parallel()

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(context.Background(), ds, zap.NewNop(), job, 96, time.Hour)
	b.Flush()
	require.NoError(t, mock.ExpectationsWereMet(), "no exec expected when nothing pending")
}

func TestJobDraftDeltaBuffer_Flush_PersistsPending(t *testing.T) {
	t.Parallel()

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*").
		WillReturnResult(sqlmock.NewResult(0, 1))

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(context.Background(), ds, zap.NewNop(), job, 999, time.Hour)
	b.HandleDelta("partial") // stays pending (below min chars, maxWait not elapsed)
	require.Equal(t, "partial", b.pending)

	b.Flush()
	require.Empty(t, b.pending)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJobDraftDeltaBuffer_Persist_LogsErrorButDoesNotPanic(t *testing.T) {
	t.Parallel()

	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*").
		WillReturnError(errCoverageTestSentinel)

	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}
	b := newJobDraftDeltaBuffer(context.Background(), ds, zap.NewNop(), job, 96, time.Hour)
	require.NotPanics(t, func() { b.persist("chunk") })
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- assertUserCanRunChatModel ---

func TestAssertUserCanRunChatModel_NilDsOrChatOrNonExperimentalIsAllowed(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	// ds == nil short-circuits.
	require.NoError(t, a.assertUserCanRunChatModel(context.Background(), uuid.New(), &models.Chat{ModelID: uuid.New()}, "openai"))

	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()
	a2 := newTestAgent(ds)

	// parentChat == nil short-circuits.
	require.NoError(t, a2.assertUserCanRunChatModel(context.Background(), uuid.New(), nil, "openai"))

	// Every currently-known provider is non-experimental (see
	// models.IsExperimentalModelRecord), so this always returns nil today even
	// with a non-nil chat carrying a ModelID. This documents that the
	// experimental-gating branch below the early return is currently dead code.
	require.NoError(t, a2.assertUserCanRunChatModel(context.Background(), uuid.New(), &models.Chat{ModelID: uuid.New()}, "openai"))
}

func TestAssertUserCanRunChatModel_ZeroModelIDIsAllowed(t *testing.T) {
	t.Parallel()
	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()
	a := newTestAgent(ds)

	require.NoError(t, a.assertUserCanRunChatModel(context.Background(), uuid.New(), &models.Chat{}, "openai"))
}

// --- resolvePersonalityName ---

func TestResolvePersonalityName_NilPersonalityIDReturnsEmpty(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.Equal(t, "", a.resolvePersonalityName(context.Background(), uuid.New(), uuid.Nil))
}

func TestResolvePersonalityName_LookupErrorReturnsEmpty(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").WillReturnError(errCoverageTestSentinel)
	mock.ExpectRollback()

	a := newTestAgent(ds)
	got := a.resolvePersonalityName(context.Background(), uuid.New(), uuid.New())
	require.Equal(t, "", got)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- updateJobStatus ---

func TestUpdateJobStatus_ReturnsWrappedErrorOnFailure(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	userID, jobID := uuid.New(), uuid.New()
	job := &models.Job{ID: jobID, UserID: userID, JobType: JobTypeChatMessage, Status: models.JobStatusPending}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	err := a.updateJobStatus(context.Background(), job, models.JobStatusProcessing)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to update job status")
}

// --- setJobStatusFailed ---

func TestSetJobStatusFailed_NilJobIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.setJobStatusFailed(context.Background(), nil, errCoverageTestSentinel)
	})
}

func TestSetJobStatusFailed_LogsAndReturnsWhenUpdateJobFails(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	userID, jobID := uuid.New(), uuid.New()
	job := &models.Job{ID: jobID, UserID: userID, JobType: JobTypeChatMessage, Status: models.JobStatusProcessing}

	// UpdateJob's authorization Exist() check reports the job missing, so
	// UpdateJob fails fast and setJobStatusFailed must return without touching
	// draft deltas or the user message (no further mock expectations set).
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	a.setJobStatusFailed(context.Background(), job, errCoverageTestSentinel)
	// job left unmodified since UpdateJob never succeeded.
	require.Equal(t, models.JobStatusProcessing, job.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- setJobStatusFailedWithPartial ---

func TestSetJobStatusFailedWithPartial_NilJobOrCauseIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	require.NotPanics(t, func() {
		a.setJobStatusFailedWithPartial(context.Background(), nil, &models.ChatMessage{}, &chatContext{}, errCoverageTestSentinel)
		a.setJobStatusFailedWithPartial(context.Background(), &models.Job{}, &models.ChatMessage{}, &chatContext{}, nil)
	})
}

func TestSetJobStatusFailedWithPartial_NilUserMessageFallsBackToSetJobStatusFailed(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	job := &models.Job{ID: uuid.New(), UserID: uuid.New(), JobType: JobTypeChatMessage, Status: models.JobStatusProcessing}

	// setJobStatusFailedWithPartial falls back to setJobStatusFailed when
	// userMessage is nil, whose UpdateJob call fails its Exist() authorization
	// check here, so no further mock expectations are required.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	a := newTestAgent(ds)
	require.NotPanics(t, func() {
		a.setJobStatusFailedWithPartial(context.Background(), job, nil, &chatContext{}, errCoverageTestSentinel)
	})
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- recordToolCalls / recordModelContextSegmentEstimates / recordToolDefinitionEstimate ---

func TestRecordToolCalls_NoopWithNilTelemetry(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.NotPanics(t, func() {
		a.recordToolCalls(context.Background(), []*models.ToolCall{
			{ToolName: "web_search"},
			{ToolName: "recall", ToolError: "boom"},
		})
	})
}

func TestRecordModelContextSegmentEstimates_NilGuards(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.NotPanics(t, func() {
		a.recordModelContextSegmentEstimates(context.Background(), nil)
	})

	a2 := &Agent{telemetry: &telemetry.Telemetry{}}
	require.NotPanics(t, func() {
		a2.recordModelContextSegmentEstimates(context.Background(), &provider.ModelContext{})
	})
}

func TestRecordToolDefinitionEstimate_NilGuards(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	require.NotPanics(t, func() {
		a.recordToolDefinitionEstimate(nil, "anything")
		a.recordToolDefinitionEstimate(&provider.ModelContext{}, nil)
	})
}

// --- persistContextBreakdown ---

func TestPersistContextBreakdown_NilAgentMessageIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	require.NotPanics(t, func() {
		a.persistContextBreakdown(context.Background(), uuid.New(), nil, &chatContext{}, &provider.ModelContext{}, nil)
	})
}

// --- runCheckpointClaude nil guards ---

func TestRunCheckpointClaude_NilAgentMessageOrModelContextIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}
	chatMessage := &models.ChatMessage{ChatID: uuid.New()}

	require.NotPanics(t, func() {
		a.runCheckpointClaude(context.Background(), uuid.New(), chatMessage, nil, &chatContext{chat: &models.Chat{}}, 1, &provider.ModelContext{}, "reason")
		a.runCheckpointClaude(context.Background(), uuid.New(), chatMessage, &models.ChatMessage{}, &chatContext{chat: &models.Chat{}}, 1, nil, "reason")
	})
}

// --- loadMoodRituals ---

func TestLoadMoodRituals_NilOrEmptyMoodReturnsNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	require.Nil(t, a.loadMoodRituals(context.Background(), uuid.New(), nil))
	require.Nil(t, a.loadMoodRituals(context.Background(), uuid.New(), &models.Mood{}))
}

// --- StartSummaryMemoryBackfill ---

func TestStartSummaryMemoryBackfill_RunsInBackgroundWithoutPanicking(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	require.NotPanics(t, func() {
		a.StartSummaryMemoryBackfill(context.Background())
	})
	// memoryTool is nil, so the background goroutine's BackfillSummaryMemories
	// call returns immediately (Skipped++); give it a moment to finish logging
	// before the test process exits, since it races with nothing else here.
	time.Sleep(50 * time.Millisecond)
}

// --- setJobStatusCancelledWithPartial ---

func TestSetJobStatusCancelledWithPartial_NilJobOrUserMessageReturnsNilNil(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	id, err := a.setJobStatusCancelledWithPartial(context.Background(), nil, &models.ChatMessage{}, nil)
	require.NoError(t, err)
	require.Nil(t, id)

	id, err = a.setJobStatusCancelledWithPartial(context.Background(), &models.Job{}, nil, nil)
	require.NoError(t, err)
	require.Nil(t, id)
}

// --- recordCancelledChatUsage ---

func TestRecordCancelledChatUsage_NilArgsAreNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{logger: zap.NewNop()}

	require.NotPanics(t, func() {
		a.recordCancelledChatUsage(context.Background(), nil, &models.ChatMessage{}, &chatContext{}, nil, metering.Decision{}, errCoverageTestSentinel, nil)
		a.recordCancelledChatUsage(context.Background(), &models.Job{}, nil, &chatContext{}, nil, metering.Decision{}, errCoverageTestSentinel, nil)
		a.recordCancelledChatUsage(context.Background(), &models.Job{}, &models.ChatMessage{}, nil, nil, metering.Decision{}, errCoverageTestSentinel, nil)
	})
}
