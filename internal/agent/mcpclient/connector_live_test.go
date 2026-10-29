//go:build mcp_live

package mcpclient

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func liveConnector(t *testing.T) (*Client, *models.MCPServer) {
	t.Helper()
	endpoint := os.Getenv("MCP_SMOKE_URL")
	if endpoint == "" {
		t.Skip("set MCP_SMOKE_URL to an explicitly selected connector")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		t.Fatal("MCP_SMOKE_URL must be an HTTP(S) endpoint")
	}
	return New(nil, nil), &models.MCPServer{
		ID: uuid.New(), Name: "live-smoke", ServerURL: endpoint,
		AuthMode: models.MCPServerAuthModeHeader, AuthToken: os.Getenv("MCP_SMOKE_AUTH_HEADER"),
		Status: models.MCPServerStatusActive,
	}
}

func TestLiveConnectorInventory(t *testing.T) {
	client, server := liveConnector(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tools, err := client.listToolsRPC(ctx, server, make(SessionState))
	if err != nil {
		t.Fatal(connectorRequestFailure("inventory", err))
	}
	encoded, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Advertised tool names and complete input schemas (review before sharing):\n%s", encoded)
}

func TestLiveConnectorRoundTrip(t *testing.T) {
	if os.Getenv("MCP_SMOKE_ALLOW_WRITE") != "1" {
		t.Skip("set MCP_SMOKE_ALLOW_WRITE=1 to permit the configured disposable-document lifecycle")
	}
	planPath := os.Getenv("MCP_SMOKE_PLAN")
	if planPath == "" {
		t.Fatal("MCP_SMOKE_PLAN is required for live writes")
	}
	scenario, err := loadConnectorScenario(planPath)
	if err != nil {
		t.Fatal(err)
	}
	client, server := liveConnector(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	report, runErr := runConnectorScenario(ctx, client, server, scenario)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Local receipt (no endpoint, auth header, arguments or result bodies):\n%s", encoded)
	if runErr != nil {
		t.Fatal(runErr)
	}
}
