package agent

import (
	"context"
	"errors"
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

var (
	// ErrTurnRunning: the turn's run (or another turn in the chat) has not finished; a turn can only
	// be regenerated once it has. SupersedeTurn stops it instead.
	ErrTurnRunning = errors.New("turn still running")
	// ErrTurnNotLatest: only the latest turn in a chat can be taken back or regenerated.
	ErrTurnNotLatest = datastore.ErrTurnNotLatest
	// ErrTurnNotUserMessage: a turn is named by its user message, not by a reply.
	ErrTurnNotUserMessage = datastore.ErrTurnNotUserMessage
	// ErrTurnSuperseded: the turn was taken back, so it cannot be regenerated.
	ErrTurnSuperseded = datastore.ErrTurnSuperseded
)

// SupersedeTurn takes back the turn userMessageID starts: the user message and its replies are
// marked superseded. A turn still running is stopped first, as the thread's Stop does (every running
// turn in the chat; with only the latest turn revisable, they are this one); a reply its run saves
// while it winds down is superseded as it is saved. Superseding a turn already taken back succeeds
// and changes nothing.
//
// Errors: datastore.ErrChatMessageNotFound (not the user's), ErrTurnNotUserMessage, ErrTurnNotLatest.
func (a *Agent) SupersedeTurn(ctx context.Context, userID, userMessageID uuid.UUID) error {
	turn, err := a.ds.CheckTurnRevisable(ctx, userID, userMessageID)
	if err != nil {
		return err
	}
	if turn.Superseded {
		return nil
	}
	running, err := a.ds.ListActiveChatJobIDsForChat(ctx, userID, turn.ChatID)
	if err != nil {
		return fmt.Errorf("supersede: list running turns: %w", err)
	}
	if len(running) > 0 {
		// CancelJob stops the whole thread, as Stop does.
		if err := a.CancelJob(ctx, userID, running[0]); err != nil {
			return fmt.Errorf("supersede: stop the running turn: %w", err)
		}
	}
	return a.ds.SupersedeTurn(ctx, userID, userMessageID, false, time.Now())
}

// RegenerateTurn supersedes the replies of the turn userMessageID starts and answers the same user
// message again, as a new chat_message job run with opts (nil: an ordinary run). The new reply is
// recorded as superseded_by on the replaced ones when it is saved; a run that saves no reply leaves
// them unlinked. ctx carries the user, timezone and turn source as for RetryUserChatMessage.
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
	// Running as a client following the chat sees it (and as the turn gate does): a turn left
	// non-terminal by a worker that died long ago does not count.
	running, err := a.ds.FindActiveTurnJobForChat(detachedCtx, userID, turn.ChatID)
	if err != nil {
		return nil, fmt.Errorf("regenerate: look for a running turn: %w", err)
	}
	if running != nil {
		return nil, ErrTurnRunning
	}
	if err := a.ds.SupersedeTurn(detachedCtx, userID, userMessageID, true, time.Now()); err != nil {
		return nil, err
	}
	if !opts.SavesMemory() && !turn.ExcludedFromMemory {
		if err := a.ds.MarkChatMessageExcludedFromMemory(detachedCtx, userID, userMessageID); err != nil {
			return nil, fmt.Errorf("regenerate: flag the message out of memory: %w", err)
		}
	}
	msg, err := a.ds.GetChatMessage(detachedCtx, userID, userMessageID)
	if err != nil {
		return nil, err
	}
	// Request-only, never stored: carried to the run in memory, as HandleUserMessage does.
	msg.TurnOptions = opts
	return a.rerunUserMessage(detachedCtx, userID, msg, "regenerate")
}

// settleTurnReply records the reply a turn just saved (datastore.SettleTurnReply): superseded at
// once when the turn was taken back while it ran, or linked as superseded_by from the replies a
// regenerate replaced. Best effort: a failure is logged, and the reply stays as saved.
func (a *Agent) settleTurnReply(ctx context.Context, userID uuid.UUID, userMessage *models.ChatMessage, replyID uuid.UUID) {
	if a.ds == nil || userMessage == nil || replyID == uuid.Nil {
		return
	}
	if err := a.ds.SettleTurnReply(context.WithoutCancel(ctx), userID, userMessage.ChatID, userMessage.ID, replyID); err != nil {
		a.logger.Warn("failed to settle a turn's reply (superseded state)",
			zap.String("chat_message_id", userMessage.ID.String()),
			zap.String("reply_id", replyID.String()),
			zap.Error(err))
	}
}
