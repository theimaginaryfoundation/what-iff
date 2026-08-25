package ritual

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// CreateRitual creates a new ritual
func (h *Handler) CreateRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Parse request body
	var req models.Ritual
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual name is required", nil)
		return
	}
	if req.Description == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual description is required", nil)
		return
	}
	if req.Content == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual content is required", nil)
		return
	}

	// Create ritual
	ritual, err := h.ds.CreateRitual(r.Context(), userID, req)
	if err != nil {
		h.logger.Error("failed to create ritual",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create ritual", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, ritual)
}

// ListRituals returns a paginated list of rituals for the authenticated user
func (h *Handler) ListRituals(w http.ResponseWriter, r *http.Request) {
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
	personalityIDStr := queryParams.Get("personality_id")
	personalityIDList := queryParams["personality_ids"]
	hasHotkeysStr := queryParams.Get("has_hotkeys")
	globalOnlyStr := queryParams.Get("global_only")
	sortParam := queryParams.Get("sort")
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")

	filters := models.RitualFilters{}

	if name != "" {
		filters.Name = &name
	}

	if searchQuery != "" {
		filters.Query = &searchQuery
	}

	if personalityIDStr != "" {
		personalityID, err := uuid.Parse(personalityIDStr)
		if err == nil {
			filters.PersonalityID = &personalityID
		}
	}

	if len(personalityIDList) > 0 {
		parsed := make([]uuid.UUID, 0, len(personalityIDList))
		for _, raw := range personalityIDList {
			for _, candidate := range strings.Split(raw, ",") {
				candidate = strings.TrimSpace(candidate)
				if candidate == "" {
					continue
				}
				id, err := uuid.Parse(candidate)
				if err != nil {
					handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_ids value", err)
					return
				}
				parsed = append(parsed, id)
			}
		}
		if len(parsed) > 0 {
			filters.PersonalityIDs = parsed
		}
	}

	if globalOnlyStr != "" {
		switch strings.ToLower(strings.TrimSpace(globalOnlyStr)) {
		case "true", "1":
			value := true
			filters.GlobalOnly = &value
		case "false", "0":
			value := false
			filters.GlobalOnly = &value
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid global_only value", nil)
			return
		}
	}

	if sortParam != "" {
		sortValue := models.RitualSort(strings.TrimSpace(sortParam))
		switch sortValue {
		case models.RitualSortNameAsc, models.RitualSortCreatedDesc, models.RitualSortUpdatedDesc:
			filters.Sort = &sortValue
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid sort value", nil)
			return
		}
	}

	if hasHotkeysStr != "" {
		switch strings.ToLower(strings.TrimSpace(hasHotkeysStr)) {
		case "true", "1":
			value := true
			filters.HasHotkeys = &value
		case "false", "0":
			value := false
			filters.HasHotkeys = &value
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid has_hotkeys value", nil)
			return
		}
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

	if filters.GlobalOnly != nil && *filters.GlobalOnly {
		filters.PersonalityID = nil
		filters.PersonalityIDs = nil
	}

	// Get paginated rituals
	ritualPage, err := h.ds.ListRituals(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list rituals",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list rituals", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, ritualPage)
}

// GetRitual retrieves a specific ritual by ID
func (h *Handler) GetRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get ritual ID from URL
	vars := mux.Vars(r)
	ritualIDStr, ok := vars["id"]
	if !ok || ritualIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}

	ritualID, err := uuid.Parse(ritualIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}

	// Get ritual
	ritual, err := h.ds.GetRitual(r.Context(), userID, ritualID)
	if ent.IsNotFound(err) || err == datastore.ErrRitualNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Ritual not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get ritual",
			zap.String("user_id", userID.String()),
			zap.String("ritual_id", ritualID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get ritual", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, ritual)
}

// UpdateRitual updates an existing ritual
func (h *Handler) UpdateRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get ritual ID from URL
	vars := mux.Vars(r)
	ritualIDStr, ok := vars["id"]
	if !ok || ritualIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}

	ritualID, err := uuid.Parse(ritualIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}

	// Parse request body
	var req models.Ritual
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual name is required", nil)
		return
	}
	if req.Description == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual description is required", nil)
		return
	}
	if req.Content == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual content is required", nil)
		return
	}

	// Set the ID from URL parameter
	req.ID = ritualID

	// Update ritual
	ritual, err := h.ds.UpdateRitual(r.Context(), userID, req)
	if ent.IsNotFound(err) || err == datastore.ErrRitualNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Ritual not found", err)
		return
	} else if err == datastore.ErrInvalidRequestBody {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mcp_server_ids", nil)
		return
	} else if err != nil {
		h.logger.Error("failed to update ritual",
			zap.String("user_id", userID.String()),
			zap.String("ritual_id", ritualID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update ritual", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, ritual)
}

// DeleteRitual deletes a ritual
func (h *Handler) DeleteRitual(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get ritual ID from URL
	vars := mux.Vars(r)
	ritualIDStr, ok := vars["id"]
	if !ok || ritualIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Ritual ID is required", nil)
		return
	}

	ritualID, err := uuid.Parse(ritualIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid ritual ID", err)
		return
	}

	// Delete ritual
	err = h.ds.DeleteRitual(r.Context(), userID, ritualID)
	if ent.IsNotFound(err) || err == datastore.ErrRitualNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Ritual not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to delete ritual",
			zap.String("user_id", userID.String()),
			zap.String("ritual_id", ritualID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete ritual", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
