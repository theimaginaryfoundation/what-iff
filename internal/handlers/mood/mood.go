package mood

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
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const moodThumbnailMaxPx = 128

func (h *Handler) ListMoods(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	page := handlerutils.ParseIntParam(r.URL.Query().Get("page"), 1)
	limit := handlerutils.ClampIntParam(r.URL.Query().Get("limit"), 20, 1, 100)

	var filters models.MoodFilters
	if name := r.URL.Query().Get("name"); name != "" {
		filters.Name = &name
	}

	result, err := h.ds.ListMoods(r.Context(), userID, page, limit, filters)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list moods", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, result)
}

func (h *Handler) GetMood(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mood ID", err)
		return
	}

	mood, err := h.ds.GetMood(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, datastore.ErrMoodNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Mood not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get mood", err)
		}
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, mood)
}

func (h *Handler) CreateMood(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	var req models.CreateMoodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Name is required", nil)
		return
	}
	if len(req.ImageIDs) > 1 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "A mood can only have one image", nil)
		return
	}

	created, err := h.ds.CreateMood(r.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrFileAttachmentNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, handlerutils.CodeNotSet, "One or more image IDs are invalid, not accessible, or not image files", nil)
		case errors.Is(err, datastore.ErrRitualNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, handlerutils.CodeNotSet, "One or more ritual IDs are invalid or not accessible", nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create mood", err)
		}
		return
	}

	// Thumbnail generation is intentionally decoupled from save.
	// Historical behavior generated thumbnails synchronously here, but that path
	// is currently disabled/deprecated so save does not fail on missing image bytes.
	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, created)
}

func (h *Handler) UpdateMood(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mood ID", err)
		return
	}

	var req models.UpdateMoodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Name is required", nil)
		return
	}
	if req.ImageIDs != nil && len(*req.ImageIDs) > 1 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "A mood can only have one image", nil)
		return
	}

	updated, err := h.ds.UpdateMood(r.Context(), userID, id, req)
	if err != nil {
		switch {
		case errors.Is(err, datastore.ErrMoodNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Mood not found", nil)
		case errors.Is(err, datastore.ErrFileAttachmentNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, handlerutils.CodeNotSet, "One or more image IDs are invalid, not accessible, or not image files", nil)
		case errors.Is(err, datastore.ErrRitualNotFound):
			handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, handlerutils.CodeNotSet, "One or more ritual IDs are invalid or not accessible", nil)
		default:
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update mood", err)
		}
		return
	}

	// Thumbnail generation is intentionally decoupled from save.
	// Historical behavior generated thumbnails synchronously here, but that path
	// is currently disabled/deprecated so save does not fail on missing image bytes.
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updated)
}

func (h *Handler) DeleteMood(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mood ID", err)
		return
	}

	if err := h.ds.DeleteMood(r.Context(), userID, id); err != nil {
		if errors.Is(err, datastore.ErrMoodNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Mood not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete mood", err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AttachToPersonalities replaces the full set of personality associations for a mood.
// POST /api/mood/{id}/personalities  body: {"personality_ids": [...]}
func (h *Handler) AttachToPersonalities(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	moodID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid mood ID", err)
		return
	}

	var req models.AttachMoodToPersonalitiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Verify mood ownership.
	if _, err := h.ds.GetMood(r.Context(), userID, moodID); err != nil {
		if errors.Is(err, datastore.ErrMoodNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Mood not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to attach personalities", err)
		}
		return
	}

	// For each personality, call SetPersonalityMoods to sync the M2M.
	// We rebuild the personality's mood list by ensuring this mood is included / excluded.
	// Simpler: expose a dedicated SetMoodPersonalities in the datastore.
	if err := h.ds.SetMoodPersonalities(r.Context(), userID, moodID, req.PersonalityIDs); err != nil {
		if errors.Is(err, datastore.ErrPersonalityNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "One or more personalities not found", nil)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to attach personalities", err)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// generateAndStoreThumbnail downloads the first mood image from storage, generates a
// JPEG thumbnail (max edge moodThumbnailMaxPx), and saves it on the mood record.
// Uses a background context so work is not cancelled when the HTTP request closes.
func (h *Handler) generateAndStoreThumbnail(userID, moodID, imageID uuid.UUID) {
	ctx := context.Background()

	att, err := h.ds.GetFileAttachment(ctx, userID, imageID)
	if err != nil {
		h.logger.Warn("mood thumbnail: failed to get image attachment",
			zap.String("mood_id", moodID.String()),
			zap.String("image_id", imageID.String()),
			zap.Error(err))
		return
	}

	data, _ := handlerutils.ResolveImageBytes(ctx, h.logger, h.fileStore, userID, att, false)

	if len(data) == 0 {
		h.logger.Warn("mood thumbnail: no image bytes available — tried all resolution paths",
			zap.String("attachment_id", att.ID.String()),
			zap.String("s3_key", att.S3Key))
		return
	}

	thumb, err := imageutil.GenerateThumbnail(data, moodThumbnailMaxPx)
	if err != nil {
		h.logger.Warn("mood thumbnail: failed to generate thumbnail", zap.Error(err))
		return
	}

	if err := h.ds.SetMoodThumbnail(ctx, moodID, thumb); err != nil {
		h.logger.Warn("mood thumbnail: failed to store thumbnail", zap.Error(err))
	}
}
