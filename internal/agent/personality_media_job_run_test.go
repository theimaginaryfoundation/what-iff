package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// runPersonalityMediaJob's cheapest branch is the missing-user-in-context
// guard. It attempts to mark the job failed via the datastore before
// returning; no sqlmock expectations are configured, so that update also
// fails, exercising the "failed to mark failed" log path too. The function
// has no return value, so this test only asserts it does not panic and
// completes (i.e. both guard branches are reached without needing a real DB).
func TestRunPersonalityMediaJob_MissingUserInContextDoesNotPanic(t *testing.T) {
	t.Parallel()

	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()

	a := newTestAgent(ds)
	job := &models.Job{ID: uuid.New(), UserID: uuid.New()}

	// context.Background() carries no user ID, so CopyUserToIDContext fails
	// immediately inside runPersonalityMediaJob, before work() is ever called.
	a.runPersonalityMediaJob(context.Background(), job, func(context.Context) (uuid.UUID, error) {
		t.Fatal("work must not run when the user is missing from context")
		return uuid.Nil, nil
	})
}
