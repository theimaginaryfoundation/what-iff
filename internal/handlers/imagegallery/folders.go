package imagegallery

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Folders are a label on a gallery image (file_attachments.folder), not a place in object storage,
// so moving an image or a whole folder never touches the stored bytes. A folder exists while it
// holds at least one image; the gallery UI keeps a freshly created, still-empty folder to itself
// until something is moved into it.

const (
	maxMoveIDs       = 500
	maxFolderBodyLen = 1 << 20
)

type folderListResponse struct {
	Folders []models.FolderCount `json:"folders"`
}

type moveImagesRequest struct {
	IDs    []uuid.UUID `json:"ids"`
	Folder string      `json:"folder"`
}

type moveFolderRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type moveResponse struct {
	Moved int `json:"moved"`
}

// ListFolders returns the folders that hold gallery images, with how many images sit directly in
// each. The top level is implied and not listed.
func (h *Handler) ListFolders(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	folders, err := h.ds.ListImageFolders(r.Context(), userID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list folders", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, folderListResponse{Folders: folders})
}

// MoveImages puts the given gallery images in a folder ("" is the top level). Ids that are not the
// caller's images are skipped; moved says how many were.
func (h *Handler) MoveImages(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	var req moveImagesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFolderBodyLen)).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	if len(req.IDs) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Choose at least one image", nil)
		return
	}
	if len(req.IDs) > maxMoveIDs {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Too many images in one move", nil)
		return
	}
	folder, err := models.NormalizeFolder(req.Folder)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, folderErrorMessage(err), err)
		return
	}
	moved, err := h.ds.MoveFileAttachmentsToFolder(r.Context(), userID, req.IDs, folder)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to move images", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, moveResponse{Moved: moved})
}

// MoveFolder renames a folder, carrying everything beneath it along; to may be an existing folder
// (they merge) or the top level.
func (h *Handler) MoveFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	var req moveFolderRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFolderBodyLen)).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}
	from, err := models.NormalizeFolder(req.From)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, folderErrorMessage(err), err)
		return
	}
	to, err := models.NormalizeFolder(req.To)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, folderErrorMessage(err), err)
		return
	}
	moved, err := h.ds.MoveImageFolder(r.Context(), userID, from, to)
	switch {
	case err == nil:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, moveResponse{Moved: moved})
	case errors.Is(err, models.ErrInvalidFolder), errors.Is(err, datastore.ErrFolderIntoItself):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, folderErrorMessage(err), err)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to move folder", err)
	}
}

// folderErrorMessage is the text a client sees for a refused folder path.
func folderErrorMessage(err error) string {
	if errors.Is(err, datastore.ErrFolderIntoItself) {
		return "A folder cannot be moved into itself"
	}
	return err.Error()
}
