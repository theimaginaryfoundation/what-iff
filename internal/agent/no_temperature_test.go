package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"go.uber.org/zap"
)

// capturingResponsesServer answers the Responses API with body and records the JSON of every
// request it receives, so tests can assert on the wire params a call site built.
func capturingResponsesServer(t *testing.T, body string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var reqs []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		reqs = append(reqs, req)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), reqs...)
	}
}

// requireNoTemperature asserts every captured request left sampling at the provider default.
// Requests never set temperature: models increasingly reject it (gpt-5.6-luna 400s on any
// value), so sending it turns a model upgrade into a silent outage of that call site.
func requireNoTemperature(t *testing.T, reqs []map[string]any) {
	t.Helper()
	require.NotEmpty(t, reqs, "expected at least one Responses API call")
	for _, req := range reqs {
		require.Equal(t, testUtilityModel, req["model"])
		require.NotContains(t, req, "temperature")
		require.NotContains(t, req, "top_p")
	}
}

func expectOneExpressionSlot(mock sqlmock.Sqlmock, key string) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(personalityExpressionRows(key))
	mock.ExpectCommit()
}

func TestPickGenerationExpression_StandaloneOmitsTemperature(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	expectOneExpressionSlot(mock, "happy")

	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", `{\"expression_key\":\"happy\",\"reasoning\":\"Warm tone.\"}`))
	a := &Agent{logger: zap.NewNop(), ds: ds, OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	id, _, err := a.PickGenerationExpression(withTestUtilityModels(context.Background()), uuid.New(), uuid.New(), nil, "hi", "hello there")
	require.NoError(t, err)
	require.NotNil(t, id, "the call must succeed, not be dropped as a 400")
	requireNoTemperature(t, reqs())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPickGenerationExpression_ForkedContextOmitsTemperature(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()
	expectOneExpressionSlot(mock, "happy")

	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", `{\"expression_key\":\"happy\",\"reasoning\":\"Warm tone.\"}`))
	a := &Agent{logger: zap.NewNop(), ds: ds, OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	id, _, err := a.PickGenerationExpression(withTestUtilityModels(context.Background()), uuid.New(), uuid.New(), &provider.ModelContext{}, "hi", "hello there")
	require.NoError(t, err)
	require.NotNil(t, id)
	reqs0 := reqs()
	requireNoTemperature(t, reqs0)
	require.Contains(t, reqs0[0], "text", "structured output format is still sent")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInferExpressionGridLikeness_OmitsTemperature(t *testing.T) {
	t.Parallel()
	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", "A tall figure with silver hair."))
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	out, err := a.inferExpressionGridLikeness(withTestUtilityModels(context.Background()), "You are Aster.", nil, "")
	require.NoError(t, err)
	require.Equal(t, "A tall figure with silver hair.", out)
	requireNoTemperature(t, reqs())
}

func TestGenerateChatName_OmitsTemperature(t *testing.T) {
	t.Parallel()
	srv, reqs := capturingResponsesServer(t, responseTextJSONBody("resp_1", "Trip planning"))
	a := &Agent{logger: zap.NewNop(), OpenAIProvider: newHTTPTestOpenAIProvider(srv.URL)}
	got, err := a.generateChatName(withTestUtilityModels(context.Background()), "help me plan a trip")
	require.NoError(t, err)
	require.Equal(t, "Trip planning", got)
	requireNoTemperature(t, reqs())
}
