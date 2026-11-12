package chat

import (
	"context"
	"errors"
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
// and the chat's agent may change the file from then on. It returns the status to answer with when c cannot be used,
// and undo, which takes back a grant this made, for a send that then fails.
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
	change, err := h.ds.GrantFileEdit(ctx, userID, msg.ChatID, file.ID)
	if err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			return undo, http.StatusNotFound, errors.New("chat not found")
		}
		return undo, http.StatusInternalServerError, errors.New("could not record the comment")
	}
	if change.Created || change.Moved {
		undo = func() {
			if err := h.ds.UndoFileEditGrant(context.WithoutCancel(ctx), userID, change); err != nil {
				h.logger.Warn("could not take back a file edit grant after a failed send", zap.Error(err))
			}
		}
	}
	msg.CommentRefs = []modeltypes.CommentRef{{
		FileID: file.ID, FileName: file.Name, ThreadID: threadID, CommentID: c.CommentID, Quote: filecomments.ShortQuote(c.Quote),
	}}
	msg.AdditionalContext = append(msg.AdditionalContext, models.AdditionalContextItem{
		Type:    models.AdditionalContextTypeFileComment,
		Content: filecomments.MentionContext(file.Name, file.ID.String(), threadID, c.Quote, c.Earlier),
	})
	return undo, 0, nil
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
