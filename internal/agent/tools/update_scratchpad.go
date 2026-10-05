package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// updateScratchpadToolArgs represents the arguments for the update_scratchpad function
type updateScratchpadToolArgs struct {
	Content string `json:"content"`
}

// updateScratchpadToolResult represents the result of updating the scratchpad
type updateScratchpadToolResult struct {
	PersonalityID string `json:"personality_id"`
	Success       bool   `json:"success"`
	ContentLength int    `json:"content_length"`
	Error         string `json:"error,omitempty"`
}

// updateScratchpadTool is the implementation of the update_scratchpad function
func (t *ScratchpadTool) UpdateScratchpadTool(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	var updateArgs updateScratchpadToolArgs
	if err := json.Unmarshal(args, &updateArgs); err != nil {
		result := updateScratchpadToolResult{
			PersonalityID: "unknown",
			Success:       false,
			Error:         fmt.Sprintf("invalid arguments: %v", err),
		}
		return marshalToolResult(result, "update_scratchpad")
	}

	// Validate content is not empty (including whitespace-only)
	trimmedContent, valid := validateNonEmptyString(updateArgs.Content)
	if !valid {
		result := updateScratchpadToolResult{
			PersonalityID: chat.PersonalityID.String(),
			Success:       false,
			ContentLength: 0,
			Error:         "scratchpad content cannot be empty",
		}
		return marshalToolResult(result, "update_scratchpad")
	}

	// Check if personality ID is nil/zero UUID
	if chat.PersonalityID == uuid.Nil {
		result := updateScratchpadToolResult{
			PersonalityID: "none",
			Success:       false,
			Error:         "scratchpad is not available for this personality",
		}
		return marshalToolResult(result, "update_scratchpad")
	}

	// Update the personality scratchpad in the datastore
	personalityModel := models.Personality{
		ID:         chat.PersonalityID,
		Scratchpad: trimmedContent,
	}

	updatedPersonality, err := t.datastore.UpdatePersonalityScratchpad(ctx, chat.UserID, personalityModel)
	if err != nil {
		t.logger.Error("failed to update personality scratchpad",
			zap.String("personality_id", chat.PersonalityID.String()),
			zap.String("content_preview", truncateForLog(trimmedContent, 64)),
			zap.Int("content_length", len(trimmedContent)),
			zap.Error(err))
		result := updateScratchpadToolResult{
			PersonalityID: chat.PersonalityID.String(),
			Success:       false,
			Error:         fmt.Sprintf("failed to update scratchpad: %v", err),
		}
		return marshalToolResult(result, "update_scratchpad")
	}

	result := updateScratchpadToolResult{
		PersonalityID: chat.PersonalityID.String(),
		Success:       true,
		ContentLength: len(updatedPersonality.Scratchpad),
	}

	return marshalToolResult(result, "update_scratchpad")
}
