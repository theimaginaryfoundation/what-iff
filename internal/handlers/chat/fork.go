package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// BranchAgent is the agent surface used after a fork: summarizing a branch whose parent checkpoint
// could not be reused.
type BranchAgent interface {
	EnqueueBranchRehydration(ctx context.Context, userID, chatID uuid.UUID)
}

type forkChatRequest struct {
	// MessageID is the branch point in the parent thread.
	MessageID string `json:"message_id"`
	// IncludeMessage keeps the branch-point message (default true). False ends the branch just
	// before it, for re-asking a user turn differently.
	IncludeMessage *bool `json:"include_message"`
	// Name optionally overrides the generated "What if: <parent>" name.
	Name string `json:"name"`
}

type forkChatResponse struct {
	Chat           *models.Chat `json:"chat"`
	CopiedMessages int          `json:"copied_messages"`
}

const maxForkNameInput = 200

// ForkChat POST /chat/{id}/fork creates a "What if…" branch: a new thread sharing the parent's
// history up to message_id, then free to diverge. Returns 201 with the branch.
func (h *Handler) ForkChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	parentID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	var req forkChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	messageID, err := uuid.Parse(strings.TrimSpace(req.MessageID))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "message_id must be a valid UUID", err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > maxForkNameInput {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "name is too long", nil)
		return
	}
	include := true
	if req.IncludeMessage != nil {
		include = *req.IncludeMessage
	}

	result, err := h.ds.ForkChat(r.Context(), userID, models.ForkChatParams{
		ParentChatID:   parentID,
		MessageID:      messageID,
		IncludeMessage: include,
		Name:           name,
	})
	switch {
	case errors.Is(err, datastore.ErrChatNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
		return
	case errors.Is(err, datastore.ErrForkMessageNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Message not found in this chat", nil)
		return
	case err != nil:
		h.logger.Error("failed to fork chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", parentID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to branch chat", err)
		return
	}

	if result.NeedsRehydration && h.branchAgent != nil {
		h.branchAgent.EnqueueBranchRehydration(r.Context(), userID, result.Chat.ID)
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, forkChatResponse{
		Chat:           result.Chat,
		CopiedMessages: result.CopiedMessages,
	})
}

// GetChatLineage GET /chat/{id}/lineage returns where a thread branched from (if anywhere) and the
// branches taken off it, so the UI can show "branched from…" and per-message branch markers.
func (h *Handler) GetChatLineage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	chatID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	chat, err := h.ds.GetChat(r.Context(), userID, chatID)
	if err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
			return
		}
		h.logger.Error("chat lineage: failed to load chat", zap.String("chat_id", chatID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat", err)
		return
	}
	if chat == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", nil)
		return
	}

	lineage := models.ChatLineage{Branches: []models.ChatBranchSummary{}}
	if chat.ForkedFromChatID != nil {
		parent := &models.ChatLineageParent{ID: *chat.ForkedFromChatID, MessageID: chat.ForkedFromMessageID}
		name, err := h.ds.GetChatName(r.Context(), userID, *chat.ForkedFromChatID)
		switch {
		case errors.Is(err, datastore.ErrChatNotFound):
			parent.Deleted = true
		case err != nil:
			h.logger.Error("chat lineage: failed to load parent", zap.String("chat_id", chatID.String()), zap.Error(err))
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat lineage", err)
			return
		default:
			parent.Name = name
		}
		lineage.Parent = parent
	}

	branches, err := h.ds.ListChatBranches(r.Context(), userID, chatID)
	if err != nil {
		h.logger.Error("chat lineage: failed to list branches", zap.String("chat_id", chatID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat lineage", err)
		return
	}
	lineage.Branches = append(lineage.Branches, branches...)

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, lineage)
}
