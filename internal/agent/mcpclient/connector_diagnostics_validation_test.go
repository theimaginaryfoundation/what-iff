package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestConnectorPlanDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{"syntax", `{"private-diagnostic-marker": !}`, "syntax error at byte"},
		{"unknown field", `{"private-diagnostic-marker":"private-value"}`, "unknown field"},
		{"type", `{"identity":123456789}`, "expected mcpclient.connectorStep at byte"},
		{"multiple values", `{} {}`, "exactly one JSON object"},
		{"empty", ``, "empty input"},
		{"truncated", `{"identity":`, "unexpected end of input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private-plan-path.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.input), 0600))
			_, err := loadConnectorScenario(path)
			require.ErrorContains(t, err, tt.want)
			for _, private := range []string{path, "private-diagnostic-marker", "private-value", "123456789"} {
				require.NotContains(t, err.Error(), private)
			}
			if tt.name == "syntax" {
				var syntax *json.SyntaxError
				require.ErrorAs(t, err, &syntax, "safe rendering must retain the original typed cause")
			}
		})
	}
	missingPath := filepath.Join(t.TempDir(), "private-missing-path.json")
	_, err := loadConnectorScenario(missingPath)
	require.ErrorContains(t, err, "file not found")
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NotContains(t, err.Error(), missingPath)
}

func TestConnectorDiscoveryDiagnostics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "private-diagnostic-marker", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	server := &models.MCPServer{ID: uuid.New(), ServerURL: srv.URL, Status: models.MCPServerStatusActive}
	report, err := runConnectorScenario(context.Background(), New(nil, nil), server, testConnectorScenario())
	require.EqualError(t, err, "discovery failed (HTTP 401)")
	require.Error(t, errors.Unwrap(err), "the original discovery error remains available")
	require.NotContains(t, err.Error(), srv.URL)
	require.NotContains(t, err.Error(), "private-diagnostic-marker")
	require.Empty(t, report.Receipts, "discovery failure cannot start document operations")
}

func TestConnectorRequestDiagnosticsRetainCause(t *testing.T) {
	for _, tt := range []struct {
		cause error
		want  string
	}{
		{context.DeadlineExceeded, "request timed out"},
		{context.Canceled, "request cancelled"},
		{&url.Error{Op: "Post", URL: "https://example.invalid/?private-diagnostic-marker", Err: errors.New("private-diagnostic-marker")}, "network request failed"},
		{fmt.Errorf("mcp server returned status 503 for tools/call: private-diagnostic-marker"), "HTTP 503"},
		{fmt.Errorf("mcp rpc tools/call failed: private-diagnostic-marker"), "JSON-RPC error"},
	} {
		err := connectorRequestFailure("read", tt.cause)
		require.ErrorContains(t, err, tt.want)
		require.ErrorIs(t, err, tt.cause)
		require.NotContains(t, err.Error(), "private-diagnostic-marker")
	}
}
