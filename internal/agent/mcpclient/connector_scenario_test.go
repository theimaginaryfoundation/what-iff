package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/tidwall/gjson"
)

// Connector scenarios use original advertised tool names, not generated full names.
// Arguments and expectations support {{run}}, {{id}} and {{marker}} in string values.
type connectorStep struct {
	Tool      string                     `json:"tool"`
	Arguments json.RawMessage            `json:"arguments"`
	Expect    map[string]json.RawMessage `json:"expect,omitempty"`
}

type connectorScenario struct {
	Identity       connectorStep `json:"identity"`
	Create         connectorStep `json:"create"`
	Read           connectorStep `json:"read"`
	Update         connectorStep `json:"update"`
	Reject         connectorStep `json:"reject"`
	Cleanup        connectorStep `json:"cleanup"`
	IDPath         string        `json:"id_path"`
	OwnershipPath  string        `json:"ownership_path"`
	OwnershipValue string        `json:"ownership_value"`
	MissingError   string        `json:"missing_error"`
}

type connectorReceipt struct {
	Phase        string `json:"phase"`
	Tool         string `json:"tool"`
	InputHash    string `json:"input_sha256"`
	OutputHash   string `json:"output_sha256"`
	IsError      bool   `json:"is_error"`
	RequestError bool   `json:"request_error,omitempty"`
}

type connectorReport struct {
	RunID     string             `json:"run_id"`
	CreatedID string             `json:"created_id,omitempty"`
	Receipts  []connectorReceipt `json:"receipts"`
	Cleanup   string             `json:"cleanup"`
}

func renderConnectorJSON(raw json.RawMessage, vars map[string]string) json.RawMessage {
	var pairs []string
	for _, key := range []string{"run", "id", "marker"} {
		encoded, _ := json.Marshal(vars[key]) // Strings always marshal successfully.
		pairs = append(pairs, "{{"+key+"}}", string(encoded[1:len(encoded)-1]))
	}
	return json.RawMessage(strings.NewReplacer(pairs...).Replace(string(raw)))
}

func connectorJSON(result CallToolResult) json.RawMessage {
	if len(result.StructuredContent) > 0 && string(result.StructuredContent) != "null" {
		return result.StructuredContent
	}
	return json.RawMessage(result.Output)
}

func matchConnectorStep(step connectorStep, result CallToolResult, vars map[string]string) error {
	if len(step.Expect) == 0 {
		return nil
	}
	raw := connectorJSON(result)
	if !json.Valid(raw) {
		return fmt.Errorf("expected a JSON tool result")
	}
	for path, expected := range step.Expect {
		actual := gjson.GetBytes(raw, path)
		var actualValue, expectedValue any
		if !actual.Exists() || json.Unmarshal([]byte(actual.Raw), &actualValue) != nil ||
			json.Unmarshal(renderConnectorJSON(expected, vars), &expectedValue) != nil {
			return fmt.Errorf("missing or invalid assertion at %q", path)
		}
		actualJSON, _ := json.Marshal(actualValue)
		expectedJSON, _ := json.Marshal(expectedValue)
		if string(actualJSON) != string(expectedJSON) {
			return fmt.Errorf("read-back mismatch at %q", path)
		}
	}
	return nil
}

func connectorDigest(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func runConnectorScenario(ctx context.Context, client *Client, server *models.MCPServer, scenario connectorScenario) (report connectorReport, runErr error) {
	report.RunID = uuid.NewString()
	report.Cleanup = "not_needed"
	if err := scenario.validate(); err != nil {
		return report, err
	}
	sessions := make(SessionState)
	// run and the captured id stay fixed. Only marker changes for update/read
	// assertions; cleanup uses the run-specific ownership value, not that marker.
	// The call closure intentionally shares vars so it sends the latest values.
	vars := map[string]string{"run": report.RunID, "marker": "whatiff-smoke-" + report.RunID}
	call, err := prepareConnectorCalls(ctx, client, server, scenario, sessions, vars, &report)
	if err != nil {
		return report, err
	}
	identity, err := call(ctx, "identity", scenario.Identity, false)
	if err != nil {
		return report, err
	}
	if err := matchConnectorStep(scenario.Identity, identity, vars); err != nil {
		return report, fmt.Errorf("identity: %w", err)
	}
	created, err := call(ctx, "create", scenario.Create, false)
	id := gjson.GetBytes(connectorJSON(created), scenario.IDPath)
	if id.Type == gjson.String && id.Str != "" {
		vars["id"] = id.Str
		report.CreatedID = id.Str
		report.Cleanup = "pending"
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cleanupErr := cleanupConnectorScenario(cleanupCtx, scenario, vars, call)
			if cleanupErr != nil {
				report.Cleanup = "failed"
				runErr = errors.Join(runErr, cleanupErr)
			} else {
				report.Cleanup = "verified"
			}
		}()
	} else {
		report.Cleanup = "unknown_created_id"
		return report, fmt.Errorf("create: missing created document ID; reconcile this run before retrying")
	}
	if err != nil {
		return report, err
	}
	if err := matchConnectorStep(scenario.Create, created, vars); err != nil {
		return report, fmt.Errorf("create: %w", err)
	}
	return report, verifyConnectorPhases(ctx, scenario, vars, call)
}

func prepareConnectorCalls(ctx context.Context, client *Client, server *models.MCPServer, scenario connectorScenario, sessions SessionState, vars map[string]string, report *connectorReport) (connectorCall, error) {
	servers := []*models.MCPServer{server}
	discovered, err := client.DiscoverToolsWithSessionState(ctx, uuid.Nil, servers, sessions)
	if err != nil {
		return nil, connectorDiscoveryFailure(discovered, err)
	}
	toolNames := make(map[string]string)
	for _, tool := range discovered.Tools {
		toolNames[tool.Name] = tool.FullName
	}
	for _, step := range []connectorStep{scenario.Identity, scenario.Create, scenario.Read, scenario.Update, scenario.Reject, scenario.Cleanup} {
		if toolNames[step.Tool] == "" {
			return nil, fmt.Errorf("required tool %q is not advertised", step.Tool)
		}
	}
	return func(callCtx context.Context, phase string, step connectorStep, wantError bool) (CallToolResult, error) {
		args := renderConnectorJSON(step.Arguments, vars)
		result, err := client.CallToolByFullNameWithSessionStateDetailed(callCtx, servers, toolNames[step.Tool], args, sessions)
		report.Receipts = append(report.Receipts, connectorReceipt{
			Phase: phase, Tool: step.Tool, InputHash: connectorDigest(args),
			OutputHash: connectorDigest([]byte(result.Output)), IsError: result.IsError, RequestError: err != nil,
		})
		if err != nil {
			return result, connectorRequestFailure(phase, err)
		}
		if result.IsError != wantError {
			return result, fmt.Errorf("%s: expected isError=%t, received %t", phase, wantError, result.IsError)
		}
		return result, nil
	}, nil
}

func verifyConnectorPhases(ctx context.Context, scenario connectorScenario, vars map[string]string, call connectorCall) error {
	for _, phase := range []string{"read_created", "update", "read_updated", "reject", "read_after_reject"} {
		step := scenario.Read
		switch phase {
		case "update":
			// Change only the content assertion marker. Ownership remains tied to
			// the original run UUID even if the update fails or reads stay stale.
			vars["marker"] += "-updated"
			step = scenario.Update
		case "reject":
			step = scenario.Reject
		}
		result, err := call(ctx, phase, step, phase == "reject")
		if err != nil {
			return err
		}
		if err := matchConnectorStep(step, result, vars); err != nil {
			return fmt.Errorf("%s: %w", phase, err)
		}
	}
	return nil
}

type connectorCall func(context.Context, string, connectorStep, bool) (CallToolResult, error)

func cleanupConnectorScenario(ctx context.Context, scenario connectorScenario, vars map[string]string, call connectorCall) error {
	// Read the captured ID and prove it still bears this run's unique ownership marker.
	// A failed or mismatched read must never trigger a delete by name or a broad cleanup.
	// This call reads by id; only ownership_path/ownership_value are asserted here,
	// not Read.Expect or the mutable content marker used by the normal read phases.
	read, err := call(ctx, "cleanup_ownership", scenario.Read, false)
	if err != nil {
		return err
	}
	owner, _ := json.Marshal(scenario.OwnershipValue)
	if err := matchConnectorStep(connectorStep{Expect: map[string]json.RawMessage{scenario.OwnershipPath: owner}}, read, vars); err != nil {
		return fmt.Errorf("cleanup ownership check failed: %w", err)
	}
	if _, err := call(ctx, "cleanup", scenario.Cleanup, false); err != nil {
		return err
	}
	missing, err := call(ctx, "verify_deleted", scenario.Read, true)
	if err != nil {
		return err
	}
	if !strings.Contains(missing.Output, scenario.MissingError) {
		return fmt.Errorf("verify_deleted: error does not confirm the document is missing")
	}
	return nil
}
