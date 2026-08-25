package job

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type mockJobCanceller struct {
	calls int
	err   error
}

func (m *mockJobCanceller) CancelJob(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	m.calls++
	return m.err
}

func TestCancelJob_TerminalNoOp(t *testing.T) {
	userID := uuid.New()
	jobID := uuid.New()
	provider := NewMockJobProvider()
	provider.AddJob(models.Job{ID: jobID, UserID: userID, Status: models.JobStatusComplete})
	canceller := &mockJobCanceller{}
	h := NewHandlerWithCanceller(provider, canceller, zap.NewNop())

	req, _ := createRequest(http.MethodPost, "/job/"+jobID.String()+"/cancel", nil)
	req = setJobIDInURLVars(req, jobID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.CancelJob(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if canceller.calls != 0 {
		t.Fatalf("expected no cancel invocation for terminal job, got %d", canceller.calls)
	}
}

func TestCancelJob_InvokesCancellerForActiveJob(t *testing.T) {
	userID := uuid.New()
	jobID := uuid.New()
	provider := NewMockJobProvider()
	provider.AddJob(models.Job{ID: jobID, UserID: userID, Status: models.JobStatusProcessing})
	canceller := &mockJobCanceller{}
	h := NewHandlerWithCanceller(provider, canceller, zap.NewNop())

	req, _ := createRequest(http.MethodPost, "/job/"+jobID.String()+"/cancel", nil)
	req = setJobIDInURLVars(req, jobID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.CancelJob(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if canceller.calls != 1 {
		t.Fatalf("expected cancel invocation once, got %d", canceller.calls)
	}
}

func TestCancelJob_CancellerFailure(t *testing.T) {
	userID := uuid.New()
	jobID := uuid.New()
	provider := NewMockJobProvider()
	provider.AddJob(models.Job{ID: jobID, UserID: userID, Status: models.JobStatusProcessing})
	canceller := &mockJobCanceller{err: errors.New("boom")}
	h := NewHandlerWithCanceller(provider, canceller, zap.NewNop())

	req, _ := createRequest(http.MethodPost, "/job/"+jobID.String()+"/cancel", nil)
	req = setJobIDInURLVars(req, jobID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.CancelJob(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestCancelJob_ForbiddenFromCanceller(t *testing.T) {
	userID := uuid.New()
	jobID := uuid.New()
	provider := NewMockJobProvider()
	provider.AddJob(models.Job{ID: jobID, UserID: userID, Status: models.JobStatusProcessing})
	canceller := &mockJobCanceller{err: datastore.ErrUnauthorized}
	h := NewHandlerWithCanceller(provider, canceller, zap.NewNop())

	req, _ := createRequest(http.MethodPost, "/job/"+jobID.String()+"/cancel", nil)
	req = setJobIDInURLVars(req, jobID)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rr := httptest.NewRecorder()

	h.CancelJob(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}
