package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

func TestJobTelemetry_EnqueueAndAgeAtStatus(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	ds.metrics = tm.Metrics
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateJobStatus(ctx, userID, created.ID, models.JobStatusProcessing, "")
	require.NoError(t, err)
	// Writing the same status again is not a status change and must not count twice.
	_, err = ds.UpdateJobStatus(ctx, userID, created.ID, models.JobStatusProcessing, "")
	require.NoError(t, err)

	toSave := *created
	toSave.Status = models.JobStatusInferenceComplete
	_, err = ds.UpdateJob(ctx, userID, toSave)
	require.NoError(t, err)

	_, err = ds.SetJobResult(ctx, userID, created.ID, uuid.New())
	require.NoError(t, err)

	chatType := telemetry.AttrJobType.String("chat_message")
	require.Equal(t, int64(1), tm.CounterValue(t, telemetry.JobsEnqueued.Name, chatType))
	age := telemetry.JobAgeAtStatus.Name
	for _, status := range []string{"processing", "inference_complete", "complete"} {
		require.Equal(t, uint64(1), tm.HistogramCount(t, age, chatType, telemetry.AttrStatus.String(status)), status)
	}
	require.ElementsMatch(t, []string{"processing", "inference_complete", "complete"},
		tm.AttributeValues(t, age, telemetry.AttrStatus))
}

func TestJobTelemetry_UpdateMissingJobRecordsNothing(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	ds.metrics = tm.Metrics

	_, err := ds.UpdateJobStatus(context.Background(), createJobTestUser(t, ds), uuid.New(), models.JobStatusFailed, "x")
	require.ErrorIs(t, err, ErrJobNotFound)
	require.Equal(t, uint64(0), tm.HistogramCount(t, telemetry.JobAgeAtStatus.Name))
}

func TestJobTelemetry_MarkChatJobCancelledRecordsAge(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	tm := telemetrytest.New(t)
	ds.metrics = tm.Metrics
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	changed, err := ds.MarkChatJobCancelled(ctx, userID, created.ID)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.JobAgeAtStatus.Name,
		telemetry.AttrJobType.String("chat_message"), telemetry.AttrStatus.String("cancelled")))
}

func TestJobBacklog_GroupsUnfinishedJobsAndFeedsGauges(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createJobTestUser(t, ds)

	for i := 0; i < 2; i++ {
		_, err := ds.CreateJob(ctx, userID, baseJobModel())
		require.NoError(t, err)
	}
	importJob := baseJobModel()
	importJob.JobType = "chat_import"
	importJob.Status = models.JobStatusProcessing
	_, err := ds.CreateJob(ctx, userID, importJob)
	require.NoError(t, err)
	done := baseJobModel()
	done.Status = models.JobStatusComplete
	_, err = ds.CreateJob(ctx, userID, done)
	require.NoError(t, err)

	groups, err := ds.JobBacklog(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 2, "terminal jobs are not backlog")
	for _, g := range groups {
		require.False(t, g.OldestAt.IsZero())
	}

	tm := telemetrytest.New(t)
	_, err = ds.RegisterJobBacklogGauges(tm.Metrics)
	require.NoError(t, err)
	v, ok := tm.GaugeValue(t, telemetry.JobsBacklog.Name,
		telemetry.AttrJobType.String("chat_message"), telemetry.AttrStatus.String("pending"))
	require.True(t, ok)
	require.Equal(t, 2.0, v)
	v, ok = tm.GaugeValue(t, telemetry.JobsBacklog.Name,
		telemetry.AttrJobType.String("chat_import"), telemetry.AttrStatus.String("processing"))
	require.True(t, ok)
	require.Equal(t, 1.0, v)
	age, ok := tm.GaugeValue(t, telemetry.JobsOldestAge.Name,
		telemetry.AttrJobType.String("chat_message"), telemetry.AttrStatus.String("pending"))
	require.True(t, ok)
	require.GreaterOrEqual(t, age, 0.0)
	require.ElementsMatch(t, []string{"pending", "processing"}, tm.AttributeValues(t, telemetry.JobsBacklog.Name, telemetry.AttrStatus))
}
