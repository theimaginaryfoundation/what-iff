package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
)

// requestRecorder captures the request Parse sends and fails the call, so the test stops
// before any response parsing.
type requestRecorder struct {
	req provider.UtilityRequest
}

func (r *requestRecorder) CallUtility(_ context.Context, req provider.UtilityRequest) (string, error) {
	r.req = req
	return "", errors.New("recorded")
}

// Parse is a quick task with structured output. Utility requests carry no sampling settings,
// so the call never sends a temperature (see provider.TestUtilityParams_NoSampling).
func TestParse_AsksForStructuredScheduleOnQuickRole(t *testing.T) {
	t.Parallel()
	rec := &requestRecorder{}
	_, err := Parse(context.Background(), rec, uuid.New(), "every day at 9am", "UTC", time.Now())
	require.Error(t, err)
	require.Equal(t, provider.UtilityRoleQuick, rec.req.Role)
	require.NotNil(t, rec.req.Schema)
	require.Equal(t, "AgentJobSchedule", rec.req.Schema.Name)
}
