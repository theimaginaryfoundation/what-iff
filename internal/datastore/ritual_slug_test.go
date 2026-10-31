package datastore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// newRitualSlugTestDatastore uses the generated schema: CreateRitual loads the mcp_servers edge.
func newRitualSlugTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createGeneratedTestSchema)
}

func boolPtr(b bool) *bool { return &b }

func TestCreateRitual_SlugAndSkillFields(t *testing.T) {
	ds, cleanup := newRitualSlugTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)

	// No slug given: the datastore derives one. Defaults for the pointer fields are applied.
	created, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "PDF Forms", Description: "d", Content: "c"})
	require.NoError(t, err)
	require.Equal(t, "pdf-forms", created.Slug)
	require.Equal(t, "", created.LicenseText())
	require.Equal(t, map[string]string{}, created.MetadataMap())
	require.Equal(t, []string{}, created.AllowedToolList())
	require.True(t, created.IsModelInvocable())
	require.True(t, created.IsUserInvocable())

	// The same slug for the same owner is refused.
	_, err = ds.CreateRitual(ctx, userID, models.Ritual{Slug: "pdf-forms", Name: "Other", Description: "d", Content: "c"})
	require.ErrorIs(t, err, ErrRitualSlugTaken)

	// Another owner may use it.
	other := createMoodTestUser(t, ds)
	_, err = ds.CreateRitual(ctx, other, models.Ritual{Slug: "pdf-forms", Name: "Other", Description: "d", Content: "c"})
	require.NoError(t, err)

	// Every skill field round-trips.
	meta := map[string]string{"author": "me"}
	tools := []string{"web_search"}
	full, err := ds.CreateRitual(ctx, userID, models.Ritual{
		Slug: "notes", Name: "Notes", Description: "d", Content: "c", Hotkeys: "Ctrl+1",
		License: strPtr("MIT"), Compatibility: strPtr("needs python"), Metadata: &meta, AllowedTools: &tools,
		DisableModelInvocation: boolPtr(true), UserInvocable: boolPtr(false),
	})
	require.NoError(t, err)
	got, err := ds.GetRitualBySlug(ctx, userID, "notes")
	require.NoError(t, err)
	require.Equal(t, full.ID, got.ID)
	require.Equal(t, "MIT", got.LicenseText())
	require.Equal(t, "needs python", got.CompatibilityText())
	require.Equal(t, meta, got.MetadataMap())
	require.Equal(t, tools, got.AllowedToolList())
	require.False(t, got.IsModelInvocable())
	require.False(t, got.IsUserInvocable())

	_, err = ds.GetRitualBySlug(ctx, userID, "missing")
	require.ErrorIs(t, err, ErrRitualNotFound)

	taken, err := ds.RitualSlugTaken(ctx, userID, "notes", uuid.Nil)
	require.NoError(t, err)
	require.True(t, taken)
	taken, err = ds.RitualSlugTaken(ctx, userID, "notes", full.ID)
	require.NoError(t, err)
	require.False(t, taken, "the skill itself does not count")
}

func TestUpdateRitual_SlugAndPointerFieldsFollowOmitRule(t *testing.T) {
	ds, cleanup := newRitualSlugTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)

	a, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "A", Description: "d", Content: "c", License: strPtr("MIT"), UserInvocable: boolPtr(false)})
	require.NoError(t, err)
	b, err := ds.CreateRitual(ctx, userID, models.Ritual{Name: "B", Description: "d", Content: "c"})
	require.NoError(t, err)

	// Omitted slug and pointer fields keep their stored values.
	updated, err := ds.UpdateRitual(ctx, userID, models.Ritual{ID: a.ID, Name: "A renamed", Description: "d2", Content: "c2"})
	require.NoError(t, err)
	require.Equal(t, "a", updated.Slug)
	require.Equal(t, "MIT", updated.LicenseText())
	require.False(t, updated.IsUserInvocable())

	// A new slug is applied; one another skill holds is refused.
	updated, err = ds.UpdateRitual(ctx, userID, models.Ritual{ID: a.ID, Slug: "alpha", Name: "A", Description: "d", Content: "c", License: strPtr("")})
	require.NoError(t, err)
	require.Equal(t, "alpha", updated.Slug)
	require.Equal(t, "", updated.LicenseText())
	_, err = ds.UpdateRitual(ctx, userID, models.Ritual{ID: b.ID, Slug: "alpha", Name: "B", Description: "d", Content: "c"})
	require.ErrorIs(t, err, ErrRitualSlugTaken)
	// Keeping its own slug is fine.
	_, err = ds.UpdateRitual(ctx, userID, models.Ritual{ID: b.ID, Slug: "b", Name: "B", Description: "d", Content: "c"})
	require.NoError(t, err)
}

func TestGetAvailableRituals_HidesNonUserInvocableSkills(t *testing.T) {
	ds, cleanup := newRitualSlugTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createMoodTestUser(t, ds)

	chat, err := ds.dbClient.Chat.Create().SetName("c").SetOwnerID(userID).Save(ctx)
	require.NoError(t, err)

	_, err = ds.CreateRitual(ctx, userID, models.Ritual{Name: "Shown", Description: "d", Content: "c"})
	require.NoError(t, err)
	_, err = ds.CreateRitual(ctx, userID, models.Ritual{Name: "Hidden", Description: "d", Content: "c", UserInvocable: boolPtr(false)})
	require.NoError(t, err)

	page, err := ds.GetAvailableRituals(ctx, userID, chat.ID, 1, 10, models.RitualFilters{})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	require.Equal(t, "shown", page.Results[0].(*models.Ritual).Slug)

	// The owner's full list still has both, and can be filtered by slug.
	all, err := ds.ListRituals(ctx, userID, 1, 10, models.RitualFilters{})
	require.NoError(t, err)
	require.Equal(t, 2, all.TotalCount)
	bySlug, err := ds.ListRituals(ctx, userID, 1, 10, models.RitualFilters{Slug: strPtr("hidden")})
	require.NoError(t, err)
	require.Equal(t, 1, bySlug.TotalCount)
}
