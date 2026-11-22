package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A connector whose address the dial guard refuses (one saved before save-time validation) is
// marked invalid, not retried every turn as a transient refresh error.
func TestDiscoveryFailureStatus_NetguardRefusalIsPermanent(t *testing.T) {
	active := &models.MCPServer{Status: models.MCPServerStatusActive}
	for _, msg := range []string{
		`Post "https://internal.example/mcp": dial tcp 10.0.0.5:443: refusing to connect to non-public address 10.0.0.5`,
		`Post "https://a.example/mcp": refusing redirect from https://a.example to another origin`,
	} {
		require.Equal(t, models.MCPServerStatusInvalid, discoveryFailureStatus(active, msg), msg)
	}
	require.Equal(t, models.MCPServerStatusRefreshError, discoveryFailureStatus(active, "dial tcp: i/o timeout"))
}
