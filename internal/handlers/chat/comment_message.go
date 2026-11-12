package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/filecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// attachComment makes msg the document comment c (ADR 0x046): the chat shows a link to the
// thread, the agent gets what the thread is on and the thread so far as context of the message,
// and the chat's agent may change the file from then on. It returns the status to answer with when c cannot be used.
func (h *Handler) attachComment(ctx context.Context, msg *models.ChatMessage, c *commentMessageRequest) (int, error) {
	userID, _ := middleware.GetUserIDFromContext(ctx)
	threadID := strings.TrimSpace(c.ThreadID)
	if c.FileID == uuid.Nil || threadID == "" || len(threadID) > 200 || len(c.CommentID) > 200 {
		return http.StatusBadRequest, errors.New("a comment needs its file and thread")
	}
	file, err := h.ds.GetFileAttachment(ctx, userID, c.FileID)
	if errors.Is(err, datastore.ErrFileAttachmentNotFound) {
		return http.StatusNotFound, errors.New("the commented file was not found")
	}
	if err != nil {
		return http.StatusInternalServerError, errors.New("could not read the commented file")
	}
	// Mentioning the agent on a file lets this chat's agent change it, to do what the comment asks.
	if err := h.ds.GrantFileEdit(ctx, userID, msg.ChatID, file.ID); err != nil {
		if errors.Is(err, datastore.ErrChatNotFound) {
			return http.StatusNotFound, errors.New("chat not found")
		}
		return http.StatusInternalServerError, errors.New("could not record the comment")
	}
	msg.CommentRefs = []modeltypes.CommentRef{{
		FileID: file.ID, FileName: file.Name, ThreadID: threadID, CommentID: c.CommentID, Quote: filecomments.ShortQuote(c.Quote),
	}}
	msg.AdditionalContext = append(msg.AdditionalContext, models.AdditionalContextItem{
		Type:    models.AdditionalContextTypeFileComment,
		Content: filecomments.MentionContext(file.Name, file.ID.String(), threadID, c.Quote, c.Earlier),
	})
	return 0, nil
}
