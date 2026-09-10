package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// alterChatsTableForAgentJobTests adds columns that exist in the real ent chats schema
// (ent/migrate/schema.go's ChatsColumns) but not in createMemoryImportTestSchema's chats
// table fixture. agentjob.go's WithChat() eager-load selects every chat column, so a missing
// column ("source" and friends) surfaces as a sqlite "no such column" error at query time.
func alterChatsTableForAgentJobTests(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`ALTER TABLE chats ADD COLUMN source text`,
		`ALTER TABLE chats ADD COLUMN import_hash text`,
		`ALTER TABLE chats ADD COLUMN rehydration_state text`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newAgentJobTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, alterChatsTableForAgentJobTests)
}

func createAgentJobTestUser(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := ds.dbClient.User.Create().
		SetID(id).
		SetUsername("job-" + id.String()[:8]).
		SetEmail("job-" + id.String()[:8] + "@example.com").
		SetPasswordHash("hash").
		Save(context.Background())
	require.NoError(t, err)
	return id
}

func createAgentJobTestModel(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.Model.Create().
		SetName("model-" + uuid.NewString()[:8]).
		SetDisplayName("Test Model").
		SetDescription("test model").
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func createAgentJobTestPersonality(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	p, err := ds.dbClient.Personality.Create().
		SetName("Vix").
		SetSystemPrompt("system prompt").
		SetUserID(userID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return p.ID
}

func createAgentJobTestChat(t *testing.T, ds *Datastore, userID, modelID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	c, err := ds.dbClient.Chat.Create().
		SetName("Chat").
		SetOwnerID(userID).
		SetModelID(modelID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return c.ID
}

func createAgentJobTestRitual(t *testing.T, ds *Datastore, userID uuid.UUID) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	r, err := ds.dbClient.Ritual.Create().
		SetOwnerID(userID).
		SetName("Morning").
		SetDescription("desc").
		SetContent("content").
		SetHotkeys("").
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	require.NoError(t, err)
	return r.ID
}

func baseAgentJobModel() models.AgentJob {
	return models.AgentJob{
		Prompt:        "do the thing",
		ScheduleInput: "daily",
		ScheduleType:  models.AgentJobScheduleTypeCron,
		Timezone:      "UTC",
		Status:        models.AgentJobStatusActive,
	}
}

func TestNormalizeAgentJobDatastoreUnexpectedError(t *testing.T) {
	require.NoError(t, normalizeAgentJobDatastoreUnexpectedError(nil))

	err := normalizeAgentJobDatastoreUnexpectedError(errors.New("boom"))
	require.ErrorIs(t, err, ErrInternalDatastore)
}

func TestToAgentJobModel_Nil(t *testing.T) {
	require.Nil(t, toAgentJobModel(nil))
}

// shouldReactivateAgentJobAfterScheduleSave already has direct unit coverage in
// agentjob_schedule_reactivate_test.go; the full UpdateAgentJobSchedule tests below
// exercise its effect end-to-end (status flips back to active).

func TestCreateAgentJob_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	personalityID := createAgentJobTestPersonality(t, ds, userID)
	chatID := createAgentJobTestChat(t, ds, userID, modelID)

	title := "Weekly digest"
	jobModel := baseAgentJobModel()
	jobModel.Title = &title
	jobModel.ChatID = &chatID
	jobModel.PersonalityID = &personalityID
	jobModel.ModelID = &modelID

	got, err := ds.CreateAgentJob(ctx, userID, jobModel)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, userID, got.UserID)
	require.NotNil(t, got.ChatID)
	require.Equal(t, chatID, *got.ChatID)
	require.NotNil(t, got.PersonalityID)
	require.Equal(t, personalityID, *got.PersonalityID)
	require.NotNil(t, got.ModelID)
	require.Equal(t, modelID, *got.ModelID)
	require.Equal(t, title, *got.Title)
	require.Equal(t, "do the thing", got.Prompt)
	require.Equal(t, models.AgentJobStatusActive, got.Status)
}

func TestCreateAgentJob_UserNotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	_, err := ds.CreateAgentJob(context.Background(), uuid.New(), baseAgentJobModel())
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestCreateAgentJob_ChatNotOwnedByUser(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	chatID := createAgentJobTestChat(t, ds, otherUserID, modelID)

	jobModel := baseAgentJobModel()
	jobModel.ChatID = &chatID

	_, err := ds.CreateAgentJob(context.Background(), userID, jobModel)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestCreateAgentJob_ChatDoesNotExist(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	missingChatID := uuid.New()

	jobModel := baseAgentJobModel()
	jobModel.ChatID = &missingChatID

	_, err := ds.CreateAgentJob(context.Background(), userID, jobModel)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestCreateAgentJob_InvalidPersonalityOverride(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	missingPersonalityID := uuid.New()

	jobModel := baseAgentJobModel()
	jobModel.PersonalityID = &missingPersonalityID

	_, err := ds.CreateAgentJob(context.Background(), userID, jobModel)
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestCreateAgentJob_PersonalityOwnedByAnotherUser(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	personalityID := createAgentJobTestPersonality(t, ds, otherUserID)

	jobModel := baseAgentJobModel()
	jobModel.PersonalityID = &personalityID

	_, err := ds.CreateAgentJob(context.Background(), userID, jobModel)
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestCreateAgentJob_InvalidModelOverride(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	missingModelID := uuid.New()

	jobModel := baseAgentJobModel()
	jobModel.ModelID = &missingModelID

	_, err := ds.CreateAgentJob(context.Background(), userID, jobModel)
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestGetAgentJob_Found(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	got, err := ds.GetAgentJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "do the thing", got.Prompt)
}

func TestGetAgentJob_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.GetAgentJob(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestGetAgentJob_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.GetAgentJob(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestListAgentJobs_UserScoping(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)

	_, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)
	_, err = ds.CreateAgentJob(ctx, otherUserID, baseAgentJobModel())
	require.NoError(t, err)

	resp, err := ds.ListAgentJobs(ctx, userID, 1, 10, models.AgentJobFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Len(t, resp.Results, 1)
}

func TestListAgentJobs_FiltersAndPagination(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)

	active := baseAgentJobModel()
	active.Title = strPtr("Morning report")
	_, err := ds.CreateAgentJob(ctx, userID, active)
	require.NoError(t, err)

	paused := baseAgentJobModel()
	paused.Status = models.AgentJobStatusPaused
	paused.ScheduleType = models.AgentJobScheduleTypeAt
	paused.Title = strPtr("Evening cleanup")
	_, err = ds.CreateAgentJob(ctx, userID, paused)
	require.NoError(t, err)

	tests := []struct {
		name    string
		filters models.AgentJobFilters
		want    int
	}{
		{
			name:    "no filters returns all",
			filters: models.AgentJobFilters{},
			want:    2,
		},
		{
			name:    "status filter",
			filters: models.AgentJobFilters{Status: statusPtr(models.AgentJobStatusPaused)},
			want:    1,
		},
		{
			name:    "schedule type filter",
			filters: models.AgentJobFilters{ScheduleType: scheduleTypePtr(models.AgentJobScheduleTypeAt)},
			want:    1,
		},
		{
			name:    "query filter matches title case-insensitively",
			filters: models.AgentJobFilters{Query: strPtr("morning")},
			want:    1,
		},
		{
			name:    "query filter with no match",
			filters: models.AgentJobFilters{Query: strPtr("nonexistent")},
			want:    0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := ds.ListAgentJobs(ctx, userID, 1, 10, tc.filters)
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.TotalCount)
			require.Len(t, resp.Results, tc.want)
		})
	}

	// Pagination: page size 1 should split the 2 unfiltered results across 2 pages.
	page1, err := ds.ListAgentJobs(ctx, userID, 1, 1, models.AgentJobFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, page1.TotalCount)
	require.Len(t, page1.Results, 1)
	require.Equal(t, 1, page1.Page)

	page2, err := ds.ListAgentJobs(ctx, userID, 2, 1, models.AgentJobFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, page2.TotalCount)
	require.Len(t, page2.Results, 1)
	require.Equal(t, 2, page2.Page)

	// Invalid pageNum/pageSize fall back to defaults (page 1, size 10).
	fallback, err := ds.ListAgentJobs(ctx, userID, 0, 0, models.AgentJobFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, fallback.Page)
	require.Len(t, fallback.Results, 2)
}

func strPtr(s string) *string { return &s }

func statusPtr(s models.AgentJobStatus) *models.AgentJobStatus { return &s }

func scheduleTypePtr(s models.AgentJobScheduleType) *models.AgentJobScheduleType { return &s }

func TestUpdateAgentJobSchedule_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	schedule := "0 9 * * *"
	runAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	nextRunAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	got, err := ds.UpdateAgentJobSchedule(ctx, userID, created.ID, "daily at 9am", models.AgentJobScheduleTypeCron, &schedule, &runAt, "America/New_York", &nextRunAt)
	require.NoError(t, err)
	require.Equal(t, "daily at 9am", got.ScheduleInput)
	require.Equal(t, schedule, *got.Schedule)
	require.WithinDuration(t, runAt, *got.RunAt, time.Second)
	require.WithinDuration(t, nextRunAt, *got.NextRunAt, time.Second)
	require.Equal(t, "America/New_York", got.Timezone)
	require.Equal(t, models.AgentJobStatusActive, got.Status)
}

func TestUpdateAgentJobSchedule_ClearsScheduleRunAtNextRunAt(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	schedule := "0 9 * * *"
	runAt := time.Now().Add(time.Hour)
	nextRunAt := time.Now().Add(2 * time.Hour)
	jobModel := baseAgentJobModel()
	jobModel.Schedule = &schedule
	jobModel.RunAt = &runAt
	jobModel.NextRunAt = &nextRunAt
	created, err := ds.CreateAgentJob(ctx, userID, jobModel)
	require.NoError(t, err)

	got, err := ds.UpdateAgentJobSchedule(ctx, userID, created.ID, "once", models.AgentJobScheduleTypeAt, nil, nil, "UTC", nil)
	require.NoError(t, err)
	require.Nil(t, got.Schedule)
	require.Nil(t, got.RunAt)
	require.Nil(t, got.NextRunAt)
}

func TestUpdateAgentJobSchedule_ReactivatesTerminalJobWithNextRun(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	jobModel := baseAgentJobModel()
	jobModel.Status = models.AgentJobStatusFailed
	jobModel.LastError = "boom"
	created, err := ds.CreateAgentJob(ctx, userID, jobModel)
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusFailed, created.Status)

	nextRunAt := time.Now().Add(time.Hour)
	got, err := ds.UpdateAgentJobSchedule(ctx, userID, created.ID, "daily", models.AgentJobScheduleTypeCron, nil, nil, "UTC", &nextRunAt)
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusActive, got.Status)
	require.Empty(t, got.LastError)
}

func TestUpdateAgentJobSchedule_TerminalJobWithoutNextRunStaysTerminal(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	jobModel := baseAgentJobModel()
	jobModel.Status = models.AgentJobStatusComplete
	created, err := ds.CreateAgentJob(ctx, userID, jobModel)
	require.NoError(t, err)

	got, err := ds.UpdateAgentJobSchedule(ctx, userID, created.ID, "once", models.AgentJobScheduleTypeAt, nil, nil, "UTC", nil)
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusComplete, got.Status)
}

func TestUpdateAgentJobSchedule_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.UpdateAgentJobSchedule(context.Background(), userID, uuid.New(), "daily", models.AgentJobScheduleTypeCron, nil, nil, "UTC", nil)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobSchedule_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateAgentJobSchedule(ctx, otherUserID, created.ID, "daily", models.AgentJobScheduleTypeCron, nil, nil, "UTC", nil)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobStatus_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	got, err := ds.UpdateAgentJobStatus(ctx, userID, created.ID, models.AgentJobStatusFailed, "it broke")
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusFailed, got.Status)
	require.Equal(t, "it broke", got.LastError)

	// Clearing lastError with an empty string.
	got, err = ds.UpdateAgentJobStatus(ctx, userID, created.ID, models.AgentJobStatusActive, "")
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusActive, got.Status)
	require.Empty(t, got.LastError)
}

func TestUpdateAgentJobStatus_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.UpdateAgentJobStatus(context.Background(), userID, uuid.New(), models.AgentJobStatusPaused, "")
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobStatus_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateAgentJobStatus(ctx, otherUserID, created.ID, models.AgentJobStatusPaused, "")
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobTitle_SetAndClear(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	title := "  My Job  "
	got, err := ds.UpdateAgentJobTitle(ctx, userID, created.ID, &title)
	require.NoError(t, err)
	require.NotNil(t, got.Title)
	require.Equal(t, "My Job", *got.Title)

	got, err = ds.UpdateAgentJobTitle(ctx, userID, created.ID, nil)
	require.NoError(t, err)
	require.Nil(t, got.Title)

	// Whitespace-only title also clears.
	blank := "   "
	got, err = ds.UpdateAgentJobTitle(ctx, userID, created.ID, &blank)
	require.NoError(t, err)
	require.Nil(t, got.Title)
}

func TestUpdateAgentJobTitle_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.UpdateAgentJobTitle(context.Background(), userID, uuid.New(), strPtr("x"))
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobTitle_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateAgentJobTitle(ctx, otherUserID, created.ID, strPtr("x"))
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobPrompt_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	got, err := ds.UpdateAgentJobPrompt(ctx, userID, created.ID, "new prompt")
	require.NoError(t, err)
	require.Equal(t, "new prompt", got.Prompt)
}

func TestUpdateAgentJobPrompt_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.UpdateAgentJobPrompt(context.Background(), userID, uuid.New(), "p")
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestUpdateAgentJobPrompt_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateAgentJobPrompt(ctx, otherUserID, created.ID, "p")
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestSetAgentJobChat_SetAndClear(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	chatID := createAgentJobTestChat(t, ds, userID, modelID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	got, err := ds.SetAgentJobChat(ctx, userID, created.ID, &chatID)
	require.NoError(t, err)
	require.NotNil(t, got.ChatID)
	require.Equal(t, chatID, *got.ChatID)

	got, err = ds.SetAgentJobChat(ctx, userID, created.ID, nil)
	require.NoError(t, err)
	require.Nil(t, got.ChatID)
}

func TestSetAgentJobChat_ChatNotOwned(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	chatID := createAgentJobTestChat(t, ds, otherUserID, modelID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.SetAgentJobChat(ctx, userID, created.ID, &chatID)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestSetAgentJobChat_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.SetAgentJobChat(context.Background(), userID, uuid.New(), nil)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestSetAgentJobChat_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.SetAgentJobChat(ctx, otherUserID, created.ID, nil)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestValidateAgentJobOverrideIDs(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	personalityID := createAgentJobTestPersonality(t, ds, userID)
	otherPersonalityID := createAgentJobTestPersonality(t, ds, otherUserID)
	modelID := createAgentJobTestModel(t, ds)
	missingID := uuid.New()

	tests := []struct {
		name    string
		patch   models.SetAgentJobOverridesPatch
		wantErr error
	}{
		{
			name:    "no-op patch is valid",
			patch:   models.SetAgentJobOverridesPatch{},
			wantErr: nil,
		},
		{
			name:    "valid personality owned by user",
			patch:   models.SetAgentJobOverridesPatch{UpdatePersonality: true, PersonalityID: &personalityID},
			wantErr: nil,
		},
		{
			name:    "clearing personality (nil id) is valid",
			patch:   models.SetAgentJobOverridesPatch{UpdatePersonality: true, PersonalityID: nil},
			wantErr: nil,
		},
		{
			name:    "personality not owned by user is invalid",
			patch:   models.SetAgentJobOverridesPatch{UpdatePersonality: true, PersonalityID: &otherPersonalityID},
			wantErr: ErrInvalidRequestBody,
		},
		{
			name:    "personality does not exist is invalid",
			patch:   models.SetAgentJobOverridesPatch{UpdatePersonality: true, PersonalityID: &missingID},
			wantErr: ErrInvalidRequestBody,
		},
		{
			name:    "valid model",
			patch:   models.SetAgentJobOverridesPatch{UpdateModel: true, ModelID: &modelID},
			wantErr: nil,
		},
		{
			name:    "clearing model (nil id) is valid",
			patch:   models.SetAgentJobOverridesPatch{UpdateModel: true, ModelID: nil},
			wantErr: nil,
		},
		{
			name:    "model does not exist is invalid",
			patch:   models.SetAgentJobOverridesPatch{UpdateModel: true, ModelID: &missingID},
			wantErr: ErrInvalidRequestBody,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := ds.dbClient.Tx(ctx)
			require.NoError(t, err)
			defer tx.Rollback()

			err = validateAgentJobOverrideIDs(ctx, tx, userID, tc.patch)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestSetAgentJobOverrides_NoFieldsRequested(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.SetAgentJobOverrides(context.Background(), userID, uuid.New(), models.SetAgentJobOverridesPatch{})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestSetAgentJobOverrides_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	personalityID := createAgentJobTestPersonality(t, ds, userID)
	modelID := createAgentJobTestModel(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	got, err := ds.SetAgentJobOverrides(ctx, userID, created.ID, models.SetAgentJobOverridesPatch{
		UpdatePersonality: true,
		PersonalityID:     &personalityID,
		UpdateModel:       true,
		ModelID:           &modelID,
	})
	require.NoError(t, err)
	require.NotNil(t, got.PersonalityID)
	require.Equal(t, personalityID, *got.PersonalityID)
	require.NotNil(t, got.ModelID)
	require.Equal(t, modelID, *got.ModelID)

	// Per the SetAgentJobOverridesPatch doc comment, the Update flag set with a nil ID clears
	// the override rather than leaving the existing value untouched.
	got, err = ds.SetAgentJobOverrides(ctx, userID, created.ID, models.SetAgentJobOverridesPatch{
		UpdatePersonality: true,
		UpdateModel:       true,
	})
	require.NoError(t, err)
	require.Nil(t, got.PersonalityID)
	require.Nil(t, got.ModelID)
}

func TestSetAgentJobOverrides_InvalidPersonality(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	otherPersonalityID := createAgentJobTestPersonality(t, ds, otherUserID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.SetAgentJobOverrides(ctx, userID, created.ID, models.SetAgentJobOverridesPatch{
		UpdatePersonality: true,
		PersonalityID:     &otherPersonalityID,
	})
	require.ErrorIs(t, err, ErrInvalidRequestBody)
}

func TestSetAgentJobOverrides_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	_, err := ds.SetAgentJobOverrides(context.Background(), userID, uuid.New(), models.SetAgentJobOverridesPatch{
		UpdateModel: true,
		ModelID:     &modelID,
	})
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestSetAgentJobOverrides_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	modelID := createAgentJobTestModel(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.SetAgentJobOverrides(ctx, otherUserID, created.ID, models.SetAgentJobOverridesPatch{
		UpdateModel: true,
		ModelID:     &modelID,
	})
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestDeleteAgentJob_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.DeleteAgentJob(ctx, userID, created.ID))

	_, err = ds.GetAgentJob(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestDeleteAgentJob_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	err := ds.DeleteAgentJob(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestDeleteAgentJob_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.DeleteAgentJob(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)

	// The job should still be there for its actual owner.
	_, err = ds.GetAgentJob(ctx, userID, created.ID)
	require.NoError(t, err)
}

func TestListAgentJobsForScheduler_ReturnsAllUsersJobs(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)

	created1, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)
	created2, err := ds.CreateAgentJob(ctx, otherUserID, baseAgentJobModel())
	require.NoError(t, err)

	jobs, err := ds.ListAgentJobsForScheduler(ctx)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	ids := []uuid.UUID{jobs[0].ID, jobs[1].ID}
	require.Contains(t, ids, created1.ID)
	require.Contains(t, ids, created2.ID)
}

func TestListAgentJobsForScheduler_Empty(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	jobs, err := ds.ListAgentJobsForScheduler(context.Background())
	require.NoError(t, err)
	require.Empty(t, jobs)
}

func TestRecordAgentJobRun_SuccessUpdatesRunMetadata(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)
	require.Equal(t, 0, created.RunCount)

	lastRunAt := time.Now().UTC().Truncate(time.Second)
	nextRunAt := lastRunAt.Add(24 * time.Hour)

	got, err := ds.RecordAgentJobRun(ctx, userID, created.ID, lastRunAt, &nextRunAt, "", nil)
	require.NoError(t, err)
	require.Equal(t, 1, got.RunCount)
	require.NotNil(t, got.LastRunAt)
	require.WithinDuration(t, lastRunAt, *got.LastRunAt, time.Second)
	require.NotNil(t, got.NextRunAt)
	require.WithinDuration(t, nextRunAt, *got.NextRunAt, time.Second)
	require.Empty(t, got.LastError)
	require.Equal(t, models.AgentJobStatusActive, got.Status)

	// A second run increments the count again and can clear next_run_at.
	got, err = ds.RecordAgentJobRun(ctx, userID, created.ID, lastRunAt, nil, "", nil)
	require.NoError(t, err)
	require.Equal(t, 2, got.RunCount)
	require.Nil(t, got.NextRunAt)
}

func TestRecordAgentJobRun_ErrorAndStatusChange(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	newStatus := models.AgentJobStatusFailed
	got, err := ds.RecordAgentJobRun(ctx, userID, created.ID, time.Now(), nil, "provider timeout", &newStatus)
	require.NoError(t, err)
	require.Equal(t, models.AgentJobStatusFailed, got.Status)
	require.Equal(t, "provider timeout", got.LastError)
}

func TestRecordAgentJobRun_NotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	_, err := ds.RecordAgentJobRun(context.Background(), userID, uuid.New(), time.Now(), nil, "", nil)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestRecordAgentJobRun_WrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	_, err = ds.RecordAgentJobRun(ctx, otherUserID, created.ID, time.Now(), nil, "", nil)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestAddAgentJobRitual_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, userID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.AddAgentJobRitual(ctx, userID, created.ID, ritualID))

	got, err := ds.GetAgentJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Len(t, got.Rituals, 1)
	require.Equal(t, ritualID, got.Rituals[0].ID)
}

func TestAddAgentJobRitual_JobNotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, userID)

	err := ds.AddAgentJobRitual(context.Background(), userID, uuid.New(), ritualID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestAddAgentJobRitual_JobWrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, otherUserID)
	created, err := ds.CreateAgentJob(ctx, otherUserID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.AddAgentJobRitual(ctx, userID, created.ID, ritualID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestAddAgentJobRitual_RitualNotOwned(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, otherUserID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.AddAgentJobRitual(ctx, userID, created.ID, ritualID)
	require.ErrorIs(t, err, ErrRitualNotFound)
}

func TestAddAgentJobRitual_RitualDoesNotExist(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.AddAgentJobRitual(ctx, userID, created.ID, uuid.New())
	require.ErrorIs(t, err, ErrRitualNotFound)
}

func TestRemoveAgentJobRitual_HappyPath(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, userID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)
	require.NoError(t, ds.AddAgentJobRitual(ctx, userID, created.ID, ritualID))

	require.NoError(t, ds.RemoveAgentJobRitual(ctx, userID, created.ID, ritualID))

	got, err := ds.GetAgentJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.Rituals)
}

func TestRemoveAgentJobRitual_JobNotFound(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()

	userID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, userID)

	err := ds.RemoveAgentJobRitual(context.Background(), userID, uuid.New(), ritualID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestRemoveAgentJobRitual_JobWrongOwner(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, otherUserID)
	created, err := ds.CreateAgentJob(ctx, otherUserID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.RemoveAgentJobRitual(ctx, userID, created.ID, ritualID)
	require.ErrorIs(t, err, ErrAgentJobNotFound)
}

func TestRemoveAgentJobRitual_RitualNotOwned(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	otherUserID := createAgentJobTestUser(t, ds)
	ritualID := createAgentJobTestRitual(t, ds, otherUserID)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.RemoveAgentJobRitual(ctx, userID, created.ID, ritualID)
	require.ErrorIs(t, err, ErrRitualNotFound)
}

func TestRemoveAgentJobRitual_RitualDoesNotExist(t *testing.T) {
	ds, cleanup := newAgentJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createAgentJobTestUser(t, ds)
	created, err := ds.CreateAgentJob(ctx, userID, baseAgentJobModel())
	require.NoError(t, err)

	err = ds.RemoveAgentJobRitual(ctx, userID, created.ID, uuid.New())
	require.ErrorIs(t, err, ErrRitualNotFound)
}
