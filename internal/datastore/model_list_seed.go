package datastore

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// seedProviderModels copies a provider's catalog into an account's own model
// list, once.
//
// An account's list is a show list, so without this everyone would start with
// an empty picker and have to search for models they already had. It runs on
// every listing rather than once, so an account gains models the same way it
// gained its first: add a second provider key months later and that provider's
// models arrive, and a model added to the catalog next year arrives too.
//
// What has been offered is recorded rather than inferred from whether the list
// is empty, because those are different states. An account that removed every
// model chose that, and re-filling its picker on the next read would be the
// application arguing with the user.
//
// Returns the preferences to use, which are the unmodified ones when there is
// nothing to seed or when seeding fails. Failure is not propagated: the caller
// is listing models, and an account that sees its catalog un-curated is in a
// better position than one that sees an error.
// seedPlan is what a seeding pass would change: model ids to append to the
// account's list, and to record as offered.
type seedPlan struct {
	AddModelIDs []string
}

// Empty reports whether there is nothing to do.
func (p seedPlan) Empty() bool { return len(p.AddModelIDs) == 0 }

// planProviderSeed decides what to offer an account, without touching the
// database.
//
// A model is offered when the account can reach its provider and has never
// been offered it before. The second half is the whole mechanism: it tells a
// model the account has never seen apart from one it removed on purpose, so a
// genuinely new model arrives while a deliberate removal stays gone.
//
// Recording per model rather than per provider matters. Per provider, an
// account's list would freeze at whatever the catalog held the day that
// provider was first seeded, and a model added later — by an upgrade, or by an
// operator curating the catalog — would never reach anyone who already had
// that provider.
func planProviderSeed(
	prefs *models.UserPreferences,
	catalog []*models.Model,
	configured func(provider string) bool,
) seedPlan {
	if prefs == nil {
		return seedPlan{}
	}

	seen := make(map[string]bool, len(prefs.SeenModelIDs))
	for _, id := range prefs.SeenModelIDs {
		seen[id] = true
	}

	plan := seedPlan{}
	for _, m := range catalog {
		if m == nil || seen[m.ID.String()] {
			continue
		}
		if !configured(m.Provider) {
			// Not reachable by this account yet, and deliberately not recorded
			// as offered: it should arrive whole on the day a key for it is
			// added, rather than being silently written off now.
			continue
		}
		plan.AddModelIDs = append(plan.AddModelIDs, m.ID.String())
		seen[m.ID.String()] = true
	}
	return plan
}

func (d *Datastore) seedProviderModels(
	ctx context.Context,
	userID uuid.UUID,
	prefs *models.UserPreferences,
	catalog []*models.Model,
	configured func(provider string) bool,
) *models.UserPreferences {
	if prefs == nil {
		return nil
	}
	plan := planProviderSeed(prefs, catalog, configured)
	if plan.Empty() {
		return prefs
	}

	updated := *prefs
	updated.AddedModelIDs = append(append([]string{}, prefs.AddedModelIDs...), plan.AddModelIDs...)
	updated.SeenModelIDs = append(append([]string{}, prefs.SeenModelIDs...), plan.AddModelIDs...)

	saved, err := d.UpdateUserPreferences(ctx, userID, updated)
	if err != nil {
		d.logger.Warn("could not offer new models to the account's list; showing the catalog uncurated",
			zap.String("user_id", userID.String()),
			zap.Int("models", len(plan.AddModelIDs)),
			zap.Error(err))
		return prefs
	}
	d.logger.Info("offered new models to the account's list",
		zap.String("user_id", userID.String()),
		zap.Int("models", len(plan.AddModelIDs)))
	return saved
}
