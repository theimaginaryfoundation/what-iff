package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// createMemoryToolArgs represents the arguments for the create_memory function
type createMemoryToolArgs struct {
	Content string `json:"content"`
	Scope   string `json:"scope"`
}

// createMemoryToolResult represents the result of creating a memory
type createMemoryToolResult struct {
	Success  bool   `json:"success"`
	MemoryID string `json:"memory_id,omitempty"`
	Content  string `json:"content"`
	Scope    string `json:"scope"`
	Error    string `json:"error,omitempty"`
}

// createMemoryTool is the implementation of the create_memory function
func (t *VectorStoreMemoryTool) CreateMemoryTool(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	var memoryArgs createMemoryToolArgs
	if err := json.Unmarshal(args, &memoryArgs); err != nil {
		result := createMemoryToolResult{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		}
		return marshalToolResult(result, "create_memory")
	}

	// Validate content is not empty (including whitespace-only)
	trimmedContent, valid := validateNonEmptyString(memoryArgs.Content)
	if !valid {
		result := createMemoryToolResult{
			Success: false,
			Content: "",
			Scope:   memoryArgs.Scope,
			Error:   "memory content cannot be empty",
		}
		return marshalToolResult(result, "create_memory")
	}

	// Validate scope is either "User" or "Chat"
	if memoryArgs.Scope != MemoryScopeUser && memoryArgs.Scope != MemoryScopeChat {
		memoryArgs.Scope = MemoryScopeChat
		t.logger.Warn("invalid scope, defaulting to chat scope", zap.String("user_id", chat.UserID.String()), zap.String("chat_id", chat.ID.String()), zap.String("scope", memoryArgs.Scope))
	}

	// Create embedding for the memory content
	embedding, err := t.CreateEmbedding(ctx, trimmedContent)
	if err != nil {
		t.logger.Error("failed to create embedding for memory",
			zap.String("content_preview", truncateForLog(trimmedContent, 64)),
			zap.Int("content_length", len(trimmedContent)),
			zap.Error(err))
		result := createMemoryToolResult{
			Success: false,
			Content: trimmedContent,
			Scope:   memoryArgs.Scope,
			Error:   fmt.Sprintf("failed to create memory embedding: %v", err),
		}
		return marshalToolResult(result, "create_memory")
	}

	memory, err := t.ds.CreateMemory(ctx, chat.UserID, models.Memory{
		ChatID:  chat.ID,
		Content: trimmedContent,
		Scope:   memoryArgs.Scope,
	}, embedding, chat.PersonalityID)

	if err != nil {
		t.logger.Error("failed to create memory",
			zap.String("content_preview", truncateForLog(trimmedContent, 64)),
			zap.Int("content_length", len(trimmedContent)),
			zap.String("scope", memoryArgs.Scope),
			zap.Error(err))
		result := createMemoryToolResult{
			Success: false,
			Content: trimmedContent,
			Scope:   memoryArgs.Scope,
			Error:   fmt.Sprintf("failed to create memory: %v", err),
		}
		return marshalToolResult(result, "create_memory")
	}

	result := createMemoryToolResult{
		Success:  true,
		MemoryID: memory.ID.String(),
		Content:  trimmedContent,
		Scope:    memoryArgs.Scope,
	}

	return marshalToolResult(result, "create_memory")
}
