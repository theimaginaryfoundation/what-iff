package memory

import (
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

// Entity wiki routes for the Memories panel (ADR 0x034). Agents write entities with their tools;
// here the user browses, edits and deletes them, and restores the ones an agent archived.

// maxEntityPatchBytes bounds an entity edit request body: the 5000-rune body is at most ~20KB.
const maxEntityPatchBytes = 64 << 10

// ListAgentEntities handles GET /memory/entities?page=&limit=&query=&personality_id=&global_only=&archived=.
func (h *Handler) ListAgentEntities(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	q := r.URL.Query()
	page := max(handlerutils.ParseIntParam(q.Get("page"), 1), 1)
	limit := handlerutils.ParseIntParam(q.Get("limit"), 50)
	if limit < 1 || limit > 200 {
		limit = 50
	}
	filters := models.AgentEntityFilters{Limit: limit, Offset: (page - 1) * limit, Archived: q.Get("archived") == "true"}
	if query := strings.TrimSpace(q.Get("query")); query != "" {
		filters.Query = &query
	}
	switch {
	case q.Get("global_only") == "true":
		global := uuid.Nil
		filters.PinnedPersonalityID = &global
	case q.Get("personality_id") != "":
		pid, err := uuid.Parse(q.Get("personality_id"))
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_id", err)
			return
		}
		filters.PinnedPersonalityID = &pid
	}
	entities, total, err := h.ds.ListAgentEntities(r.Context(), userID, filters)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list entities", err)
		return
	}
	results := make([]any, 0, len(entities))
	for _, e := range entities {
		results = append(results, e)
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, models.PaginatedResponse{
		Results:    results,
		TotalCount: total,
		TotalPages: (total + limit - 1) / limit,
		Page:       page,
	})
}

type patchAgentEntityRequest struct {
	Body *string `json:"body"`
}

// PatchAgentEntity handles PATCH /memory/entities/{id}: the user edits an entity's body.
func (h *Handler) PatchAgentEntity(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.entityRequest(w, r)
	if !ok {
		return
	}
	var req patchAgentEntityRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEntityPatchBytes)).Decode(&req); err != nil || req.Body == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "body is required", err)
		return
	}
	e, err := h.ds.SetAgentEntityBody(r.Context(), userID, id, *req.Body)
	switch {
	case errors.Is(err, datastore.ErrAgentEntityNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Entity not found", err)
	case errors.Is(err, models.ErrEntityBodyTooLong):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Entity text is limited to 5000 characters", err)
	case err != nil:
		h.logger.Error("failed to update entity", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update entity", err)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, e)
	}
}

// RestoreAgentEntity handles POST /memory/entities/{id}/restore: the user brings back an entity an
// agent archived.
func (h *Handler) RestoreAgentEntity(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.entityRequest(w, r)
	if !ok {
		return
	}
	e, err := h.ds.RestoreAgentEntity(r.Context(), userID, id)
	switch {
	case errors.Is(err, datastore.ErrAgentEntityNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Archived entity not found", err)
	case errors.Is(err, datastore.ErrAgentEntityExists):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Another entity has this name now; edit or delete that one first", err)
	case err != nil:
		h.logger.Error("failed to restore entity", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to restore entity", err)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, e)
	}
}

// DeleteAgentEntity handles DELETE /memory/entities/{id}: removes a live or archived entity for good.
func (h *Handler) DeleteAgentEntity(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := h.entityRequest(w, r)
	if !ok {
		return
	}
	err := h.ds.DeleteAgentEntity(r.Context(), userID, id)
	switch {
	case errors.Is(err, datastore.ErrAgentEntityNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Entity not found", err)
	case err != nil:
		h.logger.Error("failed to delete entity", zap.String("user_id", userID.String()), zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete entity", err)
	default:
		handlerutils.RespondWithNoContent(w)
	}
}

// entityRequest reads the caller and the {id} path variable, responding itself when either is bad.
func (h *Handler) entityRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid entity ID", err)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}
