package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// EnrichUserMessageWithRituals's cheapest reachable branch is the datastore
// error path (GetRitualsByIDs failing), which returns the original message
// unchanged alongside the error. Uses the internal (package agent) ds field
// directly, unlike rituals_test.go which is package agent_test.
func TestEnrichUserMessageWithRituals_DatastoreErrorReturnsOriginalMessage(t *testing.T) {
	t.Parallel()

	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()

	a := newTestAgent(ds)
	msg := &models.ChatMessage{Message: "hello", Rituals: []*models.Ritual{{ID: uuid.New()}}}

	// A non-empty ritual ID list forces GetRitualsByIDs past its own
	// empty-list early return and into the transaction/query path. No
	// sqlmock expectations are configured there, so it fails immediately.
	got, err := a.EnrichUserMessageWithRituals(context.Background(), uuid.New(), msg)
	require.Error(t, err)
	require.Equal(t, "hello", got)
	require.Equal(t, "hello", msg.Message)
}
