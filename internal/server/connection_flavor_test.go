package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/providerconnections"
)

// Each connection flavor maps to the agent's by name; an unknown one is an
// error rather than a guess at which API to call.
func TestAgentFlavor(t *testing.T) {
	got, err := agentFlavor(providerconnections.FlavorOpenAI)
	require.NoError(t, err)
	require.Equal(t, agent.ConnectionFlavorOpenAI, got)

	got, err = agentFlavor(providerconnections.FlavorAnthropic)
	require.NoError(t, err)
	require.Equal(t, agent.ConnectionFlavorAnthropic, got)

	_, err = agentFlavor(providerconnections.Flavor("gemini-native"))
	require.ErrorContains(t, err, "unknown flavor")
}
