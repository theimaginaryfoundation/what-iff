package datastore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	enttoolcall "github.com/theimaginaryfoundation/what-iff/ent/toolcall"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// An Advanced run that fails keeps its saved draft: the partial message carries the run (end
// reason set from the failure), its tool calls with their steps, and the draft's generation
// metadata when the caller has none (a server shutdown).
func TestFinalizeFailedChatJobWithPartial_AdvancedDraftBecomesMessage(t *testing.T) {
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

	draft := &modeltypes.AgentRunDraft{
		Run: modeltypes.AgentRun{
			Steps: []modeltypes.AgentRunStep{{Step: 1, Notes: []modeltypes.AgentRunNote{{Kind: modeltypes.AgentRunNoteText, Text: "looking"}}}},
			Plan:  []modeltypes.PlanItem{{Content: "research", Status: modeltypes.PlanItemInProgress}},
		},
		ToolCalls:       []modeltypes.AgentRunToolCall{{ToolName: "web_search", ToolInput: `{"q":"x"}`, ToolOutput: "results", Step: 1}},
		GenerationModel: "gpt-test",
	}
	require.NoError(t, ds.UpdateJobDraftAgentRun(ctx, userID, created.ID, draft))

	gotJob, resultID, err := ds.FinalizeFailedChatJobWithPartial(ctx, userID, created.ID, chatID, "", "", "Interrupted by a server shutdown", nil)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusFailed, gotJob.Status)
	require.NotNil(t, resultID)

	msg, err := ds.dbClient.ChatMessage.Get(ctx, *resultID)
	require.NoError(t, err)
	require.Equal(t, partialAgentRunMessage, msg.Message)
	require.Equal(t, "gpt-test", msg.GenerationModel, "the draft's metadata fills in for a shutdown")
	require.NotNil(t, msg.AgentRun)
	require.Equal(t, modeltypes.AgentRunEndFailed, msg.AgentRun.EndReason)
	require.Equal(t, "looking", msg.AgentRun.Steps[0].Notes[0].Text)
	require.Equal(t, modeltypes.PlanItemInProgress, msg.AgentRun.Plan[0].Status)

	calls, err := ds.dbClient.ToolCall.Query().Where(enttoolcall.HasChatMessageWith()).All(ctx)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "web_search", calls[0].ToolName)
	require.Equal(t, 1, calls[0].Step)

	reread, err := ds.GetJob(ctx, userID, created.ID)
	require.NoError(t, err)
	require.Equal(t, *resultID, *reread.ResultID)
}

// A cancelled run's draft ends as cancelled; an empty draft makes no message.
func TestConsumeDraftAgentRun_CancelledAndEmpty(t *testing.T) {
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
	require.NoError(t, ds.UpdateJobDraftAgentRun(ctx, userID, created.ID, &modeltypes.AgentRunDraft{
		Run: modeltypes.AgentRun{Steps: []modeltypes.AgentRunStep{{Step: 1, Notes: []modeltypes.AgentRunNote{{Kind: modeltypes.AgentRunNoteReasoning, Text: "hmm"}}}}},
	}))
	_, resultID, err := ds.FinalizeCancelledChatJobWithPartial(ctx, userID, created.ID, chatID, "gpt-test", "Vix", nil)
	require.NoError(t, err)
	require.NotNil(t, resultID)
	msg, err := ds.dbClient.ChatMessage.Get(ctx, *resultID)
	require.NoError(t, err)
	require.Equal(t, modeltypes.AgentRunEndCancelled, msg.AgentRun.EndReason)
	require.Equal(t, "Vix", msg.GenerationPersonality)

	empty, err := ds.CreateJob(ctx, userID, jobModel)
	require.NoError(t, err)
	require.NoError(t, ds.UpdateJobDraftAgentRun(ctx, userID, empty.ID, &modeltypes.AgentRunDraft{}))
	_, resultID, err = ds.FinalizeFailedChatJobWithPartial(ctx, userID, empty.ID, chatID, "", "", "boom", nil)
	require.NoError(t, err)
	require.Nil(t, resultID, "a draft with nothing in it makes no message")
}
