package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

const firstChatGreetingPromptLogContext = "first_chat_auto_greeting"

type chatUpdateRequest struct {
	Name            string     `json:"name"`
	LastMessageTime *time.Time `json:"last_message_time,omitempty"`
	ModelID         *uuid.UUID `json:"model_id,omitempty"`
	PersonalityID   *uuid.UUID `json:"personality_id,omitempty"`
	// DisabledTools, when set, replaces the chat's disabled-tools list.
	// Pass an empty array to re-enable all tools (use defaults).
	// Omit the field entirely to leave existing disabled-tools unchanged.
	DisabledTools *[]string `json:"disabled_tools,omitempty"`
	Tags          *[]string `json:"tags,omitempty"`
	IsFavorite    *bool     `json:"is_favorite,omitempty"`
}

type chatPatchRequest struct {
	Name            *string    `json:"name,omitempty"`
	LastMessageTime *time.Time `json:"last_message_time,omitempty"`
	ModelID         *uuid.UUID `json:"model_id,omitempty"`
	PersonalityID   *uuid.UUID `json:"personality_id,omitempty"`
	DisabledTools   *[]string  `json:"disabled_tools,omitempty"`
	Tags            *[]string  `json:"tags,omitempty"`
	IsFavorite      *bool      `json:"is_favorite,omitempty"`
	// ActiveMoodID updates the effective active mood used for generation.
	ActiveMoodID *uuid.UUID `json:"active_mood_id,omitempty"`
	// IsAutoMood controls mood policy: true=auto, false=manual.
	IsAutoMood *bool `json:"is_auto_mood,omitempty"`
	// ClearActiveMood, when true, explicitly clears the active mood (needed to distinguish
	// "omitted" from "clear" since ActiveMoodID is a pointer).
	ClearActiveMood bool `json:"clear_active_mood,omitempty"`
	// Archived hides the thread from default lists or restores it when set to false.
	Archived *bool `json:"archived,omitempty"`
}

type markChatReadResponse struct {
	UpdatedCount int `json:"updated_count"`
}

type patchChatContextRequest struct {
	ActiveScratchpad *string `json:"active_scratchpad"`
}

// CreateChat creates a new chat session
func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Parse request body
	var req models.Chat
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat name is required", nil)
		return
	}
	normalizedTags, err := modeltypes.NormalizeAndValidateChatTags(req.Tags)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}
	req.Tags = normalizedTags

	// Create chat
	chat, err := h.ds.CreateChat(r.Context(), userID, req)
	if err == datastore.ErrPersonalityNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", nil)
		return
	} else if err == datastore.ErrModelNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Model not found", nil)
		return
	} else if err == datastore.ErrExperimentalModelNotAllowed {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Experimental models are not enabled for this account", nil)
		return
	} else if err != nil {
		h.logger.Error("failed to create chat",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create chat", err)
		return
	}
	if featuregate.IsEntitled(r.Context(), userID) {
		defaultServers, err := h.ds.ListDefaultEnabledMCPServers(r.Context(), userID)
		if err != nil {
			h.logger.Warn("failed to list default-enabled mcp servers",
				zap.String("user_id", userID.String()),
				zap.Error(err))
		} else {
			for _, server := range defaultServers {
				if server == nil {
					continue
				}
				if err := h.ds.AddMCPServerToChat(r.Context(), userID, chat.ID, server.ID); err != nil {
					h.logger.Warn("failed to auto-associate mcp server to new chat",
						zap.String("user_id", userID.String()),
						zap.String("chat_id", chat.ID.String()),
						zap.String("mcp_server_id", server.ID.String()),
						zap.Error(err))
				}
			}
		}
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, chat)
}

func (h *Handler) resolveFirstChatGreetingModelID(ctx context.Context, userID uuid.UUID) *uuid.UUID {
	model, err := h.ds.GetModelByName(ctx, agent.FirstChatGreetingModelName)
	if err != nil {
		if err != datastore.ErrModelNotFound {
			h.logger.Warn("failed to resolve preferred model for first chat greeting; using chat default model",
				zap.String("context", firstChatGreetingPromptLogContext),
				zap.String("user_id", userID.String()),
				zap.String("model_name", agent.FirstChatGreetingModelName),
				zap.Error(err))
		}
		return nil
	}
	if model == nil {
		return nil
	}
	return &model.ID
}

// ListChats returns a paginated list of chats for the authenticated user
func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	queryParams := r.URL.Query()

	// Parse pagination parameters
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	// Parse filter parameters
	name := queryParams.Get("name")
	searchQuery := queryParams.Get("search")
	tagFilter := queryParams.Get("tag")
	personalityIDRaw := queryParams.Get("personality_id")
	isFavoriteRaw := queryParams.Get("is_favorite")
	archivedRaw := queryParams.Get("archived")
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")
	sourceFilter := queryParams.Get("source")
	idsRaw := queryParams.Get("ids")
	// has_hotkeys handled in GetAvailableRituals parsing below per-endpoint

	filters := models.ChatFilters{}

	if sourceFilter = strings.TrimSpace(sourceFilter); sourceFilter != "" {
		filters.Source = &sourceFilter
	}

	if name = strings.TrimSpace(name); name != "" {
		filters.Name = &name
	}

	if searchQuery = strings.TrimSpace(searchQuery); searchQuery != "" {
		filters.Query = &searchQuery
	}

	if tagFilter = strings.TrimSpace(tagFilter); tagFilter != "" {
		filters.Tag = &tagFilter
	}

	if personalityIDRaw = strings.TrimSpace(personalityIDRaw); personalityIDRaw != "" {
		personalityID, err := uuid.Parse(personalityIDRaw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id", err)
			return
		}
		filters.PersonalityID = &personalityID
	}

	if isFavoriteRaw = strings.TrimSpace(isFavoriteRaw); isFavoriteRaw != "" {
		isFavorite, err := strconv.ParseBool(isFavoriteRaw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid is_favorite", err)
			return
		}
		filters.IsFavorite = &isFavorite
	}

	if archivedRaw = strings.TrimSpace(archivedRaw); archivedRaw != "" {
		archived, err := strconv.ParseBool(archivedRaw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid archived", err)
			return
		}
		filters.Archived = &archived
	}

	if minDateStr != "" {
		minDate, err := time.Parse(time.RFC3339, minDateStr)
		if err == nil {
			filters.MinDate = &minDate
		}
	}

	if maxDateStr != "" {
		maxDate, err := time.Parse(time.RFC3339, maxDateStr)
		if err == nil {
			filters.MaxDate = &maxDate
		}
	}

	if idsRaw = strings.TrimSpace(idsRaw); idsRaw != "" {
		const maxListChatIDs = 200
		parts := strings.Split(idsRaw, ",")
		ids := make([]uuid.UUID, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := uuid.Parse(part)
			if err != nil {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ids parameter", err)
				return
			}
			ids = append(ids, id)
			if len(ids) > maxListChatIDs {
				handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Too many ids (max 200)", nil)
				return
			}
		}
		if len(ids) > 0 {
			filters.IDs = ids
		}
	}
	// (has_hotkeys handled in GetAvailableRituals where applicable)

	// Get paginated chats
	chatPage, err := h.ds.ListChats(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list chats",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list chats", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, chatPage)
}

// GetChat retrieves a specific chat by ID
func (h *Handler) GetChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	// Get chat
	chat, err := h.ds.GetChat(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get chat", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, chat)
}

// GetChatContext returns the current scratchpad + summary for a chat.
func (h *Handler) GetChatContext(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	contextPayload, err := h.ds.GetChatContext(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get chat context",
			zap.String("chat_id", chatID.String()),
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to load chat context", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, contextPayload)
}

// PatchChatContext updates the active scratchpad for the chat's active personality.
// v1 semantics are last-write-wins.
func (h *Handler) PatchChatContext(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	var req patchChatContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if req.ActiveScratchpad == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "active_scratchpad is required", nil)
		return
	}

	chat, err := h.ds.GetChat(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to load chat before context patch",
			zap.String("chat_id", chatID.String()),
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to patch chat context", err)
		return
	}
	if chat.PersonalityID == uuid.Nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat personality not found", datastore.ErrPersonalityNotFound)
		return
	}

	_, err = h.ds.UpdatePersonalityScratchpad(r.Context(), userID, models.Personality{
		ID:         chat.PersonalityID,
		Scratchpad: *req.ActiveScratchpad,
	})
	if ent.IsNotFound(err) || err == datastore.ErrPersonalityNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to update scratchpad from chat context patch",
			zap.String("chat_id", chatID.String()),
			zap.String("user_id", userID.String()),
			zap.String("personality_id", chat.PersonalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to patch chat context", err)
		return
	}

	contextPayload, err := h.ds.GetChatContext(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to reload chat context after patch",
			zap.String("chat_id", chatID.String()),
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to patch chat context", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, contextPayload)
}

// UpdateChat updates an existing chat
func (h *Handler) UpdateChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	// Parse request body
	var req chatUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	name := strings.TrimSpace(req.Name)
	if name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat name is required", nil)
		return
	}

	// Load existing chat so omitted fields don't get reset by JSON default values.
	existing, err := h.ds.GetChat(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get chat for update",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update chat", err)
		return
	}

	updated := *existing
	updated.ID = chatID
	updated.Name = name
	updated.LastMessageTime = req.LastMessageTime
	if req.ModelID != nil {
		updated.ModelID = *req.ModelID
	}
	if req.PersonalityID != nil {
		updated.PersonalityID = *req.PersonalityID
	}
	if req.DisabledTools != nil {
		updated.DisabledTools = *req.DisabledTools
	}
	if req.Tags != nil {
		normalizedTags, err := modeltypes.NormalizeAndValidateChatTags(*req.Tags)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
			return
		}
		updated.Tags = normalizedTags
	}
	if req.IsFavorite != nil {
		updated.IsFavorite = req.IsFavorite
	} else {
		// Shallow copy of *existing aliases IsFavorite; nil means "omit" for datastore.
		updated.IsFavorite = nil
	}

	// Update chat
	chat, err := h.ds.UpdateChat(r.Context(), userID, updated)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err == datastore.ErrMoodNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mood for chat personality", err)
		return
	} else if err == datastore.ErrFavoriteLimitExceeded {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "You can only favorite up to 10 chats", err)
		return
	} else if err == datastore.ErrExperimentalModelNotAllowed {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Experimental models are not enabled for this account", err)
		return
	} else if err != nil {
		h.logger.Error("failed to update chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update chat", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, chat)
}

// PatchChat partially updates an existing chat.
// Omitted fields are preserved.
func (h *Handler) PatchChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	// Parse request body
	var req chatPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Must include at least one field to patch.
	if req.Name == nil && req.LastMessageTime == nil && req.ModelID == nil && req.PersonalityID == nil && req.DisabledTools == nil && req.Tags == nil && req.IsFavorite == nil && req.ActiveMoodID == nil && req.IsAutoMood == nil && !req.ClearActiveMood && req.Archived == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "No fields to update", nil)
		return
	}

	// Load existing chat so omitted fields don't get reset.
	existing, err := h.ds.GetChat(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get chat for patch",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update chat", err)
		return
	}

	updated := *existing
	updated.ID = chatID

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat name is required", nil)
			return
		}
		updated.Name = name
	}

	if req.LastMessageTime != nil {
		updated.LastMessageTime = req.LastMessageTime
	}

	if req.ModelID != nil {
		updated.ModelID = *req.ModelID
	}

	personalityChanged := false
	if req.PersonalityID != nil {
		personalityChanged = existing.PersonalityID != *req.PersonalityID
		updated.PersonalityID = *req.PersonalityID
	}

	if req.DisabledTools != nil {
		updated.DisabledTools = *req.DisabledTools
	}
	if req.Tags != nil {
		normalizedTags, err := modeltypes.NormalizeAndValidateChatTags(*req.Tags)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
			return
		}
		updated.Tags = normalizedTags
	}
	if req.IsFavorite != nil {
		updated.IsFavorite = req.IsFavorite
	} else {
		updated.IsFavorite = nil
	}

	if req.ClearActiveMood {
		updated.ActiveMoodID = nil
		updated.IsAutoMood = true
	} else if req.ActiveMoodID != nil {
		updated.ActiveMoodID = req.ActiveMoodID
	} else if personalityChanged {
		// Moods are personality-scoped: a mood pinned under the previous personality
		// is not attached to the new one and would fail UpdateChat's mood invariant
		// (ErrMoodNotFound), silently reverting the personality change. Reset to Auto
		// unless the same request explicitly sets a mood.
		updated.ActiveMoodID = nil
		updated.IsAutoMood = true
	}
	if req.IsAutoMood != nil {
		updated.IsAutoMood = *req.IsAutoMood
	}
	if req.Archived != nil {
		updated.Archived = req.Archived
	}

	chat, err := h.ds.UpdateChat(r.Context(), userID, updated)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err == datastore.ErrMoodNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mood for chat personality", err)
		return
	} else if err == datastore.ErrFavoriteLimitExceeded {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "You can only favorite up to 10 chats", err)
		return
	} else if err == datastore.ErrExperimentalModelNotAllowed {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Experimental models are not enabled for this account", err)
		return
	} else if err != nil {
		h.logger.Error("failed to patch chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update chat", err)
		return
	}

	// Lazy rehydration: when an imported thread is restored (unarchived) for the first time, kick off
	// background summarization so it has a checkpoint summary + recent-turn window before the user
	// resumes it. Guarded so it only fires once (empty checkpoint summary) and only for imports.
	if h.agent != nil && isUnarchiveTransition(existing, req.Archived) && needsRehydration(existing) {
		h.agent.EnqueueThreadRehydration(r.Context(), userID, chatID)
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, chat)
}

// isUnarchiveTransition reports whether this PATCH flips the chat from archived to active.
func isUnarchiveTransition(existing *models.Chat, requested *bool) bool {
	if requested == nil || *requested {
		return false // not setting archived=false
	}
	return existing.Archived != nil && *existing.Archived
}

// needsRehydration reports whether an imported thread still needs its one-time lazy summary.
func needsRehydration(existing *models.Chat) bool {
	if existing.Source == nil || *existing.Source == "" {
		return false
	}
	// Already summarized (checkpoint present) or already in-flight/terminal — skip.
	switch existing.RehydrationState {
	case models.RehydrationStateProcessing, models.RehydrationStateReady:
		return false
	}
	return existing.CheckpointSummary == ""
}

// MarkChatRead marks all unread assistant messages in a chat as read.
func (h *Handler) MarkChatRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	updatedCount, err := h.ds.MarkChatMessagesRead(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	}
	if err != nil {
		h.logger.Error("failed to mark chat read",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to mark chat as read", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, markChatReadResponse{
		UpdatedCount: updatedCount,
	})
}

// DeleteChat deletes a chat and all its messages
func (h *Handler) DeleteChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["id"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	// Delete chat
	err = h.ds.DeleteChat(r.Context(), userID, chatID)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to delete chat",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete chat", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetAvailableRituals returns a paginated list of rituals available for a specific chat
func (h *Handler) GetAvailableRituals(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get chat ID from URL
	vars := mux.Vars(r)
	chatIDStr, ok := vars["chatId"]
	if !ok || chatIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Chat ID is required", nil)
		return
	}

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	queryParams := r.URL.Query()

	// Parse pagination parameters
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	// Parse filter parameters
	name := queryParams.Get("name")
	searchQuery := queryParams.Get("search")
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")

	filters := models.RitualFilters{}

	if name != "" {
		filters.Name = &name
	}

	if searchQuery != "" {
		filters.Query = &searchQuery
	}

	if minDateStr != "" {
		minDate, err := time.Parse(time.RFC3339, minDateStr)
		if err == nil {
			filters.MinDate = &minDate
		}
	}

	if maxDateStr != "" {
		maxDate, err := time.Parse(time.RFC3339, maxDateStr)
		if err == nil {
			filters.MaxDate = &maxDate
		}
	}

	// Parse has_hotkeys filter (boolean)
	if has := queryParams.Get("has_hotkeys"); has != "" {
		if b, err := strconv.ParseBool(has); err == nil {
			filters.HasHotkeys = &b
		}
	}

	// Get available rituals for this chat
	ritualsPage, err := h.ds.GetAvailableRituals(r.Context(), userID, chatID, page, pageSize, filters)
	if ent.IsNotFound(err) || err == datastore.ErrChatNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Chat not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get available rituals",
			zap.String("user_id", userID.String()),
			zap.String("chat_id", chatID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get available rituals", err)
		return
	}
	// If caller requested only hotkey rituals, include system rituals that have user bindings
	if filters.HasHotkeys != nil && *filters.HasHotkeys {
		systemR := agent.ListSystemRituals(time.Now().UTC())

		bindings, bErr := h.ds.GetSystemBindingsForUser(r.Context(), userID)
		if bErr == nil && len(bindings) > 0 {
			bmap := make(map[uuid.UUID]string, len(bindings))
			for _, b := range bindings {
				if b.Hotkeys != "" {
					bmap[b.RitualID] = b.Hotkeys
				}
			}
			for _, sr := range systemR {
				if hk, ok := bmap[sr.ID]; ok {
					rit := sr
					rit.Hotkeys = hk
					ritualsPage.Results = append(ritualsPage.Results, rit)
					ritualsPage.TotalCount++
				}
			}
		} else if bErr != nil {
			h.logger.Warn("failed to load system ritual bindings", zap.Error(bErr))
		}
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, ritualsPage)
}
