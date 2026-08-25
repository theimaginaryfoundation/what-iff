package memory

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ListMemories returns a paginated list of memories for the authenticated user.
// Supports optional sort (created_desc, created_asc, updated_desc) and status (active, inactive) query params.
func (h *Handler) ListMemories(w http.ResponseWriter, r *http.Request) {
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
	chatIDStr := queryParams.Get("chat_id")
	level := queryParams.Get("level")
	memoryType := queryParams.Get("type")
	starredStr := queryParams.Get("starred")
	pinnedPersonalityIDStr := queryParams.Get("pinned_personality_id")
	pinnedPersonalityIDList := queryParams["personality_ids"]
	globalOnlyStr := queryParams.Get("global_only")
	searchQuery := queryParams.Get("query")
	sortParam := strings.TrimSpace(queryParams.Get("sort"))
	status := strings.TrimSpace(queryParams.Get("status"))
	minDateStr := queryParams.Get("min_date")
	maxDateStr := queryParams.Get("max_date")

	filters := models.MemoryFilters{}

	if chatIDStr != "" {
		chatID, err := uuid.Parse(chatIDStr)
		if err == nil {
			filters.ChatID = &chatID
		}
	}

	if level != "" {
		levelValue := models.MemoryLevel(level)
		filters.Level = &levelValue
	}

	if memoryType != "" {
		typeValue := models.MemoryType(memoryType)
		filters.Type = &typeValue
	}

	if starredStr != "" {
		starred, err := strconv.ParseBool(starredStr)
		if err == nil {
			filters.Starred = &starred
		}
	}

	if pinnedPersonalityIDStr != "" {
		pinnedID, err := uuid.Parse(pinnedPersonalityIDStr)
		if err == nil {
			filters.PinnedPersonalityID = &pinnedID
		}
	}
	if len(pinnedPersonalityIDList) > 0 {
		ids := make([]uuid.UUID, 0, len(pinnedPersonalityIDList))
		for _, raw := range pinnedPersonalityIDList {
			for _, token := range strings.Split(raw, ",") {
				candidate := strings.TrimSpace(token)
				if candidate == "" {
					continue
				}
				pinnedID, err := uuid.Parse(candidate)
				if err != nil {
					handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid personality_ids entry", err)
					return
				}
				ids = append(ids, pinnedID)
			}
		}
		if len(ids) > 0 {
			filters.PinnedPersonalityIDs = ids
		}
	}
	if globalOnlyStr != "" {
		globalOnly, err := strconv.ParseBool(globalOnlyStr)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid global_only flag", err)
			return
		}
		filters.GlobalOnly = &globalOnly
		if globalOnly {
			filters.PinnedPersonalityID = nil
			filters.PinnedPersonalityIDs = nil
		}
	}

	if searchQuery != "" {
		filters.Query = &searchQuery
	}
	if sortParam != "" {
		sortValue := models.MemorySort(sortParam)
		switch sortValue {
		case models.MemorySortCreatedDesc, models.MemorySortCreatedAsc, models.MemorySortUpdatedDesc:
			filters.Sort = &sortValue
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid sort value", nil)
			return
		}
	}
	if status != "" {
		statusValue := models.MemoryStatus(status)
		if statusValue != models.MemoryStatusActive && statusValue != models.MemoryStatusInactive {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid status value", nil)
			return
		}
		filters.Status = &statusValue
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

	// Get paginated memories
	memoryPage, err := h.ds.ListMemories(r.Context(), userID, page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list memories",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list memories", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, memoryPage)
}
