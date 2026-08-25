package datastore

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
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

		toolCallCreates[i] = create
	}

	_, err := tx.ToolCall.CreateBulk(toolCallCreates...).Save(ctx)
	return err
}
