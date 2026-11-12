package chat

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type capturingAgent struct {
	retryAgentStub
	got *models.ChatMessage
}

func (c *capturingAgent) HandleUserMessage(_ context.Context, request models.ChatMessage) (*models.ChatMessageResponse, error) {
	c.got = &request
	return &models.ChatMessageResponse{}, nil
}

func postMessage(t *testing.T, h *Handler, userID, chatID uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/chat/"+chatID.String()+"/chat-message", bytes.NewBufferString(body))
	req = mux.SetURLVars(req, map[string]string{"chatId": chatID.String()})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()
	h.CreateChatMessage(rec, req)
	return rec
}

func TestCreateChatMessage_FromADocumentComment(t *testing.T) {
	userID, chatID, fileID := uuid.New(), uuid.New(), uuid.New()
	ag := &capturingAgent{}
	h := &Handler{
		ds: &fakeStore{getFileAttachmentFn: func(_ context.Context, u, id uuid.UUID) (*models.FileAttachment, error) {
			if u != userID || id != fileID {
				return nil, datastore.ErrFileAttachmentNotFound
			}
			return &models.FileAttachment{ID: fileID, Name: "plan.md"}, nil
		}},
		logger:       zap.NewNop(),
		messageAgent: ag,
	}

	rec := postMessage(t, h, userID, chatID, `{"message":"@Iris can you tighten this?","origin":"user","comment":{
		"file_id":"`+fileID.String()+`","thread_id":"t1","comment_id":"c2","quote":"major   features",
		"earlier":[{"author_name":"Pat Lee","text":"Which three?"}]}}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.NotNil(t, ag.got)
	require.Equal(t, "@Iris can you tighten this?", ag.got.Message, "the comment's text is the message")
	require.Len(t, ag.got.CommentRefs, 1)
	ref := ag.got.CommentRefs[0]
	require.Equal(t, fileID, ref.FileID)
	require.Equal(t, "plan.md", ref.FileName)
	require.Equal(t, "t1", ref.ThreadID)
	require.Equal(t, "c2", ref.CommentID)
	require.Equal(t, "major features", ref.Quote)
	require.Len(t, ag.got.AdditionalContext, 1)
	ctx := ag.got.AdditionalContext[0]
	require.Equal(t, models.AdditionalContextTypeFileComment, ctx.Type)
	require.Contains(t, ctx.Content, `"plan.md"`)
	require.Contains(t, ctx.Content, `The thread is on: "major features"`)
	require.Contains(t, ctx.Content, "- Pat Lee: Which three?")

	// Someone else's file, or no thread, is refused before the agent sees anything.
	ag.got = nil
	rec = postMessage(t, h, userID, chatID, `{"message":"x","origin":"user","comment":{"file_id":"`+uuid.NewString()+`","thread_id":"t1"}}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = postMessage(t, h, userID, chatID, `{"message":"x","origin":"user","comment":{"file_id":"`+fileID.String()+`","thread_id":" "}}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, ag.got)
}
