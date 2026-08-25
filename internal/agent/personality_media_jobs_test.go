package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestErrPersonalityMediaJobActive_Error(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	err := &ErrPersonalityMediaJobActive{
		Job: &models.Job{ID: jobID},
	}
	require.Contains(t, err.Error(), jobID.String())
}
