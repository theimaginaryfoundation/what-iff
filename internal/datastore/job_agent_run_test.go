package datastore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	entjob "github.com/theimaginaryfoundation/what-iff/ent/job"
	enttoolcall "github.com/theimaginaryfoundation/what-iff/ent/toolcall"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// advancedDraftJob creates a processing chat_message job with an Advanced draft whose one step
// refers to a draft tool call saved on the job.
func advancedDraftJob(t *testing.T, ds *Datastore, userID uuid.UUID) (*models.Job, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	created, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)

	callID := uuid.New()
	require.NoError(t, ds.CreateJobDraftToolCall(ctx, userID, created.ID, &models.ToolCall{
		ID: callID, ToolName: "web_search", ToolInput: `{"q":"x"}`, ToolOutput: "results", Step: 1,
	}))
	require.NoError(t, ds.UpdateJobDraftAgentRun(ctx, userID, created.ID, &modeltypes.AgentRunDraft{
		Run: modeltypes.AgentRun{
			Version: modeltypes.AgentRunVersion,
			Steps: []modeltypes.AgentRunStep{{Step: 1, Events: []modeltypes.AgentRunEvent{
				{Kind: modeltypes.AgentRunEventText, Text: "looking"},
				{Kind: modeltypes.AgentRunEventToolCall, ToolCallID: callID.String()},
			}}},
			Plan: []modeltypes.PlanItem{{Content: "research", Status: modeltypes.PlanItemInProgress}},
		},
		GenerationModel: "gpt-test",
	}))
	return created, callID
}

// An Advanced run that fails keeps its saved draft: the partial message carries the run (end
// reason set from the failure), the draft tool calls (moved onto it, ids kept), and the draft's
// generation metadata when the caller has none (a server shutdown).
func TestFinalizeFailedChatJobWithPartial_AdvancedDraftBecomesMessage(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createJobTestUser(t, ds)
	chatID := createJobTestChat(t, ds, userID, createJobTestModel(t, ds))
	created, callID := advancedDraftJob(t, ds, userID)

	gotJob, resultID, err := ds.FinalizeFailedChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", "Interrupted by a server shutdown", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, gotJob.Status)
	require.NotNil(t, resultID)

	msg, err := ds.dbClient.ChatMessage.Get(ctx, *resultID)
	require.NoError(t, err)
	require.Equal(t, PartialAgentRunMessage, msg.Message)
	require.Equal(t, "gpt-test", msg.GenerationModel, "the draft's metadata fills in for a shutdown")
	require.NotNil(t, msg.AgentRun)
	require.Equal(t, modeltypes.AgentRunVersion, msg.AgentRun.Version)
	require.Equal(t, modeltypes.AgentRunEndFailed, msg.AgentRun.EndReason)
	require.Equal(t, callID.String(), msg.AgentRun.Steps[0].Events[1].ToolCallID)

	call, err := ds.dbClient.ToolCall.Query().Where(enttoolcall.ID(callID)).WithChatMessage().Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, call.Edges.ChatMessage)
	require.Equal(t, *resultID, call.Edges.ChatMessage.ID, "the draft tool call moved onto the partial")
	require.Equal(t, 1, call.Step)
	require.False(t, ds.dbClient.ToolCall.Query().Where(enttoolcall.ID(callID), enttoolcall.HasDraftJob()).ExistX(ctx))
}

// A cancelled run's draft ends as cancelled; an empty draft makes no message.
func TestConsumeDraftAgentRun_CancelledAndEmpty(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createJobTestUser(t, ds)
	chatID := createJobTestChat(t, ds, userID, createJobTestModel(t, ds))
	created, _ := advancedDraftJob(t, ds, userID)

	_, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "gpt-test", "Vix", nil)
	require.NoError(t, err)
	require.NotNil(t, resultID)
	msg, err := ds.dbClient.ChatMessage.Get(ctx, *resultID)
	require.NoError(t, err)
	require.Equal(t, modeltypes.AgentRunEndCancelled, msg.AgentRun.EndReason)
	require.Equal(t, "Vix", msg.GenerationPersonality)

	jobModel := baseJobModel()
	jobModel.JobType = "chat_message"
	jobModel.Status = models.JobStatusProcessing
	empty, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)
	require.NoError(t, ds.UpdateJobDraftAgentRun(ctx, userID, empty.ID, &modeltypes.AgentRunDraft{}))
	_, resultID, err = ds.FinalizeFailedChatJobWithPartial(ctx, userID, empty.ID, chatID, "", "", "boom", nil)
	require.NoError(t, err)
	require.Nil(t, resultID, "a draft with nothing in it makes no message")
}

// N4: Stop handled by another instance (MarkChatJobCancelled) leaves the Advanced draft, and the
// worker's own finalize, reaching the idempotent terminal path, still saves the partial.
func TestMarkChatJobCancelled_KeepsAdvancedDraftForTheWorker(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createJobTestUser(t, ds)
	chatID := createJobTestChat(t, ds, userID, createJobTestModel(t, ds))
	created, callID := advancedDraftJob(t, ds, userID)

	cancelled, err := ds.MarkChatJobCancelled(ctx, userID, created.ID)
	require.NoError(t, err)
	require.True(t, cancelled)

	gotJob, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "gpt-test", "", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusCancelled, gotJob.Status)
	require.NotNil(t, resultID, "the idempotent path saved the partial")
	require.Equal(t, *resultID, *gotJob.ResultID)
	require.True(t, ds.dbClient.ToolCall.Query().Where(enttoolcall.ID(callID), enttoolcall.HasChatMessage()).ExistX(ctx))
}

// N4: the startup reaper finalizes an interrupted Advanced run into a partial before failing the
// rest; ClearJobDraftDeltas drops draft tool calls no message took.
func TestFailInterruptedAdvancedJobs_SavesPartialAndClearDropsOrphans(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createJobTestUser(t, ds)
	chatID := createJobTestChat(t, ds, userID, createJobTestModel(t, ds))
	userMsg, err := ds.dbClient.ChatMessage.Create().SetMessage("go").SetOrigin("User").SetChatID(chatID).Save(ctx)
	require.NoError(t, err)
	created, _ := advancedDraftJob(t, ds, userID)
	require.NoError(t, ds.dbClient.Job.UpdateOneID(created.ID).SetReference(userMsg.ID.String()).Exec(ctx))

	n, err := ds.FailInterruptedAdvancedJobs(ctx, []string{"chat_message"}, time.Now().Add(time.Minute), "Interrupted by a server restart")
	require.NoError(t, err)
	require.Equal(t, 1, n)
	reaped, err := ds.dbClient.Job.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, entjob.StatusFailed, reaped.Status)
	require.NotNil(t, reaped.ResultID)

	// A job whose reply was saved without taking a draft row: clearing drops that row.
	other, _ := advancedDraftJob(t, ds, userID)
	require.NoError(t, ds.ClearJobDraftDeltas(ctx, userID, other.ID))
	require.False(t, ds.dbClient.ToolCall.Query().Where(enttoolcall.HasDraftJobWith(entjob.ID(other.ID))).ExistX(ctx))
}

// GetJob, which the client polls, does not read the internal draft.
func TestGetJob_OmitsAdvancedDraft(t *testing.T) {
	ds, cleanup := newFinalizeChatJobTestDatastore(t)
	defer cleanup()
	userID := createJobTestUser(t, ds)
	created, _ := advancedDraftJob(t, ds, userID)

	got, err := ds.GetJob(context.Background(), userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, models.JobStatusProcessing, got.Status)
	require.NotContains(t, jobReadColumns, entjob.FieldDraftAgentRun)
}
