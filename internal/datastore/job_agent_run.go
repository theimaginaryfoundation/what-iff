package datastore

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchatmessage "github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"go.uber.org/zap"
)

// Advanced-run drafts (ADR 0x026). An Advanced turn persists its run on its job at every step
// boundary (UpdateJobDraftAgentRun), so a run that fails, is stopped, or dies with the server
// still becomes a partial assistant message (consumeDraftAgentRunToMessageTx, from the partial
// finalize path) with its notes, plan and finished tool calls, instead of nothing.

// UpdateJobDraftAgentRun replaces the job's Advanced-run draft.
func (d *Datastore) UpdateJobDraftAgentRun(ctx context.Context, userID, jobID uuid.UUID, draft *modeltypes.AgentRunDraft) error {
	_, err := d.dbClient.Job.Update().
		Where(job.ID(jobID), job.HasOwnerWith(user.ID(userID))).
		SetDraftAgentRun(draft).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "job draft_agent_run"), zap.Error(err))
	}
	return err
}

// partialAgentRunMessage is the reply text of an Advanced run that stopped before its final
// answer; the run's notes and tool calls carry what it did.
const partialAgentRunMessage = "_The run stopped before it finished. Its working notes and tool calls are below; ask me to continue to pick up the open items._"

// consumeDraftAgentRunToMessageTx turns an Advanced-run draft into a partial assistant message:
// the run (with its end reason set from the terminal status) and its finished tool calls. The
// draft's generation metadata fills in what the caller does not know (a shutdown finalizes
// without the agent). A draft with nothing in it makes no message.
func (d *Datastore) consumeDraftAgentRunToMessageTx(
	ctx context.Context,
	tx *ent.Tx,
	chatID uuid.UUID,
	draft *modeltypes.AgentRunDraft,
	terminalStatus job.Status,
	generationModel, generationPersonality string,
	generationMoodID *uuid.UUID,
) (*uuid.UUID, error) {
	if draft == nil || (len(draft.Run.Steps) == 0 && len(draft.ToolCalls) == 0) {
		return nil, nil
	}
	run := draft.Run
	if run.EndReason == "" || run.EndReason == modeltypes.AgentRunEndDone {
		run.EndReason = modeltypes.AgentRunEndFailed
		if terminalStatus == job.StatusCancelled {
			run.EndReason = modeltypes.AgentRunEndCancelled
		}
	}
	if generationModel == "" {
		generationModel = draft.GenerationModel
	}
	if generationPersonality == "" {
		generationPersonality = draft.GenerationPersonality
	}
	if generationMoodID == nil && draft.GenerationMoodID != "" {
		if id, err := uuid.Parse(draft.GenerationMoodID); err == nil {
			generationMoodID = &id
		}
	}

	createMsg := tx.ChatMessage.Create().
		SetMessage(partialAgentRunMessage).
		SetOrigin(entchatmessage.OriginAssistant).
		SetReadStatus(entchatmessage.ReadStatusUnread).
		SetChatID(chatID).
		SetAgentRun(&run)
	if generationModel != "" {
		createMsg.SetGenerationModel(generationModel)
	}
	if generationPersonality != "" {
		createMsg.SetGenerationPersonality(generationPersonality)
	}
	if generationMoodID != nil && *generationMoodID != uuid.Nil {
		createMsg.SetGenerationMoodID(*generationMoodID)
	}
	entMsg, err := createMsg.Save(ctx)
	if err != nil {
		return nil, err
	}
	if len(draft.ToolCalls) > 0 {
		creates := make([]*ent.ToolCallCreate, 0, len(draft.ToolCalls))
		for _, tc := range draft.ToolCalls {
			create := tx.ToolCall.Create().SetToolName(tc.ToolName).SetChatMessageID(entMsg.ID)
			if strings.TrimSpace(tc.ToolInput) != "" {
				create.SetToolInput(tc.ToolInput)
			}
			if tc.ToolOutput != "" {
				create.SetToolOutput(tc.ToolOutput)
			}
			if tc.ToolError != "" {
				create.SetToolError(tc.ToolError)
			}
			if tc.Step > 0 {
				create.SetStep(tc.Step)
			}
			creates = append(creates, create)
		}
		if _, err := tx.ToolCall.CreateBulk(creates...).Save(ctx); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Chat.UpdateOneID(chatID).SetLastMessageTime(entMsg.SentAt).Save(ctx); err != nil {
		return nil, err
	}
	id := entMsg.ID
	return &id, nil
}
