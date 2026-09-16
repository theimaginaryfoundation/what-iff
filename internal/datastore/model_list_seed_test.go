package datastore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func model(provider string) *models.Model {
	return &models.Model{ID: uuid.New(), Provider: provider}
}

func ids(ms ...*models.Model) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID.String())
	}
	return out
}

// An account's list is a show list, so an empty one must pass the catalog
// through — otherwise the window before seeding, or a failed seed, shows an
// empty picker.
func TestFilterToAddedModels_EmptyListPassesEverything(t *testing.T) {
	t.Parallel()

	a, b := model("openai"), model("anthropic")
	in := []*models.Model{a, b}

	require.Len(t, filterToAddedModels(in, &models.UserPreferences{}), 2)
	require.Len(t, filterToAddedModels(in, nil), 2)
}

// The distinction the previous version could not make: an account that has been
// offered models and holds an empty list removed them, and must not have the
// catalog handed back.
func TestFilterToAddedModels_EmptiedOnPurposeShowsNothing(t *testing.T) {
	t.Parallel()

	a, b := model("openai"), model("openai")
	got := filterToAddedModels([]*models.Model{a, b}, &models.UserPreferences{
		AddedModelIDs: []string{},
		SeenModelIDs:  ids(a, b),
	})

	require.Empty(t, got, "removing every model is a choice, not a gap to refill")
}

func TestFilterToAddedModels_NarrowsToTheList(t *testing.T) {
	t.Parallel()

	keep, drop := model("openai"), model("openai")
	got := filterToAddedModels([]*models.Model{keep, drop},
		&models.UserPreferences{AddedModelIDs: ids(keep), SeenModelIDs: ids(keep, drop)})

	require.Len(t, got, 1)
	require.Equal(t, keep.ID, got[0].ID)
}

// Hiding is a subtractive layer within the added list, so the two compose:
// a model must be on the list and not hidden to reach the picker.
func TestAddedAndHiddenCompose(t *testing.T) {
	t.Parallel()

	shown, hidden, notAdded := model("openai"), model("openai"), model("openai")
	prefs := &models.UserPreferences{
		AddedModelIDs:  ids(shown, hidden),
		SeenModelIDs:   ids(shown, hidden, notAdded),
		HiddenModelIDs: ids(hidden),
	}

	got := filterHidden(filterToAddedModels([]*models.Model{shown, hidden, notAdded}, prefs), prefs)
	require.Len(t, got, 1)
	require.Equal(t, shown.ID, got[0].ID)
}

func TestPlanProviderSeed_OnlyReachableProviders(t *testing.T) {
	t.Parallel()

	oa, an := model("openai"), model("anthropic")
	plan := planProviderSeed(&models.UserPreferences{},
		[]*models.Model{oa, an}, func(p string) bool { return p == "openai" })

	require.Equal(t, []string{oa.ID.String()}, plan.AddModelIDs,
		"an unreachable provider is left unoffered so it arrives whole once a key exists")
}

// The bug this replaced: recording providers froze an account's list at
// whatever the catalog held the day that provider was first seeded, so a model
// added later never reached anyone who already had that provider.
func TestPlanProviderSeed_NewCatalogModelReachesAnExistingAccount(t *testing.T) {
	t.Parallel()

	had, brandNew := model("openai"), model("openai")
	prefs := &models.UserPreferences{
		AddedModelIDs: []string{had.ID.String()},
		SeenModelIDs:  []string{had.ID.String()},
	}

	plan := planProviderSeed(prefs, []*models.Model{had, brandNew}, func(string) bool { return true })
	require.Equal(t, []string{brandNew.ID.String()}, plan.AddModelIDs,
		"a model the account has never been offered arrives even on a provider it already had")
}

// The other half: a removal is not a gap to be refilled.
func TestPlanProviderSeed_RemovedModelStaysRemoved(t *testing.T) {
	t.Parallel()

	removed := model("openai")
	prefs := &models.UserPreferences{
		AddedModelIDs: []string{},
		SeenModelIDs:  []string{removed.ID.String()},
	}

	plan := planProviderSeed(prefs, []*models.Model{removed}, func(string) bool { return true })
	require.True(t, plan.Empty(), "already offered once, and taken off the list on purpose")
}

// An account that removed everything chose that, and must not have its picker
// refilled on the next read.
func TestPlanProviderSeed_EmptiedListIsNotRefilled(t *testing.T) {
	t.Parallel()

	a, b := model("openai"), model("openai")
	prefs := &models.UserPreferences{
		AddedModelIDs: []string{},
		SeenModelIDs:  []string{a.ID.String(), b.ID.String()},
	}

	plan := planProviderSeed(prefs, []*models.Model{a, b}, func(string) bool { return true })
	require.True(t, plan.Empty())
}

// Seeding a second provider later must not disturb the first.
func TestPlanProviderSeed_LaterProviderJoinsWithoutTouchingTheFirst(t *testing.T) {
	t.Parallel()

	oa, an := model("openai"), model("anthropic")
	prefs := &models.UserPreferences{
		AddedModelIDs: []string{oa.ID.String()},
		SeenModelIDs:  []string{oa.ID.String()},
	}

	plan := planProviderSeed(prefs, []*models.Model{oa, an}, func(string) bool { return true })
	require.Equal(t, []string{an.ID.String()}, plan.AddModelIDs)
}
