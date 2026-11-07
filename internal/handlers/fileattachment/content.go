package fileattachment

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/filesave"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

// ReplaceFileAttachmentContent handles PUT /file-attachment/{id}/content (ADR 0x043 §3): the body
// is the file's whole new content. If-Match carries the content_version the client loaded; a
// mismatch is a 412 whose details say the current version, and no If-Match overwrites
// unconditionally. The response is the updated attachment, with its version as the ETag.
func (h *Handler) ReplaceFileAttachmentContent(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid attachment ID", err)
		return
	}
	ifMatch, err := parseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "If-Match must be the file's content_version", err)
		return
	}
	var chatID *uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("chat_id")); raw != "" {
		cid, err := uuid.Parse(raw)
		if err != nil {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat_id", err)
			return
		}
		chatID = &cid
	}

	r.Body = http.MaxBytesReader(w, r.Body, handlerutils.MaxUploadBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, fmt.Sprintf("File too large (max %dMB)", handlerutils.MaxUploadBytes>>20), err)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Could not read the request body", err)
		}
		return
	}

	svc := filesave.New(h.ds, h.agent.FileStore(), h.agent.ChunkPipeline(), h.logger)
	svc.MaxBytes = handlerutils.MaxUploadBytes
	updated, err := svc.Replace(r.Context(), userID, filesave.Request{
		ID: id, Content: body, ContentType: r.Header.Get("Content-Type"), IfMatch: ifMatch, ChatID: chatID,
	})
	var conflict *filesave.ConflictError
	switch {
	case err == nil:
	case errors.As(err, &conflict):
		handlerutils.RespondWithErrorDetails(w, h.logger, http.StatusPreconditionFailed, handlerutils.CodeNotSet,
			"The file changed since it was loaded: reload it, overwrite it, or save as a new file",
			map[string]int{"content_version": conflict.Current}, nil)
		return
	case errors.Is(err, filesave.ErrNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", nil)
		return
	case errors.Is(err, filesave.ErrSkillFile):
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Skill bundle files are changed in the skill editor", nil)
		return
	case errors.Is(err, filesave.ErrTypeMismatch):
		handlerutils.RespondWithError(w, h.logger, http.StatusUnsupportedMediaType, handlerutils.CodeNotSet, "The content type must match the file's", nil)
		return
	case errors.Is(err, filesave.ErrInvalidImage):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "The content is not a valid image", err)
		return
	case errors.Is(err, filesave.ErrTooLarge):
		handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, fmt.Sprintf("File too large (max %dMB)", handlerutils.MaxUploadBytes>>20), nil)
		return
	case errors.Is(err, filesave.ErrNoStore):
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "File storage not configured", nil)
		return
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to save the file", err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(updated.ContentVersion)))
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, updated)
}

// parseIfMatch reads a content version from an If-Match header: a bare or quoted integer, with
// an optional weak prefix. Empty or "*" means no condition.
func parseIfMatch(raw string) (*int, error) {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, `"`)
	if v == "" || v == "*" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return nil, fmt.Errorf("not a content version: %q", raw)
	}
	return &n, nil
}
