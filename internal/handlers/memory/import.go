package memory

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

const maxMemoryImportBytes = 32 << 20 // 32 MiB

// ImportMemories handles POST /memory/import and imports a prior memory export ZIP.
func (h *Handler) ImportMemories(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	if h.oaiClient == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "Memory import is not configured", errMemoryImportUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMemoryImportBytes)
	if err := r.ParseMultipartForm(maxMemoryImportBytes); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid multipart form", err)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Missing import file", err)
		return
	}
	defer file.Close()

	limited := io.LimitReader(file, maxMemoryImportBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Failed to read import file", err)
		return
	}
	if len(payload) > maxMemoryImportBytes {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Import file too large (max 32MB)", nil)
		return
	}
	h.logger.Info("memory import upload received",
		zap.String("user_id", userID.String()),
		zap.Int("payload_bytes", len(payload)))

	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Import file must be a ZIP archive", err)
		return
	}
	h.logger.Info("memory import archive opened",
		zap.String("user_id", userID.String()),
		zap.Int("zip_entries", len(zr.File)))

	result, err := h.ds.ImportMemoriesWithBatchEmbeddings(r.Context(), userID, zr, h.createEmbedding, h.createEmbeddings)
	if err != nil {
		h.logger.Error("failed to import memories",
			zap.String("user_id", userID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to import memories", err)
		return
	}
	h.logger.Info("memory import completed",
		zap.String("user_id", userID.String()),
		zap.Int("imported_count", result.ImportedCount),
		zap.Int("duplicate_count", result.DuplicateCount),
		zap.Int("invalid_record_count", result.InvalidRecordCount),
		zap.Int("skipped_missing_chat_count", result.SkippedMissingChat),
		zap.Int("skipped_missing_personality_count", result.SkippedMissingPersonality))

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, result)
}
