package datastore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// ErrChatRestoreConflict means a restore lost a race or a summary/checkpoint is still running.
var ErrChatRestoreConflict = errors.New("thread is no longer archived or is still being prepared; refresh and try again")

// RestoreChat atomically unarchives an owned thread with its optional context preferences.
// Only this operation writes guidance; ordinary turn bookkeeping cannot overwrite it.
func (d *Datastore) RestoreChat(ctx context.Context, userID, chatID uuid.UUID, guidance string) (*models.Chat, error) {
	guidance, err := modeltypes.NormalizeRestoreGuidance(guidance)
	if err != nil {
		return nil, err
	}
	update := d.dbClient.Chat.Update().Where(
		entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID)), entchat.Archived(true),
		entchat.Or(entchat.RehydrationStateIsNil(), entchat.RehydrationStateNotIn(models.RehydrationStatePending, models.RehydrationStateProcessing)),
		// A dead worker must not prevent restore forever (the checkpoint lease expires after 10m).
		entchat.Or(entchat.CheckpointStartedAtIsNil(), entchat.CheckpointStartedAtLT(time.Now().Add(-10*time.Minute))),
	).SetArchived(false).SetRestoreGuidance(guidance)
	if guidance != "" {
		update.SetRehydrationState(models.RehydrationStateStale).ClearResponseID()
	}
	count, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrChatRestoreConflict
	}
	return d.GetChat(ctx, userID, chatID)
}
