package chat

import (
	"bytes"
	"context"
	"errors"
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
	store := &fakeStore{getFileAttachmentFn: func(_ context.Context, u, id uuid.UUID) (*models.FileAttachment, error) {
		if u != userID || id != fileID {
			return nil, datastore.ErrFileAttachmentNotFound
		}
		return &models.FileAttachment{ID: fileID, Name: "plan.md"}, nil
	}}
	h := &Handler{
		ds:           store,
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
	require.Equal(t, [][2]uuid.UUID{{chatID, fileID}}, store.grants, "the chat's agent may now change the file")

	// Someone else's file, or no thread, is refused before the agent sees anything.
	ag.got = nil
	rec = postMessage(t, h, userID, chatID, `{"message":"x","origin":"user","comment":{"file_id":"`+uuid.NewString()+`","thread_id":"t1"}}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = postMessage(t, h, userID, chatID, `{"message":"x","origin":"user","comment":{"file_id":"`+fileID.String()+`","thread_id":" "}}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, ag.got)
}

type failingAgent struct{ retryAgentStub }

func (*failingAgent) HandleUserMessage(context.Context, models.ChatMessage) (*models.ChatMessageResponse, error) {
	return nil, errors.New("provider down")
}

func TestCreateChatMessage_ACommentThatFailsToSendGrantsNothing(t *testing.T) {
	userID, chatID, fileID := uuid.New(), uuid.New(), uuid.New()
	store := &fakeStore{getFileAttachmentFn: func(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
		return &models.FileAttachment{ID: fileID, Name: "plan.md"}, nil
	}}
	h := &Handler{ds: store, logger: zap.NewNop(), messageAgent: &failingAgent{}}
	rec := postMessage(t, h, userID, chatID, `{"message":"@Iris fix it","origin":"user","comment":{"file_id":"`+fileID.String()+`","thread_id":"t1"}}`)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Empty(t, store.grants, "the grant made for the send is taken back")
}

func TestFileEditGrants_ListAndRevoke(t *testing.T) {
	userID, chatID, fileID := uuid.New(), uuid.New(), uuid.New()
	store := &fakeStore{grants: [][2]uuid.UUID{{chatID, fileID}}}
	h := &Handler{ds: store, logger: zap.NewNop()}
	call := func(method, path string, vars map[string]string, fn http.HandlerFunc) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req = mux.SetURLVars(req, vars)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
		rec := httptest.NewRecorder()
		fn(rec, req)
		return rec
	}
	rec := call(http.MethodGet, "/chat/x/file-edit-grants", map[string]string{"chatId": chatID.String()}, h.ListFileEditGrants)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), fileID.String())

	rec = call(http.MethodGet, "/chat/x/file-edit-grants/y", map[string]string{"chatId": chatID.String(), "fileId": fileID.String()}, h.GetFileEditGrant)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"granted":true}`, rec.Body.String())

	rec = call(http.MethodDelete, "/chat/x/file-edit-grants/y", map[string]string{"chatId": chatID.String(), "fileId": fileID.String()}, h.RevokeFileEditGrant)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, store.grants)
	rec = call(http.MethodGet, "/chat/x/file-edit-grants/y", map[string]string{"chatId": chatID.String(), "fileId": fileID.String()}, h.GetFileEditGrant)
	require.JSONEq(t, `{"granted":false}`, rec.Body.String())

	rec = call(http.MethodDelete, "/chat/x/file-edit-grants/y", map[string]string{"chatId": chatID.String(), "fileId": "nope"}, h.RevokeFileEditGrant)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
