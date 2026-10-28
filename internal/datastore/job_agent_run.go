package datastore

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entchatmessage "github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/ent/toolcall"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"go.uber.org/zap"
)

// Advanced-run drafts (ADR 0x026). An Advanced turn saves each tool call as a ToolCall row linked
// to its job as the call finishes (CreateJobDraftToolCall), and the run's steps, plan and
// generation metadata on the job at every step boundary (UpdateJobDraftAgentRun). The reply takes
// the rows when it is saved (createToolCallsBulk); a run that fails, is stopped, or dies with the
// server still becomes a partial assistant message with its notes, plan and tool calls
// (consumeDraftAgentRunToMessageTx, from the partial-finalize paths, FinalizeAdvancedDraft and the
// startup reaper's FailInterruptedAdvancedJobs).
// Rows no message took are dropped with the draft.

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

// CreateJobDraftToolCall saves a finished tool call of an in-progress Advanced run as a ToolCall
// row linked to the run's job, with tc.ID as its id (the run's events refer to it). It writes one
// row per call, so a long run's draft never re-sends earlier tool outputs.
func (d *Datastore) CreateJobDraftToolCall(ctx context.Context, userID, jobID uuid.UUID, tc *models.ToolCall) error {
	owned, err := d.dbClient.Job.Query().Where(job.ID(jobID), job.HasOwnerWith(user.ID(userID))).Exist(ctx)
	if err != nil {
		return err
	}
	if !owned {
		return ErrJobNotFound
	}
	create := d.dbClient.ToolCall.Create().
		SetToolName(tc.ToolName).
		SetDraftJobID(jobID)
	if tc.ID != uuid.Nil {
		create.SetID(tc.ID)
	}
	setToolCallFields(create, tc)
	if _, err := create.Save(ctx); err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "draft tool call"), zap.Error(err))
		return err
	}
	return nil
}

// setToolCallFields copies a tool call's optional fields onto a create builder.
func setToolCallFields(create *ent.ToolCallCreate, tc *models.ToolCall) {
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
}

// attachJobDraftToolCallsTx moves every draft tool call of jobID onto messageID.
func attachJobDraftToolCallsTx(ctx context.Context, tx *ent.Tx, jobID, messageID uuid.UUID) error {
	_, err := tx.ToolCall.Update().
		Where(toolcall.HasDraftJobWith(job.ID(jobID))).
		SetChatMessageID(messageID).
		ClearDraftJob().
		Save(ctx)
	return err
}

// dropJobDraftToolCallsTx deletes the draft tool calls of jobID that no message took.
func dropJobDraftToolCallsTx(ctx context.Context, tx *ent.Tx, jobID uuid.UUID) error {
	_, err := tx.ToolCall.Delete().
		Where(toolcall.HasDraftJobWith(job.ID(jobID)), toolcall.Not(toolcall.HasChatMessage())).
		Exec(ctx)
	return err
}

// PartialAgentRunMessage is the reply text of an Advanced run that stopped before its final
// answer; the run's notes and tool calls carry what it did. It is also what later turns see of
// that reply in their history, which is accurate there too: it makes no promise about how the
// next turn runs (an unfinished run's open plan items reach an Advanced turn separately).
const PartialAgentRunMessage = "_The run stopped before it finished. Its working notes and tool calls are below._"

// consumeDraftAgentRunToMessageTx turns an Advanced-run draft into a partial assistant message:
// the run (with its end reason set from the terminal status) and the job's draft tool calls. The
// draft's generation metadata fills in what the caller does not know (a shutdown finalizes
// without the agent). A draft with no steps and no tool calls makes no message.
func (d *Datastore) consumeDraftAgentRunToMessageTx(
	ctx context.Context,
	tx *ent.Tx,
	chatID, jobID uuid.UUID,
	draft *modeltypes.AgentRunDraft,
	terminalStatus job.Status,
	generationModel, generationPersonality string,
	generationMoodID *uuid.UUID,
) (*uuid.UUID, error) {
	if draft == nil {
		return nil, nil
	}
	calls, err := tx.ToolCall.Query().Where(toolcall.HasDraftJobWith(job.ID(jobID))).Count(ctx)
	if err != nil {
		return nil, err
	}
	if len(draft.Run.Steps) == 0 && calls == 0 {
		return nil, nil
	}
	run := draft.Run
	if run.Version == 0 {
		run.Version = modeltypes.AgentRunVersion
	}
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
		SetMessage(PartialAgentRunMessage).
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
	if err := attachJobDraftToolCallsTx(ctx, tx, jobID, entMsg.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Chat.UpdateOneID(chatID).SetLastMessageTime(entMsg.SentAt).Save(ctx); err != nil {
		return nil, err
	}
	id := entMsg.ID
	return &id, nil
}

// FinalizeAdvancedDraft ends a turn job that has an Advanced draft with a partial reply built
// from it, as cancelled or failed (with failureMessage). handled is false, and nothing is written,
// when the job has no Advanced draft: the caller then ends the job as it always has, so Standard
// turns keep their behaviour. chatID is the job's chat; uuid.Nil resolves it from the job's
// reference (the user message it answers, or the chat itself for a scheduled run's ticket).
func (d *Datastore) FinalizeAdvancedDraft(ctx context.Context, userID, jobID, chatID uuid.UUID, cancelled bool, failureMessage string) (handled bool, err error) {
	entJob, err := d.dbClient.Job.Query().
		Where(job.ID(jobID), job.HasOwnerWith(user.ID(userID)), job.DraftAgentRunNotNil()).
		Select(job.FieldID, job.FieldReference).
		Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if chatID == uuid.Nil {
		if chatID, err = d.chatOfTurnJob(ctx, userID, entJob.Reference); err != nil || chatID == uuid.Nil {
			return false, err
		}
	}
	if cancelled {
		_, _, err = d.FinalizeCancelledChatJobWithPartial(ctx, userID, jobID, chatID, "", "", nil)
	} else {
		_, _, err = d.FinalizeFailedChatJobWithPartial(ctx, userID, jobID, chatID, "", "", failureMessage, nil)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// chatOfTurnJob resolves a turn job's chat from its reference: a chat_message job references the
// user message it answers, a scheduled run's ticket references the chat. uuid.Nil when neither.
func (d *Datastore) chatOfTurnJob(ctx context.Context, userID uuid.UUID, reference string) (uuid.UUID, error) {
	id, err := uuid.Parse(reference)
	if err != nil {
		return uuid.Nil, nil
	}
	chatID, err := d.dbClient.ChatMessage.Query().
		Where(entchatmessage.ID(id), entchatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID)))).
		QueryChat().OnlyID(ctx)
	if err == nil {
		return chatID, nil
	}
	if !ent.IsNotFound(err) {
		return uuid.Nil, err
	}
	exists, err := d.dbClient.Chat.Query().Where(entchat.ID(id), entchat.HasOwnerWith(user.ID(userID))).Exist(ctx)
	if err != nil || !exists {
		return uuid.Nil, err
	}
	return id, nil
}

// FailInterruptedAdvancedJobs finalizes, as failed with a partial reply, the interrupted turn
// jobs (pending or processing, not updated since olderThan) that hold an Advanced draft. The
// startup reaper calls it before FailInterruptedJobs, so a run killed by a crash or OOM keeps its
// work. It returns how many it finalized.
func (d *Datastore) FailInterruptedAdvancedJobs(ctx context.Context, jobTypes []string, olderThan time.Time, message string) (int, error) {
	if d == nil || d.dbClient == nil || len(jobTypes) == 0 {
		return 0, nil
	}
	rows, err := d.dbClient.Job.Query().
		Where(
			job.JobTypeIn(jobTypes...),
			job.StatusIn(job.Status(string(models.JobStatusPending)), job.Status(string(models.JobStatusProcessing))),
			job.UpdatedAtLT(olderThan),
			job.DraftAgentRunNotNil(),
		).
		WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID) }).
		Select(job.FieldID).
		All(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if r.Edges.Owner == nil {
			continue
		}
		handled, err := d.FinalizeAdvancedDraft(ctx, r.Edges.Owner.ID, r.ID, uuid.Nil, false, message)
		if err != nil {
			d.logger.Warn("reaper: failed to finalize an interrupted Advanced run", zap.String("job_id", r.ID.String()), zap.Error(err))
			continue
		}
		if handled {
			n++
		}
	}
	return n, nil
}
