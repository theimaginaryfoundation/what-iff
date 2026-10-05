package datastore

import (
	"context"
	"time"

	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/user"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ClaimChatCheckpoint marks a checkpoint (the scratchpad, memory and summary pass) as running for
// the chat and reports whether this caller won the claim, so only one runs per chat at a time even
// across API instances. It fails to claim while another claim is younger than staleAfter; an older
// one is taken over, which is how a checkpoint whose worker died stops blocking the chat's later
// ones. The returned time identifies the claim for ReleaseChatCheckpoint. A chat that does not
// exist or is not owned by the user is simply not claimed.
func (d *Datastore) ClaimChatCheckpoint(ctx context.Context, userID, chatID uuid.UUID, staleAfter time.Duration) (claimedAt time.Time, claimed bool, err error) {
	// Microsecond precision so the stored value compares equal on release.
	claimedAt = time.Now().UTC().Truncate(time.Microsecond)
	n, err := d.dbClient.Chat.Update().
		Where(
			entchat.ID(chatID),
			entchat.HasOwnerWith(user.ID(userID)),
			entchat.Or(
				entchat.CheckpointStartedAtIsNil(),
				entchat.CheckpointStartedAtLT(claimedAt.Add(-staleAfter)),
			),
		).
		SetCheckpointStartedAt(claimedAt).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to claim chat checkpoint", zap.String("chat_id", chatID.String()), zap.Error(err))
		return time.Time{}, false, err
	}
	return claimedAt, n > 0, nil
}

// ReleaseChatCheckpoint clears the claim taken at claimedAt. A claim that was taken over since
// (it went stale) is left to its new owner.
func (d *Datastore) ReleaseChatCheckpoint(ctx context.Context, userID, chatID uuid.UUID, claimedAt time.Time) error {
	_, err := d.dbClient.Chat.Update().
		Where(
			entchat.ID(chatID),
			entchat.HasOwnerWith(user.ID(userID)),
			entchat.CheckpointStartedAt(claimedAt),
		).
		ClearCheckpointStartedAt().
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to release chat checkpoint", zap.String("chat_id", chatID.String()), zap.Error(err))
	}
	return err
}
