package datastore

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/toolcall"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ErrToolCallNotFound: no tool call with that id on a message of the account's chat.
var ErrToolCallNotFound = errors.New("tool call not found")

// GetChatToolCall returns a saved tool call of a message in the account's chat (an MCP App's
// view is loaded through it, ADR 0x042).
func (d *Datastore) GetChatToolCall(ctx context.Context, userID, chatID, id uuid.UUID) (*models.ToolCall, error) {
	row, err := d.dbClient.ToolCall.Query().
		Where(
			toolcall.ID(id),
			toolcall.HasChatMessageWith(chatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID)))),
		).
		WithChatMessage().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrToolCallNotFound
	}
	if err != nil {
		return nil, err
	}
	return toToolCallModel(row), nil
}
