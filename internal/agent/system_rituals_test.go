package agent

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestListSystemRituals(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	rituals := ListSystemRituals(now)

	require.Len(t, rituals, 1)
	require.Equal(t, SystemRitualIDImageGenerate, rituals[0].ID)
	require.Equal(t, "Generate image", rituals[0].Name)
	require.Equal(t, "Generate an image from the conversation.", rituals[0].Description)
	require.Equal(t, now, rituals[0].CreatedAt)
	require.Equal(t, now, rituals[0].UpdatedAt)
}

func TestListSystemRituals_allHaveUniqueIDs(t *testing.T) {
	rituals := ListSystemRituals(time.Now().UTC())
	seen := make(map[uuid.UUID]struct{})
	for _, r := range rituals {
		require.NotContains(t, seen, r.ID, "duplicate system ritual ID: %s", r.ID)
		seen[r.ID] = struct{}{}
	}
}

// TestRegistryConsistency ensures ListSystemRituals and IsSystemRitual stay in sync.
func TestRegistryConsistency(t *testing.T) {
	rituals := ListSystemRituals(time.Now().UTC())
	for _, r := range rituals {
		require.True(t, IsSystemRitual(r.ID), "ritual %s from ListSystemRituals must be recognized by IsSystemRitual", r.ID)
	}
}

// TestSystemRitualIDImageGenerate_stableUUID ensures the image generate ritual ID never changes.
// Changing this would break existing DB bindings and frontend references.
func TestSystemRitualIDImageGenerate_stableUUID(t *testing.T) {
	expected := "00000000-0000-0000-0000-000000000101"
	require.Equal(t, expected, SystemRitualIDImageGenerate.String(), "SystemRitualIDImageGenerate UUID must remain stable")
}

func TestIsSystemRitual(t *testing.T) {
	require.True(t, IsSystemRitual(SystemRitualIDImageGenerate))
	require.False(t, IsSystemRitual(uuid.New()))
}

func TestSplitRituals(t *testing.T) {
	dbID := uuid.New()

	dbR := &models.Ritual{ID: dbID}
	sysR := &models.Ritual{ID: SystemRitualIDImageGenerate}

	t.Run("nil input", func(t *testing.T) {
		db, sys := SplitRituals(nil)
		require.Nil(t, db)
		require.Nil(t, sys)
	})

	t.Run("filters nil entries", func(t *testing.T) {
		db, sys := SplitRituals([]*models.Ritual{nil, sysR, nil, dbR})
		require.Len(t, db, 1)
		require.Equal(t, dbID, db[0].ID)
		require.Len(t, sys, 1)
		require.Equal(t, SystemRitualIDImageGenerate, sys[0].ID)
	})

	t.Run("preserves order within partitions", func(t *testing.T) {
		db2 := &models.Ritual{ID: uuid.New()}
		sys2 := &models.Ritual{ID: SystemRitualIDImageGenerate}
		db, sys := SplitRituals([]*models.Ritual{dbR, sysR, db2, sys2})

		require.Len(t, db, 2)
		require.Equal(t, dbR.ID, db[0].ID)
		require.Equal(t, db2.ID, db[1].ID)

		require.Len(t, sys, 2)
		require.Equal(t, sysR.ID, sys[0].ID)
		require.Equal(t, sys2.ID, sys[1].ID)
	})
}
