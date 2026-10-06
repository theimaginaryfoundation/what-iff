package datastore

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// GetChatMessagesForSummary loads all messages for a chat in chronological order with only the
// fields needed for summarization (message text, origin, sent_at). It deliberately skips the heavy
// edge loading done by ListChatMessages because rehydration summarization only needs the transcript.
func (d *Datastore) GetChatMessagesForSummary(ctx context.Context, userID, chatID uuid.UUID) ([]models.ChatMessage, error) {
	owns, err := d.dbClient.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if !owns {
		return nil, ErrChatNotFound
	}

	rows, err := d.dbClient.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(chatID))).
		Order(chatmessage.BySentAt()).
		Select(chatmessage.FieldMessage, chatmessage.FieldOrigin, chatmessage.FieldSentAt).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		return nil, err
	}

	msgs := make([]models.ChatMessage, 0, len(rows))
	for _, r := range rows {
		msgs = append(msgs, models.ChatMessage{
			Message: r.Message,
			Origin:  models.MessageOrigin(r.Origin),
			SentAt:  r.SentAt,
		})
	}
	return msgs, nil
}

// GetChatRehydrationState returns just the rehydration lifecycle state for a chat (cheap; no edges).
// Returns ErrChatNotFound when the chat does not exist or is not owned by the user.
func (d *Datastore) GetChatRehydrationState(ctx context.Context, userID, chatID uuid.UUID) (string, error) {
	state, err := d.dbClient.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Select(entchat.FieldRehydrationState).
		String(ctx)
	if ent.IsNotFound(err) {
		return "", ErrChatNotFound
	}
	if err != nil {
		return "", err
	}
	return state, nil
}

// SetChatRehydrationState writes the lazy-summarization lifecycle state for a chat
// ("" / "pending" / "processing" / "ready" / "failed"). Ownership is enforced in the WHERE clause.
func (d *Datastore) SetChatRehydrationState(ctx context.Context, userID, chatID uuid.UUID, state string) error {
	updated, err := d.dbClient.Chat.Update().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		SetRehydrationState(state).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to set chat rehydration state", zap.String("chat_id", chatID.String()), zap.Error(err))
		return err
	}
	if updated == 0 {
		return ErrChatNotFound
	}
	return nil
}

// ChatRehydrationInfo is what the turn gate reads about a chat in one query.
type ChatRehydrationInfo struct {
	// State is the rehydration lifecycle state ("" / pending / processing / ready / failed).
	State string
	// Imported is true for a chat created by a conversation import (source set).
	Imported bool
	// Summarized is true once the chat has a checkpoint summary.
	Summarized bool
}

// NeedsRehydration reports whether the chat is an imported one that still lacks its one-time
// summary and is not already being summarized or done ("failed" qualifies, so it is retried).
func (i ChatRehydrationInfo) NeedsRehydration() bool {
	if !i.Imported || i.Summarized {
		return false
	}
	return i.State == models.RehydrationStateNone || i.State == models.RehydrationStateFailed
}

// GetChatRehydrationInfo returns the rehydration state of a chat with whether it was imported and
// already has a summary (cheap; no edges). Returns ErrChatNotFound when the chat does not exist or
// is not owned by the user.
func (d *Datastore) GetChatRehydrationInfo(ctx context.Context, userID, chatID uuid.UUID) (ChatRehydrationInfo, error) {
	row, err := d.dbClient.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Select(entchat.FieldRehydrationState, entchat.FieldSource, entchat.FieldCheckpointSummary).
		Only(ctx)
	if ent.IsNotFound(err) {
		return ChatRehydrationInfo{}, ErrChatNotFound
	}
	if err != nil {
		return ChatRehydrationInfo{}, err
	}
	return ChatRehydrationInfo{
		State:      row.RehydrationState,
		Imported:   row.Source != "",
		Summarized: row.CheckpointSummary != "",
	}, nil
}

// ClaimChatRehydration atomically moves an imported thread that still needs its one-time summary
// to "pending" and reports whether this caller won the claim. A thread qualifies when it was
// imported (source set), has no checkpoint summary and is not already pending, processing or ready
// ("failed" qualifies, so a later attempt retries); unless includeArchived is set it must also be
// unarchived. The conditional update makes concurrent attempts (two tabs, a refetch on focus, a
// turn racing an open) start the summary job exactly once.
// Ownership is enforced in the WHERE clause; a chat that does not qualify is not an error.
func (d *Datastore) ClaimChatRehydration(ctx context.Context, userID, chatID uuid.UUID, includeArchived bool) (bool, error) {
	update := d.dbClient.Chat.Update().
		Where(
			entchat.ID(chatID),
			entchat.HasOwnerWith(user.ID(userID)),
			entchat.SourceNotNil(),
			entchat.SourceNEQ(""),
			entchat.Or(entchat.CheckpointSummaryIsNil(), entchat.CheckpointSummary("")),
			entchat.Or(
				entchat.RehydrationStateIsNil(),
				entchat.RehydrationStateIn(models.RehydrationStateNone, models.RehydrationStateFailed),
			),
		)
	if !includeArchived {
		update = update.Where(entchat.Archived(false))
	}
	claimed, err := update.SetRehydrationState(models.RehydrationStatePending).Save(ctx)
	if err != nil {
		d.logger.Error("failed to claim chat rehydration", zap.String("chat_id", chatID.String()), zap.Error(err))
		return false, err
	}
	return claimed > 0, nil
}

// SetImportedThreadRehydrated persists the result of a successful rehydration summary in one update:
// the checkpoint summary, the user-message count at the checkpoint (drives future checkpoint cadence),
// the window pointer (last_checkpoint_at — history loads only messages with sent_at >= this), and
// flips rehydration_state to "ready". Unlike UpdateChatCheckpointStateAndClearResponseID, the window
// pointer is an explicit message timestamp (n-5 turns back) rather than time.Now().
func (d *Datastore) SetImportedThreadRehydrated(ctx context.Context, userID, chatID uuid.UUID, summary string, checkpointUserMessageCount int, windowStart time.Time) error {
	updated, err := d.dbClient.Chat.Update().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		SetCheckpointSummary(summary).
		SetCheckpointUserMessageCount(checkpointUserMessageCount).
		SetLastCheckpointAt(windowStart).
		SetRehydrationState(models.RehydrationStateReady).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to persist imported thread rehydration", zap.String("chat_id", chatID.String()), zap.Error(err))
		return err
	}
	if updated == 0 {
		return ErrChatNotFound
	}
	return nil
}

// GetChatPersonalityID returns the id of the personality assigned to a chat, or uuid.Nil when none
// is assigned (cheap; no edges beyond the lookup). Imported-thread memory seeding reads it at
// rehydration time to apply the personality's auto-pin rule. A chat that does not exist or is not
// owned by the user also yields uuid.Nil: callers go on to write through ownership-checked methods.
func (d *Datastore) GetChatPersonalityID(ctx context.Context, userID, chatID uuid.UUID) (uuid.UUID, error) {
	id, err := d.dbClient.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		QueryPersonality().
		OnlyID(ctx)
	if ent.IsNotFound(err) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
