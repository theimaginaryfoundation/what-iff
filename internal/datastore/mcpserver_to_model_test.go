package datastore

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
)

func TestToMCPServerModel_WithRitualEdges(t *testing.T) {
	d := &Datastore{}
	mcpID := uuid.New()
	rid1 := uuid.New()
	rid2 := uuid.New()
	now := time.Now().UTC()

	entSrv := &ent.MCPServer{
		ID:             mcpID,
		Name:           "test",
		Description:    "desc",
		ServerURL:      "https://example.com/mcp",
		DefaultEnabled: true,
		CreatedAt:      now,
		UpdatedAt:      now,
		Edges: ent.MCPServerEdges{
			Rituals: []*ent.Ritual{
				{ID: rid1},
				{ID: rid2},
			},
		},
	}

	m := d.toMCPServerModel(entSrv)
	require.NotNil(t, m)
	require.Len(t, m.RitualIDs, 2)
	require.Equal(t, rid1, m.RitualIDs[0])
	require.Equal(t, rid2, m.RitualIDs[1])
}
