package datastore

import (
	"context"
	"errors"
	"slices"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// Taking a turn back (superseding it). A turn is a user message and the reply (or replies) that
// answer it. Superseding marks them superseded_at: they are kept as past versions, but left out of
// everything that lists a chat's messages (the model's history and carry-over, the message routes,
// counts, exports, bookmarks; listChatMessages and the rest filter on superseded_at IS NULL).
// Regenerating a turn supersedes only its replies and runs the same user message again; the new
// reply is recorded on the old ones as superseded_by once it is saved (SettleTurnReply).
//
// Only the latest turn in a chat can be taken back or regenerated: superseding a turn in the
// middle of a conversation would leave the turns after it answering something the model can no
// longer see. "Latest" is the newest live (not superseded) user message, by (sent_at, id).
//
// Replies come from runs. A turn's runs are its turn jobs (turnJobTypes) whose reference is the
// user message: the chat_message jobs of the send, retries and regenerates, and the agent_job_run
// of the synchronous path (agent.HandleUserMessageSync). Its replies are what those runs saved:
// every run records its reply on its job's result_id, a partial reply when it is stopped or fails
// (finalizeChatJobWithPartial), a full one as it is saved (SettleTurnReply). So other assistant
// messages after the user message are never taken for the turn's: a webhook's post, a scheduled
// run's reply or the scheduler's notice, an earlier turn's run still answering (queued turns). Only
// a turn that has no run at all, imported history, falls back to position: the assistant messages
// after it, except a scheduled run's (job_run set), which answers its own prompt.
//
// Settled by run, under a row lock. Superseding, regenerating and settling a reply all lock the
// turn's user message row (SELECT ... FOR UPDATE) first, so they run one at a time per turn:
//
//   - SettleTurnReply, run as each reply is saved (and in the partial reply's own transaction),
//     supersedes the reply on arrival when the turn was taken back, or when its run is no longer
//     the turn's newest (a regenerate started since: a Stop handled on another instance marks the
//     run cancelled at once, but its worker may still save a reply up to a poll interval later).
//     Otherwise it records the reply on its run (result_id), where a later supersede finds it.
//   - SupersedeTurn and RegenerateTurn supersede the replies the runs recorded. A reply saved but
//     not yet settled when they run is settled after them, and superseded then.
//   - RegenerateTurn re-checks, under the lock, that no run of the turn is unfinished, and creates
//     the new run's job in the same transaction, so two regenerates (a double click, a retried
//     request) cannot both start a run: the second waits for the first's lock and then sees its
//     run.
//
// Lock order, to stay free of deadlocks: the user message row first, then the chat row and job
// rows. The partial-reply transaction takes the user message lock before it writes the chat row
// (finalizeChatJobWithPartial). On SQLite (the tests) there are no row locks (lockRowForUpdate
// leaves the clause out); SQLite runs one write transaction at a time, and a transaction that read
// before another committed cannot write after it (SQLITE_BUSY), so a race there ends in an error
// rather than two runs or a live late reply.
//
// Steering (ADR 0x042): a message steered into a running turn has its own run, which the running
// turn claims and which saves no reply. The reply that took the message in belongs to the run that
// saved it, so it stays with that run's turn: taking back the steered message hides that message
// only, and regenerating it answers it alone, after that reply.
//
// What superseding cannot undo: memories, the scratchpad and the checkpoint summary a checkpoint
// already wrote from the turn, files or other side effects of the tools it ran, and the usage it
// was metered for. Later checkpoints never see a superseded message (it is not in the history they
// replay), but what was extracted before the supersede stays.

var (
	// ErrTurnNotUserMessage: a turn is named by its user message; this message is a reply.
	ErrTurnNotUserMessage = errors.New("turn: the message is not a user message")
	// ErrTurnNotLatest: the turn is not the latest in its chat; only the latest can be taken back.
	ErrTurnNotLatest = errors.New("turn: not the latest turn in the chat")
	// ErrTurnSuperseded: the turn was taken back, so it cannot be regenerated.
	ErrTurnSuperseded = errors.New("turn: the turn was taken back")
	// ErrTurnRunning: a run of the turn has not finished, so it cannot be regenerated yet.
	ErrTurnRunning = errors.New("turn still running")
)

// RevisableTurn is a user message that SupersedeTurn may act on, as CheckTurnRevisable found it.
type RevisableTurn struct {
	ChatID uuid.UUID
	// Superseded: the user message was already taken back. It is then not checked for being the
	// latest: superseding it again is a no-op.
	Superseded bool
	// ExcludedFromMemory: the user message is flagged chat_message.exclude_from_memory.
	ExcludedFromMemory bool
}

// sentAfter matches messages after (sentAt, id) in the order chats list messages in.
func sentAfter(sentAt time.Time, id uuid.UUID) predicate.ChatMessage {
	return chatmessage.Or(
		chatmessage.SentAtGT(sentAt),
		chatmessage.And(chatmessage.SentAtEQ(sentAt), chatmessage.IDGT(id)),
	)
}

// turnRuns matches the runs of the turn userMessageID starts (see the note above).
func turnRuns(userID, userMessageID uuid.UUID) []predicate.Job {
	return []predicate.Job{
		job.JobTypeIn(turnJobTypes...),
		ownedByUser(userID),
		job.ReferenceEQ(userMessageID.String()),
	}
}

// unfinishedTurnRun matches a run still at work: before its reply (pending or processing) and
// written within TurnJobStaleAfter, or in its post-inference phases. A run left pending or
// processing by a worker that died is not: the turn gate and FindActiveTurnJobForChat skip it too,
// and should its worker wake after all, a reply it saves is superseded on arrival (a newer run).
func unfinishedTurnRun(now time.Time) predicate.Job {
	return job.Or(
		job.And(job.StatusIn(job.StatusPending, job.StatusProcessing), job.UpdatedAtGTE(now.Add(-TurnJobStaleAfter))),
		job.StatusIn(job.StatusInferenceComplete, job.StatusExpressionComplete, job.StatusCompactionComplete),
	)
}

// newestTurnRun returns the newest of a turn's runs, by (created_at, id) as LatestChatMessageJobID
// orders them, or nil for none. A turn has a handful of runs; they are compared here rather than
// in SQL so the order does not depend on how a driver compares stored times.
func newestTurnRun(runs []*ent.Job) *ent.Job {
	var newest *ent.Job
	for _, r := range runs {
		if newest == nil || r.CreatedAt.After(newest.CreatedAt) ||
			(r.CreatedAt.Equal(newest.CreatedAt) && r.ID.String() > newest.ID.String()) {
			newest = r
		}
	}
	return newest
}

// turnRunReply matches the replies the turn's runs recorded on their jobs (result_id).
func turnRunReply(userID, userMessageID uuid.UUID) predicate.ChatMessage {
	types := make([]any, 0, len(turnJobTypes))
	for _, t := range turnJobTypes {
		types = append(types, t)
	}
	return func(s *sql.Selector) {
		runs := sql.Table(job.Table)
		s.Where(sql.In(s.C(chatmessage.FieldID),
			sql.Select(runs.C(job.FieldResultID)).From(runs).Where(sql.And(
				sql.In(runs.C(job.FieldJobType), types...),
				sql.EQ(runs.C(job.OwnerColumn), userID),
				sql.EQ(runs.C(job.FieldReference), userMessageID.String()),
				sql.NotNull(runs.C(job.FieldResultID)),
			)),
		))
	}
}

// turnReplies matches the live replies of the latest turn, the one msg starts in chatID: what its
// runs recorded, or, for a turn with no run (imported history), the assistant messages after it
// except a scheduled run's. No later user message bounds the positional match: a live one would
// make msg not the latest, and a superseded one does not end msg's turn.
func turnReplies(ctx context.Context, tx *ent.Tx, userID, chatID uuid.UUID, msg *ent.ChatMessage) ([]predicate.ChatMessage, error) {
	hasRuns, err := tx.Job.Query().Where(turnRuns(userID, msg.ID)...).Exist(ctx)
	if err != nil {
		return nil, err
	}
	where := []predicate.ChatMessage{
		chatmessage.HasChatWith(entchat.ID(chatID)),
		chatmessage.OriginEQ(chatmessage.OriginAssistant),
		chatmessage.SupersededAtIsNil(),
	}
	if hasRuns {
		return append(where, turnRunReply(userID, msg.ID)), nil
	}
	return append(where, chatmessage.JobRunIsNil(), sentAfter(msg.SentAt, msg.ID)), nil
}

// CheckTurnRevisable reports whether the turn started by userMessageID may be superseded or
// regenerated, without changing anything: ErrChatMessageNotFound when it is not the user's,
// ErrTurnNotUserMessage for a reply, ErrTurnNotLatest when a later live user message exists. A
// turn already taken back is reported (Superseded), not refused. SupersedeTurn and RegenerateTurn
// check again under their lock; this is for a caller that needs the chat first.
func (d *Datastore) CheckTurnRevisable(ctx context.Context, userID, userMessageID uuid.UUID) (RevisableTurn, error) {
	msg, chatID, err := loadTurnUserMessage(ctx, d.dbClient.ChatMessage, userID, userMessageID, false)
	if err != nil {
		return RevisableTurn{}, err
	}
	out := RevisableTurn{
		ChatID:             chatID,
		Superseded:         msg.SupersededAt != nil,
		ExcludedFromMemory: msg.ExcludeFromMemory != nil && *msg.ExcludeFromMemory,
	}
	if out.Superseded {
		return out, nil
	}
	if err := checkLatestTurn(ctx, d.dbClient.ChatMessage, chatID, msg); err != nil {
		return RevisableTurn{}, err
	}
	return out, nil
}

// loadTurnUserMessage loads the user message that names a turn, with its chat's id; lock locks
// its row for the rest of the transaction c belongs to (see the note above).
func loadTurnUserMessage(ctx context.Context, c *ent.ChatMessageClient, userID, userMessageID uuid.UUID, lock bool) (*ent.ChatMessage, uuid.UUID, error) {
	q := c.Query().
		Where(chatmessage.ID(userMessageID), chatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID)))).
		WithChat(func(q *ent.ChatQuery) { q.Select(entchat.FieldID) })
	if lock {
		q.Modify(lockRowForUpdate)
	}
	msg, err := q.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, uuid.Nil, ErrChatMessageNotFound
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	if msg.Origin != chatmessage.OriginUser {
		return nil, uuid.Nil, ErrTurnNotUserMessage
	}
	if msg.Edges.Chat == nil {
		return nil, uuid.Nil, ErrChatMessageNotFound
	}
	return msg, msg.Edges.Chat.ID, nil
}

// checkLatestTurn returns ErrTurnNotLatest when chatID has a live user message after msg.
func checkLatestTurn(ctx context.Context, c *ent.ChatMessageClient, chatID uuid.UUID, msg *ent.ChatMessage) error {
	later, err := c.Query().
		Where(
			chatmessage.HasChatWith(entchat.ID(chatID)),
			chatmessage.OriginEQ(chatmessage.OriginUser),
			chatmessage.SupersededAtIsNil(),
			sentAfter(msg.SentAt, msg.ID),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if later {
		return ErrTurnNotLatest
	}
	return nil
}

// inTx runs fn in a transaction, committing when it returns nil and rolling back otherwise.
func (d *Datastore) inTx(ctx context.Context, fn func(tx *ent.Tx) error) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()
	if err := fn(tx); err != nil {
		d.rollbackTx(tx)
		return err
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}
	return nil
}

// supersedeTurnRepliesTx marks the live replies of the latest turn msg starts superseded at now,
// and clears the chat's stored Responses id (chat.response_id), which named the superseded reply's
// response, so nothing can continue a response chain from it.
func supersedeTurnRepliesTx(ctx context.Context, tx *ent.Tx, userID, chatID uuid.UUID, msg *ent.ChatMessage, now time.Time) error {
	replies, err := turnReplies(ctx, tx, userID, chatID, msg)
	if err != nil {
		return err
	}
	if _, err := tx.ChatMessage.Update().Where(replies...).SetSupersededAt(now).Save(ctx); err != nil {
		return err
	}
	return tx.Chat.UpdateOneID(chatID).SetResponseID("").Exec(ctx)
}

// SupersedeTurn takes back the latest turn in a chat, the one userMessageID starts: the user
// message and its replies are marked superseded at now. It returns the ids of the turn's runs that
// have not finished (any non-terminal status, a run whose worker died included), for the caller to
// stop (agent.SupersedeTurn). Only this turn's: a scheduled run in the chat, or an earlier turn
// still answering, is left alone. A reply one of them saves while it winds down is superseded as
// it is saved (SettleTurnReply).
//
// Superseding a turn already taken back changes nothing and succeeds, and still returns its
// unfinished runs (a stop that failed the first time is tried again). Errors:
// ErrChatMessageNotFound, ErrTurnNotUserMessage, ErrTurnNotLatest.
func (d *Datastore) SupersedeTurn(ctx context.Context, userID, userMessageID uuid.UUID, now time.Time) ([]uuid.UUID, error) {
	var running []uuid.UUID
	err := d.inTx(ctx, func(tx *ent.Tx) error {
		msg, chatID, err := loadTurnUserMessage(ctx, tx.ChatMessage, userID, userMessageID, true)
		if err != nil {
			return err
		}
		if msg.SupersededAt == nil {
			if err := checkLatestTurn(ctx, tx.ChatMessage, chatID, msg); err != nil {
				return err
			}
			if err := supersedeTurnRepliesTx(ctx, tx, userID, chatID, msg, now); err != nil {
				return err
			}
			if err := tx.ChatMessage.UpdateOneID(msg.ID).SetSupersededAt(now).Exec(ctx); err != nil {
				return err
			}
		}
		running, err = tx.Job.Query().
			Where(append(turnRuns(userID, msg.ID), job.StatusIn(nonTerminalJobStatuses...))...).
			IDs(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return running, nil
}

// RegenerateTurn supersedes the replies of the latest turn, the one userMessageID starts, and
// creates the pending chat_message job that answers the user message again, in one transaction
// under the user message's row lock: it re-checks there that no run of the turn is unfinished
// (ErrTurnRunning), so two regenerates of a turn cannot both create a run. The caller starts the
// returned job. excludeFromMemory flags the user message chat_message.exclude_from_memory (the
// new run has memory saving off); the flag is never cleared. The user message's last error is
// cleared, as a retry clears it.
//
// Errors: ErrChatMessageNotFound, ErrTurnNotUserMessage, ErrTurnNotLatest, ErrTurnSuperseded,
// ErrTurnRunning.
func (d *Datastore) RegenerateTurn(ctx context.Context, userID, userMessageID uuid.UUID, excludeFromMemory bool, now time.Time) (*models.Job, error) {
	var created *ent.Job
	err := d.inTx(ctx, func(tx *ent.Tx) error {
		msg, chatID, err := loadTurnUserMessage(ctx, tx.ChatMessage, userID, userMessageID, true)
		if err != nil {
			return err
		}
		if msg.SupersededAt != nil {
			return ErrTurnSuperseded
		}
		if err := checkLatestTurn(ctx, tx.ChatMessage, chatID, msg); err != nil {
			return err
		}
		running, err := tx.Job.Query().Where(append(turnRuns(userID, msg.ID), unfinishedTurnRun(now))...).Exist(ctx)
		if err != nil {
			return err
		}
		if running {
			return ErrTurnRunning
		}
		if err := supersedeTurnRepliesTx(ctx, tx, userID, chatID, msg, now); err != nil {
			return err
		}
		update := tx.ChatMessage.UpdateOneID(msg.ID).ClearLastErrorMessage().ClearLastErrorCode()
		if excludeFromMemory {
			update.SetExcludeFromMemory(true)
		}
		if err := update.Exec(ctx); err != nil {
			return err
		}
		j, err := tx.Job.Create().
			SetJobType("chat_message").
			SetReference(msg.ID.String()).
			SetStatus(job.StatusPending).
			SetOwnerID(userID).
			SetDraftDeltas([]string{}).
			Save(ctx)
		if err != nil {
			return err
		}
		created, err = tx.Job.Query().Where(job.ID(j.ID)).WithOwner().Only(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	if d.metrics != nil {
		d.metrics.Add(ctx, telemetry.JobsEnqueued, 1, telemetry.AttrJobType.String(created.JobType))
	}
	return toJobModel(created), nil
}

// SettleTurnReply settles a reply the run runID just saved for the turn userMessageID starts in chatID (see
// the note above), in its own transaction under the user message's row lock:
//
//   - the turn was taken back while the run ran: the reply is superseded (a late reply is never
//     live);
//   - runID is no longer the turn's newest run (a regenerate started since): the reply is
//     superseded too, as replaced by the newest run's reply when that is saved already;
//   - otherwise the reply is recorded on its run (result_id, when the job has none yet) and, when the
//     turn was regenerated, as superseded_by on the replies the regenerate replaced.
//
// Every path that saves a turn's reply settles it: the agent after saving, and
// finalizeChatJobWithPartial for a partial one. runID is the job that saved the reply; uuid.Nil, or a
// job that is not one of the turn's runs, settles on the user message alone. A user message that is
// not one (a scheduled run's reference is its chat) is a no-op.
func (d *Datastore) SettleTurnReply(ctx context.Context, userID, chatID, userMessageID, runID, replyID uuid.UUID) error {
	if chatID == uuid.Nil || userMessageID == uuid.Nil || replyID == uuid.Nil {
		return nil
	}
	return d.inTx(ctx, func(tx *ent.Tx) error {
		return settleTurnReplyTx(ctx, tx, userID, chatID, userMessageID, runID, replyID, time.Now())
	})
}

// lockTurnUserMessageTx locks the row of the user message userMessageID in chatID (of userID) for
// the rest of tx and returns it, or nil when there is no such user message (the reference of a
// scheduled run is its chat).
func lockTurnUserMessageTx(ctx context.Context, tx *ent.Tx, userID, chatID, userMessageID uuid.UUID) (*ent.ChatMessage, error) {
	q := tx.ChatMessage.Query().
		Where(
			chatmessage.ID(userMessageID),
			chatmessage.OriginEQ(chatmessage.OriginUser),
			chatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))),
		)
	q.Modify(lockRowForUpdate)
	msg, err := q.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return msg, err
}

func settleTurnReplyTx(ctx context.Context, tx *ent.Tx, userID, chatID, userMessageID, runID, replyID uuid.UUID, now time.Time) error {
	if chatID == uuid.Nil || userMessageID == uuid.Nil || replyID == uuid.Nil {
		return nil
	}
	msg, err := lockTurnUserMessageTx(ctx, tx, userID, chatID, userMessageID)
	if err != nil || msg == nil {
		return err
	}
	supersede := msg.SupersededAt != nil
	var replacedBy *uuid.UUID
	if runID != uuid.Nil {
		runs, err := tx.Job.Query().Where(turnRuns(userID, userMessageID)...).All(ctx)
		if err != nil {
			return err
		}
		if newest := newestTurnRun(runs); newest != nil && slices.ContainsFunc(runs, func(r *ent.Job) bool { return r.ID == runID }) {
			// One of the turn's runs (otherwise settled on the user message alone).
			if err := tx.Job.Update().Where(job.ID(runID), job.ResultIDIsNil()).SetResultID(replyID).Exec(ctx); err != nil {
				return err
			}
			if newest.ID != runID {
				supersede = true
				replacedBy = newest.ResultID
			}
		}
	}
	if supersede {
		update := tx.ChatMessage.Update().
			Where(chatmessage.ID(replyID), chatmessage.SupersededAtIsNil()).
			SetSupersededAt(now)
		if replacedBy != nil && msg.SupersededAt == nil {
			update.SetSupersededBy(*replacedBy)
		}
		return update.Exec(ctx)
	}
	// The replies a regenerate superseded, not yet linked to what replaced them: the replies the
	// turn's earlier runs recorded. On an ordinary turn there are none and this updates nothing.
	return tx.ChatMessage.Update().
		Where(
			chatmessage.HasChatWith(entchat.ID(chatID)),
			chatmessage.IDNEQ(replyID),
			chatmessage.SupersededAtNotNil(),
			chatmessage.SupersededByIsNil(),
			turnRunReply(userID, userMessageID),
		).
		SetSupersededBy(replyID).
		Exec(ctx)
}

// LatestChatMessageJobID returns the id of the newest chat_message job (any status) for the user
// message userMessageID, or uuid.Nil when there is none: the turn's current run, after any retry
// or regenerate.
func (d *Datastore) LatestChatMessageJobID(ctx context.Context, userID, userMessageID uuid.UUID) (uuid.UUID, error) {
	id, err := d.dbClient.Job.Query().
		Where(
			job.HasOwnerWith(user.ID(userID)),
			job.JobTypeEQ("chat_message"),
			job.ReferenceEQ(userMessageID.String()),
		).
		Order(ent.Desc(job.FieldCreatedAt), ent.Desc(job.FieldID)).
		FirstID(ctx)
	if ent.IsNotFound(err) {
		return uuid.Nil, nil
	}
	return id, err
}

// MarkChatMessageExcludedFromMemory flags a user's message chat_message.exclude_from_memory. The
// flag is only ever set: it is never cleared.
func (d *Datastore) MarkChatMessageExcludedFromMemory(ctx context.Context, userID, messageID uuid.UUID) error {
	n, err := d.dbClient.ChatMessage.Update().
		Where(chatmessage.ID(messageID), chatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID)))).
		SetExcludeFromMemory(true).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}
