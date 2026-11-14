package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"go.uber.org/zap"
)

// attachComment makes msg the document comment c (ADR 0x046): the chat shows a link to the
// thread, the agent gets what the thread is on and the thread so far as context of the message,
// and the chat's agent may change the file from then on (on a page's element, every file of the
// page's project folder: grantProject). It returns the status to answer with when c cannot be
// used, and undo, which takes back the grants this made, for a send that then fails.
func (h *Handler) attachComment(ctx context.Context, msg *models.ChatMessage, c *commentMessageRequest) (undo func(), status int, err error) {
	undo = func() {}
	userID, _ := middleware.GetUserIDFromContext(ctx)
	threadID := strings.TrimSpace(c.ThreadID)
	if c.FileID == uuid.Nil || threadID == "" || len(threadID) > 200 || len(c.CommentID) > 200 {
		return undo, http.StatusBadRequest, errors.New("a comment needs its file and thread")
	}
	file, err := h.ds.GetFileAttachment(ctx, userID, c.FileID)
	if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
		return undo, http.StatusNotFound, errors.New("the commented file was not found")
	}
	if err != nil {
		return undo, http.StatusInternalServerError, errors.New("could not read the commented file")
	}
	// Mentioning the agent on a file lets this chat's agent change it, to do what the comment asks.
	// It is granted before the turn starts, which may use it at once.
	var changes []models.FileEditGrantChange
	undo = func() {
		for _, change := range changes {
			if err := h.ds.UndoFileEditGrant(context.WithoutCancel(ctx), userID, change); err != nil {
				h.logger.Warn("could not take back a file edit grant after a failed send", zap.Error(err))
			}
		}
	}
	grant := func(id uuid.UUID) error {
		change, err := h.ds.GrantFileEdit(ctx, userID, msg.ChatID, id)
		if err == nil && (change.Created || change.Moved) {
			changes = append(changes, change)
		}
		return err
	}
	if err := grant(file.ID); err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			return undo, http.StatusNotFound, errors.New("chat not found")
		}
		return undo, http.StatusInternalServerError, errors.New("could not record the comment")
	}
	project, status, err := h.grantProject(ctx, userID, file, c.ProjectFolder, grant)
	if err != nil {
		undo()
		return func() {}, status, err
	}
	msg.CommentRefs = []modeltypes.CommentRef{{
		FileID: file.ID, FileName: file.Name, ThreadID: threadID, CommentID: c.CommentID, Quote: filecomments.ShortQuote(c.Quote),
	}}
	msg.AdditionalContext = append(msg.AdditionalContext, models.AdditionalContextItem{
		Type:    models.AdditionalContextTypeFileComment,
		Content: filecomments.MentionContext(file.Name, file.ID.String(), threadID, c.Quote, c.Earlier) + project,
	})
	return undo, 0, nil
}

// maxProjectGrant is the most files a comment on a page grants with its project folder: a folder
// with more is not one page's project, and only the commented file is granted.
const maxProjectGrant = 200

// grantProject grants the files of the project folder of a page a comment on its element is about
// (ADR 0x050 §5): the element may be made by any of them, and the change it asks for may be in a
// stylesheet or script rather than the page. The commented file must be in the folder. The top
// level is no project folder (a page there is not given the whole gallery), and grants nothing
// more. It returns what the agent is told about it.
func (h *Handler) grantProject(ctx context.Context, userID uuid.UUID, file *models.FileAttachment, raw string, grant func(uuid.UUID) error) (string, int, error) {
	if strings.TrimSpace(raw) == "" {
		return "", 0, nil
	}
	folder, err := models.NormalizeFolder(raw)
	if err != nil || models.IsReservedFolder(folder) {
		return "", http.StatusBadRequest, errors.New("the page's project folder is not a folder that can be granted")
	}
	if folder == "" {
		return "", 0, nil
	}
	at := file.Folder
	if file.OriginalID != nil {
		// A chat copy carries no folder: its original's is where it is.
		original, err := h.ds.GetFileAttachment(ctx, userID, *file.OriginalID)
		if err != nil {
			return "", http.StatusInternalServerError, errors.New("could not read the commented file")
		}
		at = original.Folder
	}
	if !models.FolderIsWithin(at, folder) {
		return "", http.StatusBadRequest, errors.New("the commented file is not in the page's project folder")
	}
	ids, err := h.ds.FolderFileIDs(ctx, userID, folder, maxProjectGrant)
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("could not read the page's project folder")
	}
	if len(ids) > maxProjectGrant {
		return fmt.Sprintf("\nThe comment is on an element of a page in the folder %q, which holds more than %d files: only the commented file may be changed; ask the user to mention you on another file you need to change.", folder, maxProjectGrant), 0, nil
	}
	for _, id := range ids {
		if id == file.ID || (file.OriginalID != nil && id == *file.OriginalID) {
			continue // granted already
		}
		if err := grant(id); err != nil {
			return "", http.StatusInternalServerError, errors.New("could not record the comment")
		}
	}
	return fmt.Sprintf("\nThe comment is on an element of a page in the folder %q: you may change any file in that folder (and the folders inside it) to do what it asks.", folder), 0, nil
}

// ListFileEditGrants handles GET /chat/{chatId}/file-edit-grants: the files the chat's agent may
// change because the user mentioned it in a comment on them (ADR 0x046).
func (h *Handler) ListFileEditGrants(w http.ResponseWriter, r *http.Request) {
	userID, chatID, ok := h.grantRequest(w, r)
	if !ok {
		return
	}
	grants, err := h.ds.ListFileEditGrants(r.Context(), userID, chatID)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list file edit grants", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]any{"grants": grants})
}

// GetFileEditGrant handles GET /chat/{chatId}/file-edit-grants/{fileId}: whether the chat's agent
// may change the file (any row of it, so a chat copy answers for its original).
func (h *Handler) GetFileEditGrant(w http.ResponseWriter, r *http.Request) {
	userID, chatID, ok := h.grantRequest(w, r)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(mux.Vars(r)["fileId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid file ID", err)
		return
	}
	granted, err := h.ds.HasFileEditGrant(r.Context(), userID, chatID, fileID)
	if err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the file edit grant", err)
		return
	}
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, map[string]bool{"granted": granted})
}

// RevokeFileEditGrant handles DELETE /chat/{chatId}/file-edit-grants/{fileId}: the chat's agent may
// no longer change the file (any row of it). Revoking a grant that is not there is not an error.
func (h *Handler) RevokeFileEditGrant(w http.ResponseWriter, r *http.Request) {
	userID, chatID, ok := h.grantRequest(w, r)
	if !ok {
		return
	}
	fileID, err := uuid.Parse(mux.Vars(r)["fileId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid file ID", err)
		return
	}
	if err := h.ds.RevokeFileEdit(r.Context(), userID, chatID, fileID); err != nil {
		if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
			handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to revoke the file edit grant", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) grantRequest(w http.ResponseWriter, r *http.Request) (userID, chatID uuid.UUID, ok bool) {
	userID, ok = middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, uuid.Nil, false
	}
	chatID, err := uuid.Parse(mux.Vars(r)["chatId"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, chatID, true
}
