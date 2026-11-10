package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type promptAgentStub struct {
	got models.AgentPromptResponse
	err error
}

func (p *promptAgentStub) AnswerPrompt(_ context.Context, _, chatID, promptID uuid.UUID, resp models.AgentPromptResponse) (*models.AgentPrompt, error) {
	p.got = resp
	if p.err != nil {
		return nil, p.err
	}
	return &models.AgentPrompt{ID: promptID, ChatID: chatID, Status: models.AgentPromptAnswered, Response: &resp}, nil
}

type appAgentStub struct {
	viewErr error
	callErr error
	name    string
	args    json.RawMessage
}

func (a *appAgentStub) MCPAppView(_ context.Context, _, _, toolCallID uuid.UUID) (*agent.MCPAppView, error) {
	if a.viewErr != nil {
		return nil, a.viewErr
	}
	return &agent.MCPAppView{ToolCallID: toolCallID, HTML: "<p>hi</p>", ToolInput: json.RawMessage(`{}`)}, nil
}

func (a *appAgentStub) CallMCPAppTool(_ context.Context, _, _, _ uuid.UUID, name string, args json.RawMessage) (json.RawMessage, error) {
	a.name, a.args = name, args
	if a.callErr != nil {
		return nil, a.callErr
	}
	return json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`), nil
}

func serveAs(t *testing.T, h *Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestAnswerPrompt_Statuses(t *testing.T) {
	chatID, promptID := uuid.New(), uuid.New()
	path := "/chat/" + chatID.String() + "/prompts/" + promptID.String() + "/answer"
	tests := []struct {
		name string
		err  error
		body string
		want int
	}{
		{name: "answered", body: `{"action":"accept","answers":{"q1":{"selected":["A"]}}}`, want: http.StatusOK},
		{name: "bad json", body: `{`, want: http.StatusBadRequest},
		{name: "not found", body: `{"action":"accept"}`, err: datastore.ErrAgentPromptNotFound, want: http.StatusNotFound},
		{name: "closed", body: `{"action":"accept"}`, err: datastore.ErrAgentPromptClosed, want: http.StatusConflict},
		{name: "invalid", body: `{"action":"accept"}`, err: agent.ErrInvalidPromptResponse, want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &promptAgentStub{err: tt.err}
			h := &Handler{ds: &fakeStore{}, logger: zap.NewNop(), promptAgent: stub}
			w := serveAs(t, h, http.MethodPost, path, []byte(tt.body))
			assert.Equal(t, tt.want, w.Code, w.Body.String())
			if tt.want == http.StatusOK {
				assert.Equal(t, []string{"A"}, stub.got.Answers["q1"].Selected)
			}
		})
	}
}

func TestMCPAppEndpoints(t *testing.T) {
	chatID, callID := uuid.New(), uuid.New()
	base := "/chat/" + chatID.String() + "/tool-calls/" + callID.String() + "/app"

	stub := &appAgentStub{}
	h := &Handler{ds: &fakeStore{}, logger: zap.NewNop(), appAgent: stub}
	w := serveAs(t, h, http.MethodGet, base, nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var view agent.MCPAppView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&view))
	assert.Equal(t, callID, view.ToolCallID)

	w = serveAs(t, h, http.MethodPost, base+"/tools/call", []byte(`{"name":"refresh","arguments":{"n":1}}`))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"content":[{"type":"text","text":"done"}]}`, w.Body.String())
	assert.Equal(t, "refresh", stub.name)
	assert.JSONEq(t, `{"n":1}`, string(stub.args))

	assert.Equal(t, http.StatusBadRequest, serveAs(t, h, http.MethodPost, base+"/tools/call", []byte(`{"name":" "}`)).Code)

	stub.callErr = mcpclient.ErrToolNotForApps
	assert.Equal(t, http.StatusForbidden, serveAs(t, h, http.MethodPost, base+"/tools/call", []byte(`{"name":"delete_all"}`)).Code)
	stub.viewErr = agent.ErrNoMCPApp
	assert.Equal(t, http.StatusNotFound, serveAs(t, h, http.MethodGet, base, nil).Code)
	stub.viewErr = datastore.ErrToolCallNotFound
	assert.Equal(t, http.StatusNotFound, serveAs(t, h, http.MethodGet, base, nil).Code)
	stub.viewErr = agent.ErrMCPAppUnavailable
	assert.Equal(t, http.StatusBadGateway, serveAs(t, h, http.MethodGet, base, nil).Code)

	h.appAgent = nil
	assert.Equal(t, http.StatusServiceUnavailable, serveAs(t, h, http.MethodGet, base, nil).Code)
}
