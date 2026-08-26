package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createJobTestSchema creates the "jobs" table per ent/migrate/schema.go's JobsColumns. None of
// the other shared schema fragments define it (it is distinct from agent_jobs, which backs the
// unrelated AgentJob entity and is already covered by createAccountBackupTestSchema).
//
// Must be composed after createMemoryImportTestSchema (users is the FK parent here) via
// newTestDatastore(t, createMemoryImportTestSchema, createJobTestSchema).
func createJobTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE jobs (
		id uuid PRIMARY KEY,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		job_type text NOT NULL,
		reference text NOT NULL,
		status text NOT NULL DEFAULT 'pending',
		error text,
		result_id uuid,
		draft_deltas json,
		progress text,
		user_jobs uuid NOT NULL,
		FOREIGN KEY (user_jobs) REFERENCES users(id)
	)`)
	require.NoError(t, err)
}

// newJobTestDatastore composes the schema fragments job.go's plain CRUD queries need:
// createMemoryImportTestSchema (users) and createJobTestSchema (jobs).
func newJobTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createJobTestSchema)
}

// newFinalizeChatJobTestDatastore additionally provides chats (with the full real column set,
// via alterChatsTableForAgentJobTests) and chat_messages, which finalizeChatJobWithPartial's
// helpers touch.
func newFinalizeChatJobTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createAccountBackupTestSchema, alterChatsTableForAgentJobTests, createJobTestSchema)
}

func createJobTestUser(t *testing.T, ds *Datastore) uuid.UUID {
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

func createJobTestModel(t *testing.T, ds *Datastore) uuid.UUID {
	t.Helper()
	m, err := ds.dbClient.Model.Create().
		SetName("model-" + uuid.NewString()[:8]).
		SetDisplayName("Test Model").
		SetDescription("test model").
		Save(context.Background())
	require.NoError(t, err)
	return m.ID
}

func createJobTestChat(t *testing.T, ds *Datastore, userID, modelID uuid.UUID) uuid.UUID {
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

func baseJobModel() models.Job {
	return models.Job{
		JobType:   "chat_message",
		Reference: "ref-1",
		Status:    models.JobStatusPending,
	}
}

func TestCreateJob_HappyPathMinimal(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	got, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, userID, got.UserID)
	require.Equal(t, "chat_message", got.JobType)
	require.Equal(t, "ref-1", got.Reference)
	require.Equal(t, models.JobStatusPending, got.Status)
	require.Empty(t, got.Error)
	require.Nil(t, got.ResultID)
}

func TestCreateJob_WithOptionalFields(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	resultID := uuid.New()
	jobModel := baseJobModel()
	jobModel.Error = "warm-up failure"
	jobModel.ResultID = &resultID
	jobModel.DraftDeltas = []string{"chunk1", "chunk2"}
	jobModel.Progress = `{"done":1}`

	got, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)
	require.Equal(t, "warm-up failure", got.Error)
	require.NotNil(t, got.ResultID)
	require.Equal(t, resultID, *got.ResultID)
	require.Equal(t, []string{"chunk1", "chunk2"}, got.DraftDeltas)
	require.Equal(t, `{"done":1}`, got.Progress)
}

func TestCreateJob_UserNotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	_, err := ds.CreateJob(context.Background(), uuid.New(), baseJobModel())
	require.Error(t, err)
}

func TestGetJob_Found(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "chat_message", got.JobType)
}

func TestGetJob_NotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	_, err := ds.GetJob(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestGetJob_WrongOwner(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	_, err = ds.GetJob(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestListJobs_UserScoping(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)

	_, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)
	_, err = ds.CreateJob(ctx, otherUserID, baseJobModel())
	require.NoError(t, err)

	resp, err := ds.ListJobs(ctx, userID, 1, 10, models.JobFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.TotalCount)
	require.Len(t, resp.Results, 1)
}

func TestListJobs_FiltersAndPagination(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)

	first := baseJobModel()
	first.JobType = "chat_message"
	first.Reference = "ref-a"
	_, err := ds.CreateJob(ctx, userID, first)
	require.NoError(t, err)

	second := baseJobModel()
	second.JobType = "personality_generation"
	second.Reference = "ref-b"
	second.Status = models.JobStatusComplete
	_, err = ds.CreateJob(ctx, userID, second)
	require.NoError(t, err)

	tests := []struct {
		name    string
		filters models.JobFilters
		want    int
	}{
		{name: "no filters returns all", filters: models.JobFilters{}, want: 2},
		{name: "job type filter", filters: models.JobFilters{JobType: jobStrPtr("personality_generation")}, want: 1},
		{name: "reference filter", filters: models.JobFilters{Reference: jobStrPtr("ref-a")}, want: 1},
		{name: "status filter", filters: models.JobFilters{Status: jobStatusPtr(models.JobStatusComplete)}, want: 1},
		{name: "empty job type filter is ignored", filters: models.JobFilters{JobType: jobStrPtr("")}, want: 2},
		{name: "empty reference filter is ignored", filters: models.JobFilters{Reference: jobStrPtr("")}, want: 2},
		{name: "no match", filters: models.JobFilters{Reference: jobStrPtr("nonexistent")}, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := ds.ListJobs(ctx, userID, 1, 10, tc.filters)
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.TotalCount)
			require.Len(t, resp.Results, tc.want)
		})
	}

	// Pagination: page size 1 should split the 2 unfiltered results across 2 pages.
	page1, err := ds.ListJobs(ctx, userID, 1, 1, models.JobFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, page1.TotalCount)
	require.Len(t, page1.Results, 1)
	require.Equal(t, 1, page1.Page)

	page2, err := ds.ListJobs(ctx, userID, 2, 1, models.JobFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, page2.TotalCount)
	require.Len(t, page2.Results, 1)
	require.Equal(t, 2, page2.Page)

	// Invalid pageNum/pageSize fall back to defaults (page 1, size 10).
	fallback, err := ds.ListJobs(ctx, userID, 0, 0, models.JobFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, fallback.Page)
	require.Len(t, fallback.Results, 2)
}

func jobStrPtr(s string) *string { return &s }

func jobStatusPtr(s models.JobStatus) *models.JobStatus { return &s }

func TestUpdateJob_HappyPathAndFieldClearing(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	resultID := uuid.New()
	jobModel := baseJobModel()
	jobModel.Error = "boom"
	jobModel.ResultID = &resultID
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	// Update with a new type/reference/status and no error/resultID: both get cleared.
	update := created
	update.JobType = "expression_grid"
	update.Reference = "ref-2"
	update.Status = models.JobStatusProcessing
	update.Error = ""
	update.ResultID = nil

	got, err := ds.UpdateJob(ctx, userID, *update)
	require.NoError(t, err)
	require.Equal(t, "expression_grid", got.JobType)
	require.Equal(t, "ref-2", got.Reference)
	require.Equal(t, models.JobStatusProcessing, got.Status)
	require.Empty(t, got.Error)
	require.Nil(t, got.ResultID)
}

func TestUpdateJob_PreservesDraftDeltasWhenNotSet(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	jobModel := baseJobModel()
	jobModel.DraftDeltas = []string{"a", "b"}
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, created.DraftDeltas)

	update := *created
	update.DraftDeltas = nil
	got, err := ds.UpdateJob(ctx, userID, update)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, got.DraftDeltas)

	update2 := *created
	update2.DraftDeltas = []string{"c"}
	got2, err := ds.UpdateJob(ctx, userID, update2)
	require.NoError(t, err)
	require.Equal(t, []string{"c"}, got2.DraftDeltas)
}

func TestUpdateJob_NotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	jobModel := baseJobModel()
	jobModel.ID = uuid.New()
	_, err := ds.UpdateJob(context.Background(), userID, jobModel)
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestUpdateJob_WrongOwner(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateJob(ctx, otherUserID, *created)
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestUpdateJobStatus_SetAndClearError(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	got, err := ds.UpdateJobStatus(ctx, userID, created.ID, models.JobStatusFailed, "it broke")
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, got.Status)
	require.Equal(t, "it broke", got.Error)

	got, err = ds.UpdateJobStatus(ctx, userID, created.ID, models.JobStatusComplete, "")
	require.NoError(t, err)
	require.Equal(t, models.JobStatusComplete, got.Status)
	require.Empty(t, got.Error)
}

func TestUpdateJobStatus_NotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	_, err := ds.UpdateJobStatus(context.Background(), userID, uuid.New(), models.JobStatusComplete, "")
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestUpdateJobStatus_WrongOwner(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	_, err = ds.UpdateJobStatus(ctx, otherUserID, created.ID, models.JobStatusComplete, "")
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestUpdateJobProgress_HappyPath(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.UpdateJobProgress(ctx, userID, created.ID, `{"count":3}`))

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, `{"count":3}`, got.Progress)
}

// TestUpdateJobProgress_WrongOwnerUpdatesZeroRows documents the doc-commented behavior:
// UpdateJobProgress is a single scoped UPDATE with no owner pre-check, so a mismatched user
// simply updates zero rows and returns nil rather than a not-found error.
func TestUpdateJobProgress_WrongOwnerUpdatesZeroRows(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.UpdateJobProgress(ctx, otherUserID, created.ID, `{"count":99}`))

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.Progress)
}

func TestClearJobDraftDeltas_HappyPath(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	jobModel := baseJobModel()
	jobModel.DraftDeltas = []string{"a", "b"}
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	require.NoError(t, ds.ClearJobDraftDeltas(ctx, userID, created.ID))

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.DraftDeltas)
}

func TestSetJobResult_HappyPath(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	resultID := uuid.New()
	got, err := ds.SetJobResult(ctx, userID, created.ID, resultID)
	require.NoError(t, err)
	require.NotNil(t, got.ResultID)
	require.Equal(t, resultID, *got.ResultID)
	require.Equal(t, models.JobStatusComplete, got.Status)
}

func TestSetJobResult_NotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	_, err := ds.SetJobResult(context.Background(), userID, uuid.New(), uuid.New())
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestSetJobResult_WrongOwner(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	_, err = ds.SetJobResult(ctx, otherUserID, created.ID, uuid.New())
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestDeleteJob_HappyPath(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.DeleteJob(ctx, userID, created.ID))

	_, err = ds.GetJob(ctx, userID, created.ID)
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestDeleteJob_NotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	err := ds.DeleteJob(context.Background(), userID, uuid.New())
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestDeleteJob_WrongOwner(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	err = ds.DeleteJob(ctx, otherUserID, created.ID)
	require.ErrorIs(t, err, ErrJobNotFound)

	// Job should still exist for its actual owner.
	_, err = ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
}

func TestFindActivePersonalityMediaJob_ReturnsSoleActiveJob(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)

	newer := baseJobModel()
	newer.JobType = "personality_portrait"
	newer.Reference = "newer"
	newer.Status = models.JobStatusProcessing
	createdNewer, err := ds.CreateJob(ctx, userID, newer)
	require.NoError(t, err)

	got, err := ds.FindActivePersonalityMediaJob(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, createdNewer.ID, got.ID)
}

// TestFindActivePersonalityMediaJob_MultipleActiveJobsReturnsNewest exercises the case where a
// user has two concurrently-active jobs of different types under FindActivePersonalityMediaJob's
// job_type IN-list (expression_grid, personality_portrait, personality_generation) — e.g. an
// expression_grid job and a personality_portrait job both mid-flight. The doc comment promises
// "the newest non-terminal personality background job," which requires falling back to the first
// row after ordering rather than erroring when more than one row matches.
func TestFindActivePersonalityMediaJob_MultipleActiveJobsReturnsNewest(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)

	older := baseJobModel()
	older.JobType = "expression_grid"
	older.Reference = "older"
	_, err := ds.CreateJob(ctx, userID, older)
	require.NoError(t, err)

	newer := baseJobModel()
	newer.JobType = "personality_portrait"
	newer.Reference = "newer"
	newer.Status = models.JobStatusProcessing
	createdNewer, err := ds.CreateJob(ctx, userID, newer)
	require.NoError(t, err)

	got, err := ds.FindActivePersonalityMediaJob(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, createdNewer.ID, got.ID)
}

func TestFindActivePersonalityMediaJob_IgnoresTerminalAndOtherTypes(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)

	terminal := baseJobModel()
	terminal.JobType = "personality_generation"
	terminal.Status = models.JobStatusComplete
	_, err := ds.CreateJob(ctx, userID, terminal)
	require.NoError(t, err)

	other := baseJobModel()
	other.JobType = "chat_message"
	other.Status = models.JobStatusProcessing
	_, err = ds.CreateJob(ctx, userID, other)
	require.NoError(t, err)

	got, err := ds.FindActivePersonalityMediaJob(ctx, userID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestFindActivePersonalityMediaJob_NoJobs(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	got, err := ds.FindActivePersonalityMediaJob(context.Background(), userID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestFindLatestActiveChatMessageJob_Found(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	userMessageID := uuid.New()

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Reference = userMessageID.String()
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	got, err := ds.FindLatestActiveChatMessageJob(ctx, userID, userMessageID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, created.ID, got.ID)
}

func TestFindLatestActiveChatMessageJob_TerminalIsIgnored(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	userMessageID := uuid.New()

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Reference = userMessageID.String()
	jobModel.Status = models.JobStatusComplete
	_, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	got, err := ds.FindLatestActiveChatMessageJob(ctx, userID, userMessageID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestFindLatestActiveChatMessageJob_NotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()

	userID := createJobTestUser(t, ds)
	got, err := ds.FindLatestActiveChatMessageJob(context.Background(), userID, uuid.New())
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestFindActivePersonalityGenerationJob_Found(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	flowID := uuid.New()

	jobModel := baseJobModel()
	jobModel.JobType = "personality_generation"
	jobModel.Reference = flowID.String()
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	got, err := ds.FindActivePersonalityGenerationJob(ctx, userID, flowID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, created.ID, got.ID)
}

func TestFindActivePersonalityGenerationJob_WrongFlowNotFound(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	flowID := uuid.New()

	jobModel := baseJobModel()
	jobModel.JobType = "personality_generation"
	jobModel.Reference = flowID.String()
	jobModel.Status = models.JobStatusProcessing
	_, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	got, err := ds.FindActivePersonalityGenerationJob(ctx, userID, uuid.New())
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestFinalizeCancelledChatJobWithPartial_WithDraftDeltasCreatesMessage(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	jobModel.DraftDeltas = []string{"Hello ", "world"}
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	moodID := uuid.New()
	gotJob, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "gpt-test", "Vix", &moodID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusCancelled, gotJob.Status)
	require.Empty(t, gotJob.Error)
	require.Empty(t, gotJob.DraftDeltas)
	require.NotNil(t, resultID)
	require.NotNil(t, gotJob.ResultID)
	require.Equal(t, *resultID, *gotJob.ResultID)

	msg, err := ds.dbClient.ChatMessage.Get(ctx, *resultID)
	require.NoError(t, err)
	require.Equal(t, "Hello world", msg.Message)
	require.Equal(t, "gpt-test", msg.GenerationModel)
}

func TestFinalizeFailedChatJobWithPartial_NoDraftDeltasNoMessage(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	gotJob, resultID, err := ds.FinalizeFailedChatJobWithPartial(ctx, userID, created.ID, chatID, "gpt-test", "Vix", "provider timeout", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, gotJob.Status)
	require.Equal(t, "provider timeout", gotJob.Error)
	require.Nil(t, resultID)
	require.Nil(t, gotJob.ResultID)
}

// TestFinalizeFailedChatJobWithPartial_WhitespaceOnlyDraftTreatedAsEmpty documents that
// consumeDraftDeltasToMessageTx joins draft_deltas and then checks strings.TrimSpace against
// blank, so whitespace-only streamed content is treated the same as no content: no assistant
// message is persisted.
func TestFinalizeFailedChatJobWithPartial_WhitespaceOnlyDraftTreatedAsEmpty(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	jobModel.DraftDeltas = []string{"   ", "\t"}
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	gotJob, resultID, err := ds.FinalizeFailedChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", "boom", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, gotJob.Status)
	require.Nil(t, resultID)
}

func TestFinalizeChatJobWithPartial_IdempotentOnAlreadyTerminalJob(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	resultID := uuid.New()
	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusCancelled
	jobModel.ResultID = &resultID
	jobModel.DraftDeltas = []string{"leftover"}
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	gotJob, gotResultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusCancelled, gotJob.Status)
	require.Empty(t, gotJob.DraftDeltas)
	require.NotNil(t, gotResultID)
	require.Equal(t, resultID, *gotResultID)
}

// TestFinalizeChatJobWithPartial_IdempotentOnAlreadyTerminalJobWithoutResult documents the
// finalizeTerminalPartialIdempotentTx branch where the already-terminal job has no result_id:
// the idempotent path returns a nil resultID rather than an error.
func TestFinalizeChatJobWithPartial_IdempotentOnAlreadyTerminalJobWithoutResult(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusFailed
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	gotJob, gotResultID, err := ds.FinalizeFailedChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", "still broken", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, gotJob.Status)
	require.Nil(t, gotResultID)
}

func TestFinalizeChatJobWithPartial_ChatNotOwned(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, otherUserID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	_, _, err = ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", nil)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestFinalizeChatJobWithPartial_ChatDoesNotExist(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	_, _, err = ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, uuid.New(), "", "", nil)
	require.ErrorIs(t, err, ErrChatNotFound)
}

func TestFinalizeChatJobWithPartial_JobNotFound(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	_, _, err := ds.FinalizeCancelledChatJobWithPartial(ctx, userID, uuid.New(), chatID, "", "", nil)
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestFinalizeChatJobWithPartial_JobWrongOwner(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, otherUserID, jobModel)
	require.NoError(t, err)

	_, _, err = ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", nil)
	require.ErrorIs(t, err, ErrJobNotFound)
}

func TestFinalizeChatJobWithPartial_UnsupportedTerminalStatus(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	modelID := createJobTestModel(t, ds)
	chatID := createJobTestChat(t, ds, userID, modelID)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	_, _, err = ds.finalizeChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", nil, job.StatusPending, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported partial-response terminal status")
}

func TestAppendJobDraftDeltas_HappyPath(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.AppendJobDraftDeltas(ctx, userID, created.ID, []string{"a", "b"}))
	require.NoError(t, ds.AppendJobDraftDeltas(ctx, userID, created.ID, []string{"c"}))

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, got.DraftDeltas)
}

func TestAppendJobDraftDeltas_EmptyIsNoOp(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.AppendJobDraftDeltas(ctx, userID, created.ID, nil))
	require.NoError(t, ds.AppendJobDraftDeltas(ctx, userID, created.ID, []string{}))

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.DraftDeltas)
}

// TestAppendJobDraftDeltas_WrongOwnerUpdatesZeroRows mirrors UpdateJobProgress: this is a
// single scoped UPDATE with no owner pre-check, so a mismatched user updates zero rows and
// returns nil rather than a not-found error.
func TestAppendJobDraftDeltas_WrongOwnerUpdatesZeroRows(t *testing.T) {
	ds, cleanup := newJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	userID := createJobTestUser(t, ds)
	otherUserID := createJobTestUser(t, ds)
	created, err := ds.CreateJob(ctx, userID, baseJobModel())
	require.NoError(t, err)

	require.NoError(t, ds.AppendJobDraftDeltas(ctx, otherUserID, created.ID, []string{"x"}))

	got, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Empty(t, got.DraftDeltas)
}

// The following tests predate this file's sqlite-harness tranche and use the package's
// sqlmock/single-file-sqlite helpers instead; kept as-is rather than ported, to avoid losing
// their (differently-shaped) coverage of AppendJobDraftDeltas and
// FinalizeFailedChatJobWithPartial.

func TestAppendJobDraftDeltas_NoOpWhenEmpty(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	err := ds.AppendJobDraftDeltas(context.Background(), uuid.New(), uuid.New(), nil)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAppendJobDraftDeltas_UsesSingleScopedUpdate(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*user_jobs.*").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := ds.AppendJobDraftDeltas(
		context.Background(),
		uuid.New(),
		uuid.New(),
		[]string{"hello", " world"},
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAppendJobDraftDeltas_ReturnsUpdateError(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	mock.ExpectExec("UPDATE .*jobs.*draft_deltas.*").
		WillReturnError(errors.New("update failed"))

	err := ds.AppendJobDraftDeltas(context.Background(), uuid.New(), uuid.New(), []string{"chunk"})
	require.Error(t, err)
	require.ErrorContains(t, err, "update failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFinalizeFailedChatJobWithPartial_PersistsDraftAndFailure(t *testing.T) {
	ds, cleanup := newSQLiteDatastore(t)
	defer cleanup()
	createPartialJobTestTables(t, ds)
	ctx := context.Background()

	user, err := ds.dbClient.User.Create().
		SetUsername("partial-user").
		SetEmail("partial@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	chat, err := ds.dbClient.Chat.Create().SetName("Partial reply").SetOwnerID(user.ID).Save(ctx)
	require.NoError(t, err)
	created, err := ds.CreateJob(ctx, user.ID, models.Job{
		JobType:   "chat_message",
		Reference: uuid.NewString(),
		Status:    models.JobStatusProcessing,
	})
	require.NoError(t, err)
	require.NoError(t, ds.AppendJobDraftDeltas(ctx, user.ID, created.ID, []string{"A partial ", "answer."}))

	updated, resultID, err := ds.FinalizeFailedChatJobWithPartial(
		ctx, user.ID, created.ID, chat.ID, "claude-sonnet", "Scribe", "provider rejected a later tool step", nil,
	)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, updated.Status)
	require.Equal(t, "provider rejected a later tool step", updated.Error)
	require.Empty(t, updated.DraftDeltas)
	require.NotNil(t, resultID)
	require.Equal(t, resultID, updated.ResultID)

	msg, err := ds.dbClient.ChatMessage.Query().Where(chatmessage.ID(*resultID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "A partial answer.", msg.Message)
	require.Equal(t, chatmessage.OriginAssistant, msg.Origin)
	require.Equal(t, "claude-sonnet", msg.GenerationModel)
	require.Equal(t, "Scribe", msg.GenerationPersonality)
}

// createPartialJobTestTables hand-writes the tables this test needs rather than
// running Ent's migration.
//
// That hand-writing is why this test broke on main once already: one PR added
// chat_messages.context_breakdown to the Ent schema while another added the
// CREATE TABLE below without it; they merged within hours of each other, and
// every write to chat_messages here failed with "no such column". Each PR was
// green against a main that did not yet contain the other.
//
// So: a column added to ent/schema/chatmessage.go has to be added here too.
// Nothing enforces that — the compiler cannot see inside a SQL string — which is
// the standing hazard of a hand-written schema in a test.
func createPartialJobTestTables(t *testing.T, ds *Datastore) {
	t.Helper()
	for _, statement := range []string{
		`ALTER TABLE chats ADD COLUMN source text`,
		`ALTER TABLE chats ADD COLUMN import_hash text`,
		`ALTER TABLE chats ADD COLUMN rehydration_state text`,
		`CREATE TABLE jobs (
			id uuid PRIMARY KEY,
			job_type text NOT NULL,
			reference text NOT NULL,
			status text NOT NULL,
			error text,
			result_id uuid,
			draft_deltas json,
			progress text,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			user_jobs uuid NOT NULL
		)`,
		`CREATE TABLE chat_messages (
			id uuid PRIMARY KEY,
			message text NOT NULL,
			origin text NOT NULL,
			read_status text NOT NULL,
			response_id text,
			sent_at datetime NOT NULL,
			tokens integer,
			generation_model text,
			generation_personality text,
			generation_expression_reasoning text,
			last_error_message text,
			checkpoint_completed_at datetime,
			context_breakdown json,
			bookmarked boolean NOT NULL DEFAULT false,
			chat_messages uuid NOT NULL,
			chat_message_generation_mood uuid,
			chat_message_generation_expression uuid
		)`,
	} {
		_, err := ds.sqlDB.Exec(statement)
		require.NoError(t, err)
	}
}
