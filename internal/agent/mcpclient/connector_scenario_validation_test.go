package mcpclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConnectorScenario_RejectsIncompletePlansBeforeConnecting(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*connectorScenario)
	}{
		{"no identity assertion", func(s *connectorScenario) { s.Identity.Expect = nil }},
		{"no ownership proof", func(s *connectorScenario) { s.OwnershipValue = "shared-name" }},
		{"no read marker assertion", func(s *connectorScenario) { delete(s.Read.Expect, "value") }},
		{"cleanup by fixed ID", func(s *connectorScenario) { s.Cleanup.Arguments = json.RawMessage(`{"id":"unrelated"}`) }},
		{"invalid arguments", func(s *connectorScenario) { s.Create.Arguments = json.RawMessage(`[]`) }},
		{"no missing-document error", func(s *connectorScenario) { s.MissingError = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scenario := testConnectorScenario()
			tt.change(&scenario)
			report, err := runConnectorScenario(context.Background(), nil, nil, scenario)
			require.Error(t, err)
			require.Empty(t, report.Receipts)
			require.Equal(t, "not_needed", report.Cleanup)
		})
	}
}

func TestConnectorScenario_JSONSafeVariables(t *testing.T) {
	vars := map[string]string{"id": "quote\"\\\n{{marker}}", "marker": "unique-marker", "run": "run-1"}
	rendered := renderConnectorJSON(json.RawMessage(`{"id":"{{id}}","value":"<p>{{marker}}</p>"}`), vars)
	require.True(t, json.Valid(rendered))
	require.Equal(t, vars["id"], gjson.GetBytes(rendered, "id").String(), "replacement must escape JSON and must not recursively expand an ID")
	require.Equal(t, "<p>unique-marker</p>", gjson.GetBytes(rendered, "value").String())
}

func TestConnectorScenario_ReceiptOmitsArgumentsAndBodies(t *testing.T) {
	server := newConnectorScenarioServer(t, "", false, nil)
	report, err := runConnectorScenario(context.Background(), New(nil, nil), server, testConnectorScenario())
	require.NoError(t, err)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	for _, receipt := range report.Receipts {
		require.Len(t, receipt.InputHash, 64)
		require.Len(t, receipt.OutputHash, 64)
	}
	for _, bodyValue := range []string{"test-world", "whatiff-smoke-", "unsupported value", server.ServerURL} {
		require.False(t, strings.Contains(string(encoded), bodyValue))
	}
}
