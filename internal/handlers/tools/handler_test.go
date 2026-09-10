package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"go.uber.org/zap"
)

func TestListToolsSerializesResolvedDisplayDescriptions(t *testing.T) {
	handler := NewHandler(zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	recorder := httptest.NewRecorder()

	handler.ListTools(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	var got []agent.ToolMeta
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
	require.Equal(t, agent.GetAvailableTools(req.Context()), got)

	byName := make(map[string]string, len(got))
	for _, tool := range got {
		byName[tool.Name] = tool.Description
	}
	require.Equal(t, "Update this personality's working notes, which persist across conversations using the same personality.", byName["update_scratchpad"])
	require.NotEmpty(t, byName["create_agent_job"])
	require.NotEmpty(t, byName["find_context"])
}
