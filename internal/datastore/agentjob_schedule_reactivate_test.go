package datastore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent/agentjob"
)

func TestShouldReactivateAgentJobAfterScheduleSave(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	next := &now

	require.False(t, shouldReactivateAgentJobAfterScheduleSave(agentjob.StatusActive, next))
	require.False(t, shouldReactivateAgentJobAfterScheduleSave(agentjob.StatusPaused, next))
	require.False(t, shouldReactivateAgentJobAfterScheduleSave(agentjob.StatusComplete, nil))
	require.False(t, shouldReactivateAgentJobAfterScheduleSave(agentjob.StatusFailed, nil))

	require.True(t, shouldReactivateAgentJobAfterScheduleSave(agentjob.StatusComplete, next))
	require.True(t, shouldReactivateAgentJobAfterScheduleSave(agentjob.StatusFailed, next))
}
