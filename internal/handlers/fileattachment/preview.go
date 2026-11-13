package fileattachment

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/imageutil"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// Previews (ADR 0x049 §8): a picture of a file the server cannot draw, a draw.io diagram, sent by
// the browser that drew it and kept beside the stored object it shows (storage.FileKeyForPreview).

const (
	// maxPreviewBytes caps an uploaded preview.
	maxPreviewBytes = 8 << 20
	// maxPreviewSide caps a preview's width and height, and maxPreviewPixels its area: it is decoded
	// whole to make its thumbnail, at 4 bytes a pixel (32 MB at the cap). Checked from the PNG's
	// header before anything is decoded.
	maxPreviewSide   = 4096
	maxPreviewPixels = 8 << 20
)

// PutFilePreview handles PUT /file-attachment/{id}/preview?version=N: the body is a PNG of the file
// as it is at content_version N, stored with a JPEG thumbnail. A version that is no longer the
// file's is a 409 whose details say the current one. Only diagrams take a preview.
func (h *Handler) PutFilePreview(w http.ResponseWriter, r *http.Request) {
	userID, att, ok := h.previewFile(w, r)
	if !ok {
		return
	}
	if !drawio.IsDiagramFile(att.Name, att.FileType) {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Only diagrams have a preview", nil)
		return
	}
	version, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("version")))
	if err != nil || version < 1 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "version must be the content_version the preview shows", err)
		return
	}
	if version != att.ContentVersion {
		h.previewConflict(w, att.ContentVersion)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPreviewBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, handlerutils.CodeNotSet, fmt.Sprintf("Preview too large (max %dMB)", maxPreviewBytes>>20), err)
		} else {
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Could not read the request body", err)
		}
		return
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxPreviewSide || cfg.Height > maxPreviewSide || cfg.Width*cfg.Height > maxPreviewPixels {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet,
			fmt.Sprintf("The preview must be a PNG of at most %d pixels a side and %d megapixels", maxPreviewSide, maxPreviewPixels>>20), err)
		return
	}
	thumb, err := imageutil.GenerateThumbnail(body, imageutil.DefaultThumbnailMaxPx)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "The preview is not a valid image", err)
		return
	}
	store := h.agent.FileStore()
	if store == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusServiceUnavailable, handlerutils.CodeNotSet, "File storage not configured", nil)
		return
	}
	object := att.S3Key
	if err := storage.PutPreview(r.Context(), h.logger, store, object, body, thumb); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to store the preview", err)
		return
	}
	// A save that landed meanwhile moved the file to a new object: this picture is of the old one,
	// which the save releases, so it is dropped now rather than left for that.
	if cur, err := h.ds.GetFileAttachment(r.Context(), userID, att.ID); err == nil && cur.S3Key != object {
		storage.DeletePreviews(r.Context(), h.logger, store, object)
		h.previewConflict(w, cur.ContentVersion)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetFilePreview handles GET /file-attachment/{id}/preview: the file's preview as a PNG, or its
// thumbnail (size=thumbnail) as a JPEG; 404 when it has none of its current content.
//
// Its ETag is the preview's tag (storage.PreviewTag), a hash of the picture kept beside it: it
// changes whenever the picture is replaced, also at the same content_version (an agent's render),
// and a current copy is answered 304 from the tag alone, without downloading the picture. A
// preview stored without a tag is downloaded and hashed instead.
func (h *Handler) GetFilePreview(w http.ResponseWriter, r *http.Request) {
	_, att, ok := h.previewFile(w, r)
	if !ok {
		return
	}
	thumb := r.URL.Query().Get("size") == "thumbnail"
	key, mediaType := storage.FileKeyForPreview(att.S3Key), "image/png"
	if thumb {
		key, mediaType = storage.FileKeyForPreviewThumbnail(att.S3Key), "image/jpeg"
	}
	etagOf := func(tag string) string {
		if thumb {
			tag += "-thumbnail"
		}
		return strconv.Quote(tag)
	}
	store := h.agent.FileStore()
	if store == nil || att.S3Key == "" {
		h.noPreview(w)
		return
	}
	tag, err := storage.PreviewTag(r.Context(), store, att.S3Key)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the preview", err)
		return
	}
	if tag != "" && notModified(w, r, etagOf(tag)) {
		return
	}
	data, err := store.DownloadFile(r.Context(), key)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the preview", err)
		return
	}
	if len(data) == 0 {
		h.noPreview(w)
		return
	}
	if tag == "" && notModified(w, r, etagOf(storage.PreviewTagOf(data))) {
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// notModified sets the preview's ETag and caching, and answers 304 when the caller's copy is it.
func notModified(w http.ResponseWriter, r *http.Request, etag string) bool {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") != etag {
		return false
	}
	w.WriteHeader(http.StatusNotModified)
	return true
}

func (h *Handler) noPreview(w http.ResponseWriter) {
	handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "This file has no preview of its current version", nil)
}

// previewFile is the caller's file named in the path, or false once it has answered.
func (h *Handler) previewFile(w http.ResponseWriter, r *http.Request) (uuid.UUID, *models.FileAttachment, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, nil, false
	}
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid attachment ID", err)
		return uuid.Nil, nil, false
	}
	att, err := h.ds.GetFileAttachment(r.Context(), userID, id)
	switch {
	case err == nil:
		return userID, att, true
	case errors.Is(err, datastore.ErrFileAttachmentNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the file", err)
	}
	return uuid.Nil, nil, false
}

func (h *Handler) previewConflict(w http.ResponseWriter, current int) {
	handlerutils.RespondWithErrorDetails(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet,
		"The file changed since this preview was drawn", map[string]int{"content_version": current}, nil)
}
