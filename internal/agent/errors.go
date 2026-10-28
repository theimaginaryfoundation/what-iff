package agent

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
)

// ErrQuotaExceeded indicates that the turn was rejected because a usage limit was reached.
var ErrQuotaExceeded = errors.New("quota exceeded")

// failureCode is the stable code recorded with a failed job or chat turn, so
// clients can branch on it rather than on the text, or "" when the failure has
// none.
func failureCode(err error) string {
	var notConfigured *providerkeys.NotConfiguredError
	switch {
	case errors.As(err, &notConfigured):
		return models.ErrCodeProviderNotConfigured
	case errors.Is(err, ErrQuotaExceeded):
		return models.ErrCodeQuotaExceeded
	}
	return ""
}

// recordJobFailureCode records failureCode(err) on a job already marked failed
// through UpdateJobStatus. A failure without a code writes nothing. It is best
// effort: the job is failed either way, and the text is already recorded.
func (a *Agent) recordJobFailureCode(ctx context.Context, userID, jobID uuid.UUID, err error) {
	code := failureCode(err)
	if code == "" {
		return
	}
	if serr := a.ds.SetJobErrorCode(ctx, userID, jobID, code); serr != nil {
		a.logger.Warn("failed to record job failure code",
			zap.String("job_id", jobID.String()),
			zap.String("code", code),
			zap.Error(serr))
	}
}
