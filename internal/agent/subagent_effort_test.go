package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// bodyRecorder is an httptest server that records the JSON request bodies it receives.
func bodyRecorder(t *testing.T, respond string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

// A sub-agent inherits the turn's selected effort, mapped onto its own model, and reports it for
// metering (ADR 0x026) only when it is above the model's native level: Qwen 3.7 and GLM reason at
// high unasked, Opus 5.5 at medium.
func TestCallSubagentModel_InheritsReasoningEffort(t *testing.T) {
	t.Parallel()
	ctx := withSubagentReasoningEffort(context.Background(), models.ReasoningEffortHigh)

	t.Run("qwen", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		expectModelRow(mock, "qwen3.7-plus", "qwen", true)
		rec := newChatCompletionsRecorder(t, http.StatusOK, chatCompletionTextBody("qwen thought"))
		a := newSubagentChatCompletionsAgent(rec.srv.URL)
		a.ds = ds

		out, err := a.callSubagentModel(ctx, uuid.New(), "qwen3.7-plus", buildSubagentModelContext("", "", "hi"), nil, false)
		require.NoError(t, err)
		assert.Equal(t, models.ReasoningEffort(""), out.ReasoningEffort, "high is qwen3.7's native level")
		assert.Equal(t, true, rec.bodies[0]["enable_thinking"])
		assert.Equal(t, float64(12288), rec.bodies[0]["thinking_budget"])
	})

	t.Run("anthropic", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		expectModelRow(mock, "claude-opus-5-5", "anthropic", true)
		srv, bodies := bodyRecorder(t, claudeMessageTextJSONBody("msg_a", "claude thought"))
		a := newTestAgent(ds)
		a.ClaudeProvider = newHTTPTestClaudeProvider(srv.URL)

		out, err := a.callSubagentModel(ctx, uuid.New(), "claude-opus-5-5", buildSubagentModelContext("", "", "hi"), nil, false)
		require.NoError(t, err)
		assert.Equal(t, models.ReasoningEffortHigh, out.ReasoningEffort)
		assert.Equal(t, "adaptive", bodies()[0]["thinking"].(map[string]any)["type"])
		assert.Equal(t, "high", bodies()[0]["output_config"].(map[string]any)["effort"])
	})

	t.Run("zai", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		expectModelRow(mock, "glm-5.2", "zai", true)
		srv, bodies := bodyRecorder(t, claudeMessageTextJSONBody("msg_z", "glm thought"))
		a := newTestAgent(ds)
		a.ZAIProvider = newHTTPTestClaudeProvider(srv.URL)

		out, err := a.callSubagentModel(ctx, uuid.New(), "glm-5.2", buildSubagentModelContext("", "", "hi"), nil, false)
		require.NoError(t, err)
		assert.Equal(t, models.ReasoningEffort(""), out.ReasoningEffort, "GLM reasons at its deepest level unasked")
		assert.Equal(t, "max", bodies()[0]["output_config"].(map[string]any)["effort"])
	})

	t.Run("no selection sends nothing", func(t *testing.T) {
		t.Parallel()
		ds, mock, cleanup := newTestDatastore(t)
		defer cleanup()
		expectModelRow(mock, "glm-5.2", "zai", true)
		srv, bodies := bodyRecorder(t, claudeMessageTextJSONBody("msg_z", "glm"))
		a := newTestAgent(ds)
		a.ZAIProvider = newHTTPTestClaudeProvider(srv.URL)

		out, err := a.callSubagentModel(context.Background(), uuid.New(), "glm-5.2", buildSubagentModelContext("", "", "hi"), nil, false)
		require.NoError(t, err)
		assert.Empty(t, out.ReasoningEffort)
		_, hasOutputConfig := bodies()[0]["output_config"]
		assert.False(t, hasOutputConfig, "a sub-agent with no inherited effort keeps today's request")
	})
}
