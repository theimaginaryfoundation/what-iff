package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMCPServerJSONRedactsAuthToken(t *testing.T) {
	model := MCPServer{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		Name:           "stripe",
		Description:    "Stripe MCP",
		ServerURL:      "https://mcp.stripe.com",
		AuthToken:      "super-secret-token",
		DefaultEnabled: true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	raw, err := json.Marshal(model)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "super-secret-token")
	require.False(t, strings.Contains(string(raw), "auth_token"))
}
