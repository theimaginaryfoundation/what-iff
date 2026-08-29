package fileattachment

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
)

type attachmentContentResponse struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Content string    `json:"content"`
}

// GetFileAttachmentContent returns the raw text content of an attachment owned
// by the authenticated user. Content is untrusted user data and is returned as
// JSON text so the frontend can render it as text rather than HTML.
func (h *Handler) GetFileAttachmentContent(w http.ResponseWriter, r *http.Request) {
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

	attachment, err := h.ds.GetFileAttachment(r.Context(), userID, id)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "File attachment not found", err)
		return
	}

	content, ok := h.agent.ResolveAttachmentTextContent(r.Context(), userID, attachment)
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnprocessableEntity, handlerutils.CodeNotSet, "Attachment does not contain readable text", nil)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, attachmentContentResponse{
		ID:      attachment.ID,
		Name:    attachment.Name,
		Content: content,
	})
}
