package datastore

import (
	"context"
	"errors"
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
)

// Taking a turn back (superseding it). A turn is a user message and the reply (or replies) that
// answer it. Superseding marks them superseded_at: they are kept as past versions, but left out of
// everything that lists a chat's messages (the model's history and carry-over, the message routes,
// counts, exports; listChatMessages and the rest filter on superseded_at IS NULL). Regenerating a
// turn supersedes only its replies and runs the same user message again; the new reply is recorded
// on the old ones as superseded_by once it is saved (SettleTurnReply).
//
// Only the latest turn in a chat can be taken back or regenerated: superseding a turn in the
// middle of a conversation would leave the turns after it answering something the model can no
// longer see. "Latest" is the newest live (not superseded) user message, by (sent_at, id), and its
// replies are the assistant messages after it, except a scheduled run's (job_run set), which
// answers its own prompt rather than the user's message.
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

// turnReplies matches the replies of the latest turn, the one msg starts in chatID: the assistant
// messages after it, leaving out a scheduled run's (see the note above). No later user message
// bounds them: a live one would make msg not the latest, and a superseded one does not end msg's
// turn (a regenerated reply can land after a turn taken back in between, and is still msg's).
// Replies of a later superseded turn match too; they are superseded already.
func turnReplies(chatID uuid.UUID, msg *ent.ChatMessage) []predicate.ChatMessage {
	return []predicate.ChatMessage{
		chatmessage.HasChatWith(entchat.ID(chatID)),
		chatmessage.OriginEQ(chatmessage.OriginAssistant),
		chatmessage.JobRunIsNil(),
		sentAfter(msg.SentAt, msg.ID),
	}
}

// CheckTurnRevisable reports whether the turn started by userMessageID may be superseded or
// regenerated, without changing anything: ErrChatMessageNotFound when it is not the user's,
// ErrTurnNotUserMessage for a reply, ErrTurnNotLatest when a later live user message exists. A
// turn already taken back is reported (Superseded), not refused.
func (d *Datastore) CheckTurnRevisable(ctx context.Context, userID, userMessageID uuid.UUID) (RevisableTurn, error) {
	msg, chatID, err := d.loadTurnUserMessage(ctx, d.dbClient.ChatMessage, userID, userMessageID)
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

// loadTurnUserMessage loads the user message that names a turn, with its chat's id.
func (d *Datastore) loadTurnUserMessage(ctx context.Context, c *ent.ChatMessageClient, userID, userMessageID uuid.UUID) (*ent.ChatMessage, uuid.UUID, error) {
	msg, err := c.Query().
		Where(chatmessage.ID(userMessageID), chatmessage.HasChatWith(entchat.HasOwnerWith(user.ID(userID)))).
		WithChat(func(q *ent.ChatQuery) { q.Select(entchat.FieldID) }).
		Only(ctx)
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

// SupersedeTurn takes back the latest turn in a chat, the one userMessageID starts: its replies
// and, unless repliesOnly, the user message itself are marked superseded at now. repliesOnly is
// regenerating: the user message stays live, to be answered again. It also clears the chat's stored
// Responses id (chat.response_id), which named the superseded reply's response, so nothing can
// continue a response chain from it.
//
// Superseding a turn already taken back is a no-op that succeeds; regenerating one is
// ErrTurnSuperseded. The caller stops a running turn first
// (agent.SupersedeTurn): a reply its worker saves afterwards is superseded when it is saved
// (SettleTurnReply), and a second sweep after the commit covers a reply saved while this ran.
func (d *Datastore) SupersedeTurn(ctx context.Context, userID, userMessageID uuid.UUID, repliesOnly bool, now time.Time) error {
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

	msg, chatID, err := d.loadTurnUserMessage(ctx, tx.ChatMessage, userID, userMessageID)
	if err != nil {
		d.rollbackTx(tx)
		return err
	}
	if msg.SupersededAt != nil {
		d.rollbackTx(tx)
		if repliesOnly {
			return ErrTurnSuperseded
		}
		// Already taken back: nothing to do. A reply its run saved since was superseded when it
		// was saved (SettleTurnReply).
		return nil
	}
	if err := checkLatestTurn(ctx, tx.ChatMessage, chatID, msg); err != nil {
		d.rollbackTx(tx)
		return err
	}
	replies := turnReplies(chatID, msg)
	if _, err := tx.ChatMessage.Update().
		Where(append(replies, chatmessage.SupersededAtIsNil())...).
		SetSupersededAt(now).
		Save(ctx); err != nil {
		d.rollbackTx(tx)
		return err
	}
	if !repliesOnly {
		if err := tx.ChatMessage.UpdateOneID(msg.ID).SetSupersededAt(now).Exec(ctx); err != nil {
			d.rollbackTx(tx)
			return err
		}
	}
	if err := tx.Chat.UpdateOneID(chatID).SetResponseID("").Exec(ctx); err != nil {
		d.rollbackTx(tx)
		return err
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	if repliesOnly {
		// The caller checked that nothing is running; no reply can arrive late.
		return nil
	}
	// A reply committed while the transaction ran, after its update read the rows, and checked
	// before the user message's mark committed (SettleTurnReply) would otherwise stay live. Such a
	// reply was created before the commit, which bounds the sweep: a reply of a turn started since
	// is never touched.
	committedAt := time.Now()
	if _, err := d.dbClient.ChatMessage.Update().
		Where(append(replies, chatmessage.SupersededAtIsNil(), chatmessage.SentAtLTE(committedAt))...).
		SetSupersededAt(now).
		Save(ctx); err != nil {
		d.logger.Warn("supersede: the second sweep for late replies failed",
			zap.String("chat_message_id", userMessageID.String()), zap.Error(err))
	}
	return nil
}

// SettleTurnReply records a reply just saved for the turn userMessageID starts in chatID: when the
// turn was taken back while it ran, the reply is superseded too (a late reply is never live); when
// the turn was regenerated, it is recorded as superseded_by on the replies it replaced. Every path
// that saves a turn's reply calls it (the agent after saving, finalizeChatJobWithPartial for a
// partial one). A user message that is not one (a scheduled run's reference is its chat) is a no-op.
func (d *Datastore) SettleTurnReply(ctx context.Context, userID, chatID, userMessageID, replyID uuid.UUID) error {
	return settleTurnReply(ctx, d.dbClient.ChatMessage, userID, chatID, userMessageID, replyID, time.Now())
}

func settleTurnReply(ctx context.Context, c *ent.ChatMessageClient, userID, chatID, userMessageID, replyID uuid.UUID, now time.Time) error {
	if chatID == uuid.Nil || userMessageID == uuid.Nil || replyID == uuid.Nil {
		return nil
	}
	msg, err := c.Query().
		Where(
			chatmessage.ID(userMessageID),
			chatmessage.OriginEQ(chatmessage.OriginUser),
			chatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))),
		).
		Select(chatmessage.FieldID, chatmessage.FieldSentAt, chatmessage.FieldSupersededAt).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if msg.SupersededAt != nil {
		return c.Update().
			Where(chatmessage.ID(replyID), chatmessage.SupersededAtIsNil()).
			SetSupersededAt(now).
			Exec(ctx)
	}
	// The replies a regenerate superseded, not yet linked to what replaced them: the replies the
	// turn's earlier runs saved (their jobs' result ids). Found through the jobs rather than by
	// position, which cannot tell them from the replies of a later turn that was taken back. On an
	// ordinary turn there are none and this updates nothing.
	return c.Update().
		Where(
			chatmessage.HasChatWith(entchat.ID(chatID)),
			chatmessage.IDNEQ(replyID),
			chatmessage.SupersededAtNotNil(),
			chatmessage.SupersededByIsNil(),
			func(s *sql.Selector) {
				runs := sql.Table(job.Table)
				s.Where(sql.In(s.C(chatmessage.FieldID),
					sql.Select(runs.C(job.FieldResultID)).From(runs).Where(sql.And(
						sql.EQ(runs.C(job.FieldJobType), "chat_message"),
						sql.EQ(runs.C(job.FieldReference), userMessageID.String()),
						sql.NotNull(runs.C(job.FieldResultID)),
					)),
				))
			},
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

// MarkChatMessageExcludedFromMemory flags a user's message chat_message.exclude_from_memory (a
// regenerate run with memory saving off). The flag is only ever set: it is never cleared.
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
