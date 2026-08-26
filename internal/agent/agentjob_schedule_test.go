package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ParseAgentJobSchedule is a thin wrapper over schedule.Parse. The cheapest
// reachable branch is schedule.Parse's own empty-input guard, which fires
// before any provider call, so a zero-value Agent (nil OpenAIProvider) is
// sufficient here.
func TestParseAgentJobSchedule_EmptyInputReturnsError(t *testing.T) {
	t.Parallel()

	a := &Agent{}
	_, err := a.ParseAgentJobSchedule(context.Background(), uuid.New(), "   ", "UTC", time.Now())
	require.Error(t, err)
	require.ErrorContains(t, err, "schedule_input is required")
}
