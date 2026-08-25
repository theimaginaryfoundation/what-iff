package personality

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
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

var accentColorPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// CreatePersonality creates a new personality
func (h *Handler) CreatePersonality(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// We keep this non-fatal so personality creation never fails due to a preferences/listing hiccup.
	isFirstPersonality := false
	if page, err := h.ds.ListPersonalities(r.Context(), userID, 1, 1, models.PersonalityFilters{}); err == nil {
		isFirstPersonality = page.TotalCount == 0
	} else {
		h.logger.Warn("failed to check personality count before create; skipping auto-default behavior",
			zap.String("user_id", userID.String()),
			zap.Error(err))
	}

	// Parse request body
	var req models.Personality
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Personality name is required", nil)
		return
	}

	if req.SystemPrompt == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "System prompt is required", nil)
		return
	}
	if handlerutils.UTF16CodeUnitCount(req.SystemPrompt) > handlerutils.TextLimitHardMax {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, models.ErrCodeSystemPromptTooLong,
			fmt.Sprintf("System prompt exceeds maximum length of %d characters", handlerutils.TextLimitHardMax), nil)
		return
	}
	if err := normalizeAndValidateStyling(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}

	// Create personality
	personality, err := h.ds.CreatePersonality(r.Context(), userID, req)
	if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Cover image not found", nil)
		return
	} else if err != nil {
		h.logger.Error("failed to create personality",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create personality", err)
		return
	}

	// If this is the first personality, automatically set it as the user's default.
	if isFirstPersonality && personality != nil {
		prefs, err := h.ds.GetUserPreferences(r.Context(), userID)
		if err != nil {
			h.logger.Warn("failed to fetch user preferences for auto-default personality",
				zap.String("user_id", userID.String()),
				zap.String("personality_id", personality.ID.String()),
				zap.Error(err))
		} else {
			prefs.DefaultPersonalityID = personality.ID
			if _, err := h.ds.UpdateUserPreferences(r.Context(), userID, *prefs); err != nil {
				h.logger.Warn("failed to update user preferences for auto-default personality",
					zap.String("user_id", userID.String()),
					zap.String("personality_id", personality.ID.String()),
					zap.Error(err))
			}
		}
	}

	handlerutils.RefreshResponseWriteDeadline(w, 60*time.Second)
	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, personality)
}

// ListPersonalities returns a paginated list of personalities for the authenticated user
func (h *Handler) ListPersonalities(w http.ResponseWriter, r *http.Request) {
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
	searchQuery := queryParams.Get("query")
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")
	personalityIDsStr := queryParams.Get("personality_ids")

	filters := models.PersonalityFilters{}

	if name != "" {
		filters.Name = &name
	}

	if searchQuery != "" {
		filters.Query = &searchQuery
	}

	if personalityIDsStr != "" {
		personalityIDs, err := parsePersonalityIDs(personalityIDsStr)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_ids", err)
			return
		}
		filters.IDs = personalityIDs
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

	// Get paginated personalities
	personalityPage, err := h.ds.ListPersonalities(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list personalities",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list personalities", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, personalityPage)
}

func parsePersonalityIDs(value string) ([]uuid.UUID, error) {
	parts := strings.Split(value, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		id, err := uuid.Parse(trimmed)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetPersonality retrieves a specific personality by ID
func (h *Handler) GetPersonality(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get personality ID from URL
	vars := mux.Vars(r)
	personalityIDStr, ok := vars["id"]
	if !ok || personalityIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Personality ID is required", nil)
		return
	}

	personalityID, err := uuid.Parse(personalityIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}

	// Get personality
	personality, err := h.ds.GetPersonality(r.Context(), userID, personalityID)
	if ent.IsNotFound(err) || err == datastore.ErrPersonalityNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get personality",
			zap.String("user_id", userID.String()),
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get personality", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, personality)
}

// UpdatePersonality updates an existing personality
func (h *Handler) UpdatePersonality(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get personality ID from URL
	vars := mux.Vars(r)
	personalityIDStr, ok := vars["id"]
	if !ok || personalityIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Personality ID is required", nil)
		return
	}

	personalityID, err := uuid.Parse(personalityIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}

	// Parse request body
	var req models.Personality
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Personality name is required", nil)
		return
	}

	if req.SystemPrompt == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "System prompt is required", nil)
		return
	}
	if handlerutils.UTF16CodeUnitCount(req.SystemPrompt) > handlerutils.TextLimitHardMax {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, models.ErrCodeSystemPromptTooLong,
			fmt.Sprintf("System prompt exceeds maximum length of %d characters", handlerutils.TextLimitHardMax), nil)
		return
	}
	if err := normalizeAndValidateStyling(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
		return
	}

	// Set the ID from URL parameter
	req.ID = personalityID

	// Update personality
	personality, err := h.ds.UpdatePersonality(r.Context(), userID, req)
	if ent.IsNotFound(err) || err == datastore.ErrPersonalityNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Cover image not found", nil)
		return
	} else if err != nil {
		h.logger.Error("failed to update personality",
			zap.String("user_id", userID.String()),
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update personality", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, personality)
}

// DeletePersonality deletes a personality
func (h *Handler) DeletePersonality(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	// Get personality ID from URL
	vars := mux.Vars(r)
	personalityIDStr, ok := vars["id"]
	if !ok || personalityIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Personality ID is required", nil)
		return
	}

	personalityID, err := uuid.Parse(personalityIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality ID", err)
		return
	}

	// Delete personality
	err = h.ds.DeletePersonality(r.Context(), userID, personalityID)
	if ent.IsNotFound(err) || err == datastore.ErrPersonalityNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Personality not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to delete personality",
			zap.String("user_id", userID.String()),
			zap.String("personality_id", personalityID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete personality", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func normalizeAndValidateStyling(req *models.Personality) error {
	if req == nil {
		return nil
	}
	if req.AccentColor != nil {
		trimmed := strings.TrimSpace(*req.AccentColor)
		if trimmed == "" {
			req.AccentColor = nil
		} else {
			if !accentColorPattern.MatchString(trimmed) {
				return errors.New("accent_color must be a hex color like #A1B2C3 or #A1B2C3FF")
			}
			req.AccentColor = &trimmed
		}
	}

	if req.ThumbnailCircle != nil {
		circle := req.ThumbnailCircle
		values := []float64{circle.CX, circle.CY, circle.R}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return errors.New("thumbnail_circle must contain finite numeric values")
			}
		}
		if circle.CX < 0 || circle.CX > 1 || circle.CY < 0 || circle.CY > 1 {
			return errors.New("thumbnail_circle center values must be between 0 and 1")
		}
		if circle.R <= 0 || circle.R > 1 {
			return errors.New("thumbnail_circle radius must be greater than 0 and at most 1")
		}
	}

	return nil
}
