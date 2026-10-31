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

// createToolCallsBulk saves a message's tool calls within tx. A call an Advanced run already
// saved as a job draft (JobDraft) is moved onto the message; the rest are created, keeping their
// id when the caller assigned one (an Advanced run's events refer to it).
func (d *Datastore) createToolCallsBulk(ctx context.Context, tx *ent.Tx, chatMessageID uuid.UUID, toolCalls []*models.ToolCall) error {
	var creates []*ent.ToolCallCreate
	for _, toolCall := range toolCalls {
		if toolCall.JobDraft && toolCall.ID != uuid.Nil {
			n, err := tx.ToolCall.Update().
				Where(toolcall.ID(toolCall.ID), toolcall.Not(toolcall.HasChatMessage())).
				SetChatMessageID(chatMessageID).
				ClearDraftJob().
				Save(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			// The draft row is missing (its write failed): create it below with the same id.
		}
		create := tx.ToolCall.Create().
			SetToolName(toolCall.ToolName).
			SetChatMessageID(chatMessageID)
		if toolCall.ID != uuid.Nil {
			create.SetID(toolCall.ID)
		}
		setToolCallFields(create, toolCall)
		creates = append(creates, create)
	}
	if len(creates) == 0 {
		return nil
	}
	_, err := tx.ToolCall.CreateBulk(creates...).Save(ctx)
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

// ChatToolInputsSince returns the recorded inputs of every call to toolName by the chat's
// messages sent at or after since (every message when since is nil), oldest first. The checkpoint
// skill step reads load_skill calls this way to learn which skills the segment loaded.
func (d *Datastore) ChatToolInputsSince(ctx context.Context, userID, chatID uuid.UUID, since *time.Time, toolName string) ([]string, error) {
	msgPreds := []predicate.ChatMessage{chatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID)))}
	if since != nil {
		msgPreds = append(msgPreds, chatmessage.SentAtGTE(*since))
	}
	rows, err := d.dbClient.ToolCall.Query().
		Where(toolcall.ToolName(toolName), toolcall.HasChatMessageWith(msgPreds...)).
		Order(ent.Asc(toolcall.FieldCreatedAt)).
		Select(toolcall.FieldToolInput).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ToolInput)
	}
	return out, nil
}
