package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// Taking a turn back. SupersedeTurn marks a turn's user message and its replies superseded;
// RegenerateTurn supersedes only the replies and answers the same user message again. Superseded
// messages are kept as past versions, but nothing that builds a turn sees them, because every
// read of a chat's messages leaves them out (see datastore/chatmessage_supersede.go):
//
//   - the model's history and the checkpoint carry-over (messageContextBuilder, through
//     ListChatMessages), on every provider and in an Advanced run, whose rebuilds start from the
//     turn's context;
//   - the previous Advanced run's open plan (previousRunOpenPlan), the first-message checks
//     (ChatHasMessages for the user-name line, isFirstUserMessageInChat), the checkpoint's
//     assistant count, window start and skill/tool bookkeeping, thread rehydration and find_context;
//   - later memory extraction: a checkpoint extracts from the conversation its turn replayed (the
//     OpenAI path chains off that turn's own response, whose input was built without them), so a
//     superseded message never reaches it.
//
// No turn continues a Responses chain from an earlier turn: each OpenAI turn sends its whole
// context and starts its adapter with no previous_response_id, and a checkpoint chains only off the
// response of the turn it ends. chat.response_id (written after each turn, read by nothing that
// builds a turn) is cleared anyway when a turn is superseded, so it never names a superseded reply.
//
// What cannot be undone: memories, the scratchpad and the checkpoint summary already written by a
// checkpoint that ran at the end of the turn (or of a later one, before the supersede), the side
// effects of the tools it ran (files, created memories, sent messages), and the usage it was
// metered for.
//
// Only the latest turn in a chat may be taken back or regenerated (datastore.ErrTurnNotLatest).
// Which messages are a turn's replies, and how a reply its run saves late is settled, is decided by
// run, under a lock on the user message: see datastore/chatmessage_supersede.go.

var (
	// ErrTurnRunning: a run of the turn (or another turn in the chat) has not finished; a turn can
	// only be regenerated once it has. SupersedeTurn stops it instead.
	ErrTurnRunning = datastore.ErrTurnRunning
	// ErrTurnNotLatest: only the latest turn in a chat can be taken back or regenerated.
	ErrTurnNotLatest = datastore.ErrTurnNotLatest
	// ErrTurnNotUserMessage: a turn is named by its user message, not by a reply.
	ErrTurnNotUserMessage = datastore.ErrTurnNotUserMessage
	// ErrTurnSuperseded: the turn was taken back, so it cannot be regenerated.
	ErrTurnSuperseded = datastore.ErrTurnSuperseded
)

// SupersedeTurn takes back the turn userMessageID starts: the user message and its replies are
// marked superseded (datastore.SupersedeTurn), and then the turn's own runs that have not finished
// are stopped, each as Stop stops a job (in this process through its cancel func, otherwise by
// marking it cancelled for the instance running it). Only this turn's runs: a scheduled run in the
// chat, or an earlier turn still answering, keeps running. A reply a stopped run saves while it
// winds down is superseded as it is saved. The turn is marked first, so a take-back refused
// (ErrTurnNotLatest) stops nothing. Superseding a turn already taken back succeeds and changes
// nothing (its unfinished runs are stopped again).
//
// A run that fails to stop is logged, not returned: the turn is taken back by then, and whatever
// that run saves is superseded on arrival.
//
// Errors: datastore.ErrChatMessageNotFound (not the user's), ErrTurnNotUserMessage, ErrTurnNotLatest.
func (a *Agent) SupersedeTurn(ctx context.Context, userID, userMessageID uuid.UUID) error {
	running, err := a.ds.SupersedeTurn(ctx, userID, userMessageID, time.Now())
	if err != nil {
		return err
	}
	for _, jobID := range running {
		if err := a.cancelChatJob(ctx, userID, jobID); err != nil {
			a.logger.Warn("supersede: failed to stop a run of the turn taken back",
				zap.String("chat_message_id", userMessageID.String()),
				zap.String("job_id", jobID.String()),
				zap.Error(err))
		}
	}
	return nil
}

// RegenerateTurn supersedes the replies of the turn userMessageID starts and answers the same user
// message again, as a new chat_message job run with opts (nil: an ordinary run). opts reach the run
// as a new send's do (ChatMessage.TurnOptions: pre/post context, one-turn model, output cap,
// memory switches); they are never stored, so pass them again on every regenerate that should
// have them. The new reply is recorded as superseded_by on the replaced ones when it is saved; a
// run that saves no reply leaves them unlinked. ctx carries the user, timezone and turn source as
// for RetryUserChatMessage.
//
// The replies are superseded and the job is created in one transaction, under a lock on the user
// message that also re-checks that none of the turn's runs is unfinished
// (datastore.RegenerateTurn), so two regenerates of one turn start one run between them.
//
// Memory saving can be switched off for the new run (opts.MemorySave false flags the user message,
// as a send would), not back on: a user message flagged when it was sent stays flagged.
//
// Errors: datastore.ErrChatMessageNotFound, ErrTurnNotUserMessage, ErrTurnNotLatest,
// ErrTurnSuperseded, ErrTurnRunning.
func (a *Agent) RegenerateTurn(ctx context.Context, userMessageID uuid.UUID, opts *models.TurnOptions) (*models.ChatMessageResponse, error) {
	detachedCtx, userID, err := detachTurnContext(ctx)
	if err != nil {
		return nil, err
	}
	turn, err := a.ds.CheckTurnRevisable(detachedCtx, userID, userMessageID)
	if err != nil {
		return nil, err
	}
	if turn.Superseded {
		return nil, ErrTurnSuperseded
	}
	// Nothing may be running in the chat, as a client following the chat sees it (and as the turn
	// gate does): a turn left non-terminal by a worker that died long ago does not count. The
	// turn's own runs are checked again under the lock, race-free, as the job is created.
	running, err := a.ds.FindActiveTurnJobForChat(detachedCtx, userID, turn.ChatID)
	if err != nil {
		return nil, fmt.Errorf("regenerate: look for a running turn: %w", err)
	}
	if running != nil {
		return nil, ErrTurnRunning
	}
	newJob, err := a.ds.RegenerateTurn(detachedCtx, userID, userMessageID, !opts.SavesMemory(), time.Now())
	if err != nil {
		return nil, err
	}
	msg, err := a.ds.GetChatMessage(detachedCtx, userID, userMessageID)
	if err != nil {
		// The job exists but will not run: end it, so it does not hold the turn as running.
		if _, ferr := a.ds.UpdateJobStatus(detachedCtx, userID, newJob.ID, models.JobStatusFailed, "regenerate: load the user message"); ferr != nil {
			a.logger.Warn("regenerate: failed to fail the job that could not start", zap.String("job_id", newJob.ID.String()), zap.Error(ferr))
		}
		return nil, err
	}
	// Request-only, never stored: carried to the run in memory, as HandleUserMessage does.
	msg.TurnOptions = opts
	return a.startUserMessageRun(detachedCtx, userID, newJob, msg, "regenerate"), nil
}

// settleTurnReply settles the reply a turn just saved (datastore.SettleTurnReply), by the run that
// saved it (chatCtx's turn job): superseded at once when the turn was taken back or regenerated
// while it ran, otherwise recorded on its run and linked as superseded_by from the replies a
// regenerate replaced. Best effort: a failure is logged, and the reply stays as saved.
func (a *Agent) settleTurnReply(ctx context.Context, userID uuid.UUID, chatCtx *chatContext, userMessage *models.ChatMessage, replyID uuid.UUID) {
	if a.ds == nil || userMessage == nil || replyID == uuid.Nil {
		return
	}
	if err := a.ds.SettleTurnReply(context.WithoutCancel(ctx), userID, userMessage.ChatID, userMessage.ID, chatCtx.turnJobID(), replyID); err != nil {
		a.logger.Warn("failed to settle a turn's reply (superseded state)",
			zap.String("chat_message_id", userMessage.ID.String()),
			zap.String("reply_id", replyID.String()),
			zap.Error(err))
	}
}

// turnJobID is the job running the turn (turnID), or uuid.Nil when there is none.
func (c *chatContext) turnJobID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	id, err := uuid.Parse(c.turnID)
	if err != nil {
		return uuid.Nil
	}
	return id
}
