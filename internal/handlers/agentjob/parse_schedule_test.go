package agentjob

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type failingScheduleParser struct{ err error }

func (p failingScheduleParser) ParseAgentJobSchedule(context.Context, uuid.UUID, string, string, time.Time) (*models.AgentJobSchedulePreview, error) {
	return nil, p.err
}

// Parsing that can't run because the simple role has no model is the account's to fix, so it is
// answered with its code and the role rather than as input that couldn't be read.
func TestParseSchedule_UtilityRoleUnavailableIsAConflict(t *testing.T) {
	t.Parallel()
	err := &models.UtilityRoleUnavailableError{Role: models.UtilityRoleSimple, Err: models.ErrUtilityRoleNotSet}
	h := NewHandler(nil, failingScheduleParser{err: err}, nil, zap.NewNop())
	req := httptest.NewRequest(http.MethodPost, "/agent-job/schedule/parse", strings.NewReader(`{"schedule_input":"every day at 9"}`))
	req = req.WithContext(apicontext.WithUserID(req.Context(), uuid.New()))
	rec := httptest.NewRecorder()

	h.ParseSchedule(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), models.ErrCodeUtilityModelNotSet)
	require.Contains(t, rec.Body.String(), `"role":"simple"`)
}
