package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/params"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"go.uber.org/zap"
)

// fakeJobStore is an in-memory agentJobUpdateStore holding one user's jobs and chats.
type fakeJobStore struct {
	user   uuid.UUID
	jobs   map[uuid.UUID]*models.AgentJob
	chats  map[uuid.UUID]*models.Chat
	writes []string
	failOn string // a write that fails, by name
}

func newFakeJobStore(user uuid.UUID) *fakeJobStore {
	return &fakeJobStore{user: user, jobs: map[uuid.UUID]*models.AgentJob{}, chats: map[uuid.UUID]*models.Chat{}}
}

func (f *fakeJobStore) job(userID, id uuid.UUID) (*models.AgentJob, error) {
	j, ok := f.jobs[id]
	if !ok || userID != f.user {
		return nil, datastore.ErrAgentJobNotFound
	}
	return j, nil
}

func (f *fakeJobStore) write(name string) error {
	f.writes = append(f.writes, name)
	if f.failOn == name {
		return errors.New("pq: connection refused at 10.0.0.5")
	}
	return nil
}

func (f *fakeJobStore) GetChat(_ context.Context, userID, id uuid.UUID) (*models.Chat, error) {
	c, ok := f.chats[id]
	if !ok || userID != f.user {
		return nil, datastore.ErrChatNotFound
	}
	return c, nil
}

func (f *fakeJobStore) GetAgentJob(_ context.Context, userID, id uuid.UUID) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	cp := *j
	cp.Rituals = append([]*models.Ritual(nil), j.Rituals...)
	return &cp, nil
}

func (f *fakeJobStore) UpdateAgentJobTitle(_ context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	if err := f.write("title"); err != nil {
		return nil, err
	}
	j.Title = title
	return j, nil
}

func (f *fakeJobStore) UpdateAgentJobParameters(_ context.Context, userID, id uuid.UUID, patch models.AgentJobParametersPatch) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	if err := f.write("prompt"); err != nil {
		return nil, err
	}
	prompt, defs, values := j.Prompt, j.Parameters, j.ParameterValues
	if patch.Prompt != nil {
		prompt = *patch.Prompt
	}
	if patch.Parameters != nil {
		defs = *patch.Parameters
	}
	if patch.ParameterValues != nil {
		values = *patch.ParameterValues
	}
	defs, values, err = params.Prepare(prompt, defs, values)
	if err != nil {
		return nil, err
	}
	j.Prompt, j.Parameters, j.ParameterValues = prompt, defs, values
	j.MissingParameters = params.Missing(defs, values)
	j.Status = params.StatusAfterSave(j.Status, len(j.MissingParameters) > 0)
	return j, nil
}

func (f *fakeJobStore) SetAgentJobChat(_ context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	if err := f.write("thread"); err != nil {
		return nil, err
	}
	j.ChatID = chatID
	return j, nil
}

func (f *fakeJobStore) SetAgentJobOverrides(_ context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	if err := f.write("model"); err != nil {
		return nil, err
	}
	if patch.UpdateModel {
		j.ModelID = patch.ModelID
	}
	return j, nil
}

func (f *fakeJobStore) UpdateAgentJobSchedule(_ context.Context, userID, id uuid.UUID, input string, typ models.AgentJobScheduleType, sched *string, runAt *time.Time, tz string, next *time.Time) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	if err := f.write("schedule"); err != nil {
		return nil, err
	}
	j.ScheduleInput, j.ScheduleType, j.Schedule, j.RunAt, j.Timezone, j.NextRunAt = input, typ, sched, runAt, tz, next
	if j.Status == models.AgentJobStatusComplete && next != nil {
		j.Status = models.AgentJobStatusActive
	}
	return j, nil
}

func (f *fakeJobStore) UpdateAgentJobStatus(_ context.Context, userID, id uuid.UUID, status models.AgentJobStatus, _ string) (*models.AgentJob, error) {
	j, err := f.job(userID, id)
	if err != nil {
		return nil, err
	}
	if missing := params.Missing(j.Parameters, j.ParameterValues); status == models.AgentJobStatusActive && len(missing) > 0 {
		return nil, fmt.Errorf("%w: %w", datastore.ErrAgentJobNeedsValues, &params.MissingError{Names: missing})
	}
	if err := f.write("status"); err != nil {
		return nil, err
	}
	j.Status = status
	return j, nil
}

func (f *fakeJobStore) AddAgentJobRitual(_ context.Context, userID, jobID, ritualID uuid.UUID) error {
	j, err := f.job(userID, jobID)
	if err != nil {
		return err
	}
	if err := f.write("add skill"); err != nil {
		return err
	}
	j.Rituals = append(j.Rituals, &models.Ritual{ID: ritualID})
	return nil
}

func (f *fakeJobStore) RemoveAgentJobRitual(_ context.Context, userID, jobID, ritualID uuid.UUID) error {
	j, err := f.job(userID, jobID)
	if err != nil {
		return err
	}
	if err := f.write("remove skill"); err != nil {
		return err
	}
	kept := j.Rituals[:0]
	for _, r := range j.Rituals {
		if r.ID != ritualID {
			kept = append(kept, r)
		}
	}
	j.Rituals = kept
	return nil
}

type jobUpdateFixture struct {
	store   *fakeJobStore
	chat    *models.Chat
	job     *models.AgentJob
	parsed  []string // schedule inputs the parser was asked to read
	tzs     []string
	models  map[string]uuid.UUID
	updater *agentJobUpdater
}

func newJobUpdateFixture(t *testing.T) *jobUpdateFixture {
	t.Helper()
	user := uuid.New()
	f := &jobUpdateFixture{
		store:  newFakeJobStore(user),
		chat:   &models.Chat{ID: uuid.New(), UserID: user},
		models: map[string]uuid.UUID{"claude-opus-5-5": uuid.New()},
	}
	title := "Morning check-in"
	f.job = &models.AgentJob{
		ID: uuid.New(), UserID: user, Title: &title, Prompt: "Ask how Gori slept",
		ScheduleInput: "every day at 8am", ScheduleType: models.AgentJobScheduleTypeCron,
		Timezone: "America/New_York", Status: models.AgentJobStatusActive,
	}
	f.store.jobs[f.job.ID] = f.job
	f.store.chats[f.chat.ID] = f.chat
	f.updater = &agentJobUpdater{
		store: f.store,
		parseSchedule: func(_ context.Context, _ uuid.UUID, input, tz string, now time.Time) (*models.AgentJobSchedulePreview, error) {
			f.parsed, f.tzs = append(f.parsed, input), append(f.tzs, tz)
			if input == "nonsense" {
				return nil, errors.New("could not understand that schedule")
			}
			cron := "0 9 * * *"
			next := now.Add(time.Hour)
			return &models.AgentJobSchedulePreview{ScheduleType: models.AgentJobScheduleTypeCron, Schedule: &cron,
				Timezone: tz, HumanSummary: "Every day at 9:00 AM", NextRuns: []time.Time{next}}, nil
		},
		resolveModel: func(_ context.Context, ref string) (uuid.UUID, error) {
			if id, ok := f.models[ref]; ok {
				return id, nil
			}
			return uuid.Nil, errors.New("not found")
		},
		logger: zap.NewNop(),
	}
	return f
}

func (f *jobUpdateFixture) run(t *testing.T, args string) updateAgentJobToolResult {
	t.Helper()
	out, err := f.updater.update(context.Background(), f.chat, []byte(args))
	require.NoError(t, err, "a bad argument is a result for the model, not a Go error")
	var res updateAgentJobToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestUpdateAgentJobChangesOnlyTheFieldsPassed(t *testing.T) {
	f := newJobUpdateFixture(t)

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"prompt":"  Ask how Gori slept, and about the dream journal  "}`, f.job.ID))

	require.True(t, res.Success, res.Error)
	assert.Equal(t, []string{"prompt"}, res.Updated)
	assert.Equal(t, []string{"prompt"}, f.store.writes, "nothing else is written")
	assert.Equal(t, "Ask how Gori slept, and about the dream journal", f.job.Prompt)
	assert.Equal(t, "Morning check-in", res.Title, "the result shows the whole job as it now stands")
	assert.Equal(t, "every day at 8am", res.Schedule)
	assert.Empty(t, f.parsed, "the schedule was not re-read")
}

func TestUpdateAgentJobReschedulesInTheJobsTimezone(t *testing.T) {
	f := newJobUpdateFixture(t)

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"schedule_input":"every day at 9am"}`, f.job.ID))

	require.True(t, res.Success, res.Error)
	assert.Equal(t, []string{"America/New_York"}, f.tzs, "no client timezone: the job keeps its own")
	assert.Equal(t, "every day at 9am", f.job.ScheduleInput)
	assert.Equal(t, "Every day at 9:00 AM", res.HumanSummary)
	assert.NotNil(t, res.NextRunAt)
}

func TestUpdateAgentJobMovesTheJobToAnotherThread(t *testing.T) {
	f := newJobUpdateFixture(t)
	other := &models.Chat{ID: uuid.New(), UserID: f.chat.UserID}
	f.store.chats[other.ID] = other

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"thread_id":%q}`, f.job.ID, other.ID))
	require.True(t, res.Success, res.Error)
	require.NotNil(t, f.job.ChatID)
	assert.Equal(t, other.ID, *f.job.ChatID)
	assert.Equal(t, other.ID.String(), res.ThreadID)

	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"thread_id":"current"}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Equal(t, f.chat.ID, *f.job.ChatID)

	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"thread_id":"new"}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Nil(t, f.job.ChatID, "a dedicated thread is made on the next run")
	assert.Contains(t, res.Note, "thread of its own")
}

func TestUpdateAgentJobRefusesThreadsItMayNotUse(t *testing.T) {
	f := newJobUpdateFixture(t)
	sandboxed := &models.Chat{ID: uuid.New(), UserID: f.chat.UserID, ContextScope: models.ContextScopeSandbox}
	f.store.chats[sandboxed.ID] = sandboxed
	someoneElses := uuid.New() // not in the store for this user

	for name, thread := range map[string]string{
		"not an id":      "the morning thread",
		"unknown":        uuid.NewString(),
		"someone else's": someoneElses.String(),
		"sandboxed":      sandboxed.ID.String(),
	} {
		t.Run(name, func(t *testing.T) {
			res := f.run(t, fmt.Sprintf(`{"job_id":%q,"thread_id":%q,"prompt":"changed"}`, f.job.ID, thread))
			assert.False(t, res.Success)
			assert.NotEmpty(t, res.Error)
			assert.Empty(t, f.store.writes, "a bad thread changes nothing, not even the other fields")
		})
	}
}

func TestUpdateAgentJobPausesAndResumes(t *testing.T) {
	f := newJobUpdateFixture(t)

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"status":"Paused"}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Equal(t, models.AgentJobStatusPaused, f.job.Status)

	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"status":"active"}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Equal(t, models.AgentJobStatusActive, f.job.Status)
}

func TestUpdateAgentJobPausingWithANewScheduleLeavesItPaused(t *testing.T) {
	f := newJobUpdateFixture(t)
	f.job.Status = models.AgentJobStatusComplete

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"schedule_input":"every day at 9am","status":"paused"}`, f.job.ID))

	require.True(t, res.Success, res.Error)
	assert.Equal(t, models.AgentJobStatusPaused, f.job.Status)
	assert.Equal(t, []string{"schedule", "status"}, f.store.writes)
}

func TestUpdateAgentJobReplacesSkillsAndModel(t *testing.T) {
	f := newJobUpdateFixture(t)
	keep, drop, add := uuid.New(), uuid.New(), uuid.New()
	f.job.Rituals = []*models.Ritual{{ID: keep}, {ID: drop}}

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"skill_ids":[%q,%q,%q],"model_override":"claude-opus-5-5"}`, f.job.ID, keep, add, add))

	require.True(t, res.Success, res.Error)
	assert.ElementsMatch(t, []string{keep.String(), add.String()}, res.SkillIDs)
	assert.Equal(t, f.models["claude-opus-5-5"].String(), res.ModelID)

	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"skill_ids":[],"model_override":""}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Empty(t, res.SkillIDs, "[] removes every skill")
	assert.Empty(t, res.ModelID, "\"\" goes back to the default model")
}

func TestUpdateAgentJobRefusesBadArgumentsWithoutWriting(t *testing.T) {
	f := newJobUpdateFixture(t)
	id := f.job.ID
	for name, args := range map[string]string{
		"not json":       `nope`,
		"no job id":      `{"prompt":"x"}`,
		"bad job id":     `{"job_id":"morning","prompt":"x"}`,
		"unknown job":    fmt.Sprintf(`{"job_id":%q,"prompt":"x"}`, uuid.New()),
		"nothing to do":  fmt.Sprintf(`{"job_id":%q}`, id),
		"empty prompt":   fmt.Sprintf(`{"job_id":%q,"prompt":"  "}`, id),
		"empty schedule": fmt.Sprintf(`{"job_id":%q,"schedule_input":""}`, id),
		"bad schedule":   fmt.Sprintf(`{"job_id":%q,"schedule_input":"nonsense","prompt":"x"}`, id),
		"bad status":     fmt.Sprintf(`{"job_id":%q,"status":"deleted"}`, id),
		"unknown model":  fmt.Sprintf(`{"job_id":%q,"model_override":"gpt-0","prompt":"x"}`, id),
		"bad skill id":   fmt.Sprintf(`{"job_id":%q,"skill_ids":["journal"]}`, id),
	} {
		t.Run(name, func(t *testing.T) {
			res := f.run(t, args)
			assert.False(t, res.Success)
			assert.NotEmpty(t, res.Error)
			assert.Empty(t, f.store.writes)
		})
	}
}

func TestUpdateAgentJobSaysWhatChangedWhenALaterWriteFails(t *testing.T) {
	f := newJobUpdateFixture(t)
	f.store.failOn = "thread"

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"prompt":"new","thread_id":"current"}`, f.job.ID))

	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "updated prompt")
	assert.Contains(t, res.Error, "thread_id")
	assert.NotContains(t, res.Error, "10.0.0.5", "datastore errors are not shown to the model")
}

func TestUpdateAgentJobIsRefusedInASandbox(t *testing.T) {
	f := newJobUpdateFixture(t)
	f.chat.ContextScope = models.ContextScopeSandbox

	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"prompt":"x"}`, f.job.ID))

	assert.False(t, res.Success)
	assert.Equal(t, sandboxedAgentJobNote, res.Error)
	assert.Empty(t, f.store.writes)
}

func TestResolveJobThread(t *testing.T) {
	f := newJobUpdateFixture(t)
	ctx := context.Background()

	for _, raw := range []string{"", "  ", "new", "NEW"} {
		got, err := resolveJobThread(ctx, f.store, f.chat, raw)
		require.NoError(t, err, raw)
		assert.Nil(t, got, "%q is a dedicated thread", raw)
	}
	got, err := resolveJobThread(ctx, f.store, f.chat, "Current")
	require.NoError(t, err)
	assert.Equal(t, f.chat.ID, *got)

	got, err = resolveJobThread(ctx, f.store, f.chat, f.chat.ID.String())
	require.NoError(t, err)
	assert.Equal(t, f.chat.ID, *got)

	_, err = resolveJobThread(ctx, failingChats{}, f.chat, uuid.NewString())
	assert.ErrorIs(t, err, errJobThreadLookup, "a datastore failure is told apart from a bad id")
}

type failingChats struct{}

func (failingChats) GetChat(context.Context, uuid.UUID, uuid.UUID) (*models.Chat, error) {
	return nil, errors.New("pq: connection refused")
}

func TestUpdateAgentJobParameters(t *testing.T) {
	f := newJobUpdateFixture(t)

	// Define a parameter in the same call as the prompt that uses it: the job waits for a value.
	res := f.run(t, fmt.Sprintf(`{"job_id":%q,"prompt":"News on {{topics}} in {{region}}",
		"parameters":[{"name":"topics","type":"list"},{"name":"region","type":"single_select","options":["US","EU"]}],
		"parameter_values":[{"name":"region","value":"EU"}]}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Equal(t, []string{"prompt", "parameters", "parameter_values"}, res.Updated)
	assert.Equal(t, string(models.AgentJobStatusNeedsValues), res.Status)
	assert.Equal(t, []string{"topics"}, res.MissingParameters)
	assert.Contains(t, res.Note, "topics")

	// Resuming is refused while a value is missing.
	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"status":"active"}`, f.job.ID))
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "missing: topics")

	// Setting the last value merges with the others and makes the job active again.
	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"parameter_values":[{"name":"topics","items":["AI","climate"]}]}`, f.job.ID))
	require.True(t, res.Success, res.Error)
	assert.Equal(t, string(models.AgentJobStatusActive), res.Status)
	assert.Empty(t, res.MissingParameters)
	assert.Equal(t, "EU", res.ParameterValues["region"].Text)
	assert.Equal(t, []string{"AI", "climate"}, res.ParameterValues["topics"].Items)

	// An undefined placeholder is reported and changes nothing.
	res = f.run(t, fmt.Sprintf(`{"job_id":%q,"prompt":"Now {{other}}"}`, f.job.ID))
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "other")
	assert.Equal(t, "News on {{topics}} in {{region}}", f.job.Prompt)
}

func TestMergeJobParamValues(t *testing.T) {
	base := modeltypes.JobParameterValues{"a": modeltypes.TextValue("x"), "b": modeltypes.ListValue("y")}
	out, err := mergeJobParamValues(base, []jobParamValueArg{{Name: "a", Value: ptrTo("")}, {Name: "c", Items: &[]string{"z"}}})
	require.NoError(t, err)
	_, hasA := out["a"]
	assert.False(t, hasA, "an empty value clears the parameter")
	assert.Equal(t, []string{"y"}, out["b"].Items, "values not passed are kept")
	assert.Equal(t, []string{"z"}, out["c"].Items)
	assert.Equal(t, "x", base["a"].Text, "base is not modified")

	_, err = mergeJobParamValues(nil, []jobParamValueArg{{Name: "a", Value: ptrTo("x"), Items: &[]string{"y"}}})
	assert.Error(t, err)
}

func ptrTo[T any](v T) *T { return &v }
