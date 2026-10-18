package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func testConnectorScenario() connectorScenario {
	step := func(tool, args string, expect map[string]json.RawMessage) connectorStep {
		return connectorStep{Tool: tool, Arguments: json.RawMessage(args), Expect: expect}
	}
	return connectorScenario{
		Identity: step("target", `{}`, map[string]json.RawMessage{"world": json.RawMessage(`"test-world"`)}),
		Create:   step("create-document", `{"owner":"{{run}}","value":"{{marker}}"}`, nil),
		Read: step("read", `{"id":"{{id}}"}`, map[string]json.RawMessage{
			"id": json.RawMessage(`"{{id}}"`), "owner": json.RawMessage(`"{{run}}"`),
			"value": json.RawMessage(`"{{marker}}"`), "count": json.RawMessage(`0`), "enabled": json.RawMessage(`false`),
		}),
		Update:  step("update", `{"id":"{{id}}","value":"{{marker}}"}`, nil),
		Reject:  step("reject", `{"id":"{{id}}"}`, nil),
		Cleanup: step("delete", `{"id":"{{id}}"}`, nil),
		IDPath:  "id", OwnershipPath: "owner", OwnershipValue: "{{run}}", MissingError: "document not found",
	}
}

func TestConnectorRoundTrip(t *testing.T) {
	for _, mode := range []string{"JSON", "SSE", "legacy_text", "plain_ack"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel() // Each subtest owns its connector, document and session state.
			server := newConnectorScenarioServer(t, mode, mode == "SSE", nil)
			report, err := runConnectorScenario(context.Background(), New(nil, nil), server, testConnectorScenario())
			require.NoError(t, err)
			require.Equal(t, "doc-1", report.CreatedID)
			require.Equal(t, "verified", report.Cleanup)
			require.Len(t, report.Receipts, 10)
			require.Equal(t, "read_created", report.Receipts[2].Phase)
			require.NotEqual(t, report.Receipts[2].OutputHash, report.Receipts[4].OutputHash)
		})
	}
}

func TestConnectorRoundTrip_Failures(t *testing.T) {
	for _, tt := range []struct{ fault, wantError, cleanup string }{
		{"wrong_world", "identity: read-back mismatch", "not_needed"},
		{"missing_tool", "not advertised", "not_needed"},
		{"missing_id", "missing created document ID", "unknown_created_id"},
		{"stale_update", "read_updated: read-back mismatch", "verified"},
		{"unflagged_error", "reject: expected isError=true", "verified"},
		{"rejected_write", "read_after_reject: read-back mismatch", "verified"},
		{"ownership_changed", "cleanup ownership check failed", "failed"},
		{"cleanup_noop", "verify_deleted: expected isError=true", "failed"},
		{"missing_masked_by_permission", "error does not confirm the document is missing", "failed"},
		{"protocol_failure", "reject: transport or protocol failure", "verified"},
		{"non_json_legacy", "read_created: expected a JSON tool result", "failed"},
	} {
		t.Run(tt.fault, func(t *testing.T) {
			server := newConnectorScenarioServer(t, tt.fault, false, nil)
			report, err := runConnectorScenario(context.Background(), New(nil, nil), server, testConnectorScenario())
			require.ErrorContains(t, err, tt.wantError)
			require.Equal(t, tt.cleanup, report.Cleanup)
			var phases []string
			for _, receipt := range report.Receipts {
				phases = append(phases, receipt.Phase)
			}
			switch tt.fault {
			case "missing_tool":
				require.Empty(t, phases, "capability preflight must fail before calling any tool")
			case "wrong_world":
				require.Equal(t, []string{"identity"}, phases)
			case "ownership_changed":
				require.Equal(t, []string{"identity", "create", "read_created", "update", "read_updated", "cleanup_ownership"}, phases)
			case "non_json_legacy":
				require.Equal(t, []string{"identity", "create", "read_created", "cleanup_ownership"}, phases)
			default:
				require.NotEmpty(t, phases)
			}
		})
	}
}

func TestConnectorRoundTrip_CleanupAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := newConnectorScenarioServer(t, "", false, cancel)
	report, err := runConnectorScenario(ctx, New(nil, nil), server, testConnectorScenario())
	require.Error(t, err)
	require.Equal(t, "verified", report.Cleanup, "cleanup must use its own bounded context")
}

func newConnectorScenarioServer(t *testing.T, fault string, sse bool, cancel context.CancelFunc) *models.MCPServer {
	t.Helper()
	// State and its lock belong to this server instance, not the test package.
	// Independent subtests may run in parallel; requests within a fixture are locked.
	var mu sync.Mutex
	var document map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		defer r.Body.Close()
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			w.Header().Set(mcpSessionHeader, "scenario-session")
			result["serverInfo"] = map[string]any{"name": "fixture", "version": "1"}
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			var tools []map[string]any
			for _, name := range []string{"target", "create-document", "read", "update", "reject", "delete"} {
				if fault == "missing_tool" && name == "delete" {
					continue
				}
				tools = append(tools, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}})
			}
			result["tools"] = tools
		case "tools/call":
			if r.Header.Get(mcpSessionHeader) != "scenario-session" {
				http.Error(w, "missing session", http.StatusBadRequest)
				return
			}
			if fault == "protocol_failure" && req.Params.Name == "reject" {
				http.Error(w, "fixture failure", http.StatusServiceUnavailable)
				return
			}
			payload := map[string]any{}
			isError := false
			switch req.Params.Name {
			case "target":
				payload["world"] = "test-world"
				if fault == "wrong_world" {
					payload["world"] = "another-world"
				}
			case "create-document":
				document = map[string]any{"id": "doc-1", "owner": req.Params.Arguments["owner"], "value": req.Params.Arguments["value"], "count": 0, "enabled": false}
				payload = document
				if fault == "missing_id" {
					payload = map[string]any{"created": true}
				}
			case "read":
				if document == nil {
					isError = true
					payload["error"] = "document not found"
					if fault == "missing_masked_by_permission" {
						payload["error"] = "permission denied"
					}
				} else {
					payload = document
				}
			case "update":
				if fault != "stale_update" {
					document["value"] = req.Params.Arguments["value"]
				}
				if fault == "ownership_changed" {
					document["owner"] = "another-owner"
				}
				payload["updated"] = true
				if cancel != nil {
					cancel()
				}
			case "reject":
				isError = fault != "unflagged_error"
				payload["error"] = "unsupported value"
				if fault == "rejected_write" {
					document["value"] = "unexpected mutation"
				}
			case "delete":
				if fault != "cleanup_noop" {
					document = nil
				}
				payload["deleted"] = true
			}
			result = map[string]any{"isError": isError, "structuredContent": payload, "content": []map[string]any{{"type": "text", "text": "connector reply"}}}
			if fault == "legacy_text" {
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Errorf("encode legacy result: %v", err)
					return
				}
				delete(result, "structuredContent")
				result["content"] = []map[string]any{{"type": "text", "text": string(encoded)}}
			}
			if (fault == "non_json_legacy" && req.Params.Name == "read") ||
				(fault == "plain_ack" && (req.Params.Name == "update" || req.Params.Name == "delete")) {
				delete(result, "structuredContent")
				result["content"] = []map[string]any{{"type": "text", "text": "plain acknowledgement"}}
			}
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
			return
		}
		envelope := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			encoded, err := json.Marshal(envelope)
			if err != nil {
				t.Errorf("encode SSE: %v", err)
				return
			}
			_, err = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\ndata: %s\n\n", encoded)
			if err != nil {
				t.Errorf("write SSE: %v", err)
			}
		} else if err := json.NewEncoder(w).Encode(envelope); err != nil {
			t.Errorf("encode result: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return &models.MCPServer{ID: uuid.New(), Name: "fixture", ServerURL: srv.URL, Status: models.MCPServerStatusActive}
}
