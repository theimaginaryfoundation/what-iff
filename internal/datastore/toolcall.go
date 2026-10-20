package datastore

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/toolcall"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// toToolCallModel converts from Ent ToolCall to model
func toToolCallModel(e *ent.ToolCall) *models.ToolCall {
	if e == nil {
		return nil
	}

	chatMessageID := uuid.Nil
	if e.Edges.ChatMessage != nil {
		chatMessageID = e.Edges.ChatMessage.ID
	}

	return &models.ToolCall{
		ID:            e.ID,
		ChatMessageID: chatMessageID,
		ToolName:      e.ToolName,
		ToolInput:     e.ToolInput,
		ToolOutput:    e.ToolOutput,
		ToolError:     e.ToolError,
		Step:          e.Step,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
}

// createToolCallsBulk creates multiple tool calls in bulk within the provided transaction
func (d *Datastore) createToolCallsBulk(ctx context.Context, tx *ent.Tx, chatMessageID uuid.UUID, toolCalls []*models.ToolCall) error {
	if len(toolCalls) == 0 {
		return nil
	}

	toolCallCreates := make([]*ent.ToolCallCreate, len(toolCalls))
	for i, toolCall := range toolCalls {
		create := tx.ToolCall.Create().
			SetToolName(toolCall.ToolName).
			SetChatMessageID(chatMessageID)

		if toolCall.ToolInput != "" {
			create.SetToolInput(toolCall.ToolInput)
		}
		if toolCall.ToolOutput != "" {
			create.SetToolOutput(toolCall.ToolOutput)
		}
		if toolCall.ToolError != "" {
			create.SetToolError(toolCall.ToolError)
		}
		if toolCall.Step > 0 {
			create.SetStep(toolCall.Step)
		}

		toolCallCreates[i] = create
	}

	_, err := tx.ToolCall.CreateBulk(toolCallCreates...).Save(ctx)
	return err
}

// ChatToolNamesUsedSince returns the distinct names of the tools called by the chat's messages
// sent at or after since (every message when since is nil). A call that failed still counts.
func (d *Datastore) ChatToolNamesUsedSince(ctx context.Context, userID, chatID uuid.UUID, since *time.Time) (map[string]struct{}, error) {
	msgPreds := []predicate.ChatMessage{chatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID)))}
	if since != nil {
		msgPreds = append(msgPreds, chatmessage.SentAtGTE(*since))
	}
	names, err := d.dbClient.ToolCall.Query().
		Where(toolcall.HasChatMessageWith(msgPreds...)).
		Unique(true).
		Select(toolcall.FieldToolName).
		Strings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out, nil
}
