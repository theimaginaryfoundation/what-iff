package datastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/params"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	mt "github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

func ptr[T any](v T) *T { return &v }

func newsJobModel() models.AgentJob {
	job := baseAgentJobModel()
	job.Prompt = "Summarise news on {{topics}} in {{region}}."
	job.Parameters = []mt.JobParameter{
		{Name: "topics", Type: mt.JobParameterList},
		{Name: "region", Type: mt.JobParameterSingleSelect, Options: []string{"US", "EU"}},
	}
	return job
}

func TestCreateAgentJob_ParametersWithoutValuesNeedValues(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createAgentJobTestUser(t, ds)

	created, err := ds.CreateAgentJob(ctx, userID, newsJobModel())
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusNeedsValues, created.Status)
	require.Equal(t, []string{"topics", "region"}, created.MissingParameters)
	require.Equal(t, mt.JobParameterFormatBullets, created.Parameters[0].Format, "list defaults to bullets")

	// Reloaded from the database, the JSON columns round-trip.
	got, err := ds.GetAgentJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.Parameters, got.Parameters)
	require.Equal(t, []string{"topics", "region"}, got.MissingParameters)
}

func TestCreateAgentJob_UndefinedPlaceholderRejected(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	userID := createAgentJobTestUser(t, ds)

	job := baseAgentJobModel()
	job.Prompt = "News on {{topics}}"
	_, err := ds.CreateAgentJob(context.Background(), userID, job)
	var verr *params.ValidationError
	require.ErrorAs(t, err, &verr)
	require.Equal(t, []string{"topics"}, verr.Undefined)
}

func TestCreateAgentJob_JobWithoutParametersKeepsLegacyShape(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	userID := createAgentJobTestUser(t, ds)

	created, err := ds.CreateAgentJob(context.Background(), userID, baseAgentJobModel())
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusActive, created.Status)
	require.NotNil(t, created.Parameters)
	require.NotNil(t, created.ParameterValues)
	require.Empty(t, created.MissingParameters)
}

func TestUpdateAgentJobParameters_FillingValuesActivates(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, newsJobModel())
	require.NoError(t, err)

	// One value: still waiting.
	got, err := ds.UpdateAgentJobParameters(ctx, userID, created.ID, models.AgentJobParametersPatch{
		ParameterValues: &mt.JobParameterValues{"topics": mt.ListValue("AI", "climate")},
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusNeedsValues, got.Status)
	require.Equal(t, []string{"region"}, got.MissingParameters)

	// Both: active.
	got, err = ds.UpdateAgentJobParameters(ctx, userID, created.ID, models.AgentJobParametersPatch{
		ParameterValues: &mt.JobParameterValues{"topics": mt.ListValue("AI", "climate"), "region": mt.TextValue("EU")},
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusActive, got.Status)
	require.Empty(t, got.MissingParameters)

	// Removing an option the value uses drops the value and the job waits again.
	defs := newsJobModel().Parameters
	defs[1].Options = []string{"US"}
	got, err = ds.UpdateAgentJobParameters(ctx, userID, created.ID, models.AgentJobParametersPatch{Parameters: &defs})
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusNeedsValues, got.Status)
	require.Equal(t, []string{"region"}, got.MissingParameters)
	_, kept := got.ParameterValues["topics"]
	require.True(t, kept)
}

func TestUpdateAgentJobParameters_PausedStaysPaused(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createAgentJobTestUser(t, ds)
	job := newsJobModel()
	job.ParameterValues = mt.JobParameterValues{"topics": mt.ListValue("AI"), "region": mt.TextValue("US")}
	created, err := ds.CreateAgentJob(ctx, userID, job)
	require.NoError(t, err)
	_, err = ds.UpdateAgentJobStatus(ctx, userID, created.ID, models.AgentJobStatusPaused, "")
	require.NoError(t, err)

	got, err := ds.UpdateAgentJobParameters(ctx, userID, created.ID, models.AgentJobParametersPatch{
		ParameterValues: &mt.JobParameterValues{},
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusPaused, got.Status)
	require.Equal(t, []string{"topics", "region"}, got.MissingParameters)

	// Resuming is refused while values are missing.
	_, err = ds.UpdateAgentJobStatus(ctx, userID, created.ID, models.AgentJobStatusActive, "")
	require.ErrorIs(t, err, ErrAgentJobNeedsValues)
	var missing *params.MissingError
	require.True(t, errors.As(err, &missing))
	require.Equal(t, []string{"topics", "region"}, missing.Names)
}

func TestUpdateAgentJobParameters_UndefinedPlaceholderChangesNothing(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateAgentJobParameters(ctx, userID, created.ID, models.AgentJobParametersPatch{Prompt: ptr("Now with {{x}}")})
	var verr *params.ValidationError
	require.ErrorAs(t, err, &verr)

	got, err := ds.GetAgentJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, "do the thing", got.Prompt)
}

func TestUpdateAgentJobSchedule_ReactivatedJobStillNeedsValues(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createAgentJobTestUser(t, ds)
	job := newsJobModel()
	job.ParameterValues = mt.JobParameterValues{"topics": mt.ListValue("AI"), "region": mt.TextValue("US")}
	created, err := ds.CreateAgentJob(ctx, userID, job)
	require.NoError(t, err)
	_, err = ds.UpdateAgentJobStatus(ctx, userID, created.ID, models.AgentJobStatusComplete, "")
	require.NoError(t, err)
	// Paused/complete jobs keep their status when values are cleared.
	_, err = ds.UpdateAgentJobParameters(ctx, userID, created.ID, models.AgentJobParametersPatch{ParameterValues: &mt.JobParameterValues{}})
	require.NoError(t, err)

	next := time.Now().Add(time.Hour)
	got, err := ds.UpdateAgentJobSchedule(ctx, userID, created.ID, "in an hour", models.AgentJobScheduleTypeAt, nil, &next, "UTC", &next)
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusNeedsValues, got.Status, "a rescheduled job missing values waits for them instead of going active")
}
