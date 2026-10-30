package database

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/agentjob"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/providerconnection"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ConvertSeededModels moves the seeded catalog onto accounts' connections, on
// an install whose catalog was seeded (SeedModelCatalog), and removes it
// (ADR 0x027). For each account that uses a seeded model, as its default, in
// a chat, in an agent job or as a favourite, the model is enabled on the
// account's built-in connection for its provider, with the same name and
// capabilities, and the account's uses point at it.
//
// An account with no connection for the model's provider (no key, so it could
// never run that model) keeps nothing: its default is cleared, its chats fall
// back to its default, the favourite is dropped, and a job that names the
// model fails because the model no longer exists. Retired seeded models are
// removed the same way, without being converted.
//
// It runs at startup in one transaction and is safe to run again, or on two
// instances at once: a model already on the connection is reused, and once the
// seeded rows are gone there is nothing left to do. It reports how many
// seeded rows it removed.
func ConvertSeededModels(ctx context.Context, client *ent.Client, logger *zap.Logger) (int, error) {
	tx, err := client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin converting seeded models: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	seeded, err := tx.Model.Query().
		Where(model.ConnectionIDIsNil(), model.NameIn(seededNames()...)).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("read seeded models: %w", err)
	}
	if len(seeded) == 0 {
		return 0, nil
	}
	c := seedConversion{tx: tx, converted: map[uuid.UUID]map[uuid.UUID]uuid.UUID{}}
	for _, m := range seeded {
		if err := c.convert(ctx, m); err != nil {
			return 0, err
		}
	}
	if err := c.repointFavorites(ctx, seeded); err != nil {
		return 0, err
	}
	if err := c.clearRest(ctx, modelIDs(seeded)); err != nil {
		return 0, err
	}
	if _, err := tx.Model.Delete().Where(model.IDIn(modelIDs(seeded)...)).Exec(ctx); err != nil {
		return 0, fmt.Errorf("remove seeded models: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit converted models: %w", err)
	}
	logger.Info("converted seeded models onto accounts' connections", zap.Int("seeded_models", len(seeded)))
	return len(seeded), nil
}

// seedConversion holds one conversion's transaction and, per seeded model,
// the model each account's uses now point at.
type seedConversion struct {
	tx *ent.Tx
	// converted maps a seeded model's ID to each converting account's new model.
	converted map[uuid.UUID]map[uuid.UUID]uuid.UUID
}

// convert repoints every account's uses of the seeded model m that it can.
func (c *seedConversion) convert(ctx context.Context, m *ent.Model) error {
	users, err := c.usersOf(ctx, m.ID)
	if err != nil {
		return err
	}
	c.converted[m.ID] = map[uuid.UUID]uuid.UUID{}
	if m.Deleted {
		return nil
	}
	for _, userID := range users {
		conn, err := c.tx.ProviderConnection.Query().
			Where(providerconnection.UserID(userID), providerconnection.Builtin(string(m.Provider))).
			Only(ctx)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read user %s's %s connection: %w", userID, m.Provider, err)
		}
		target, err := c.modelOn(ctx, conn, m)
		if err != nil {
			return err
		}
		c.converted[m.ID][userID] = target
		if err := c.repoint(ctx, userID, m.ID, target); err != nil {
			return err
		}
	}
	return nil
}

// usersOf lists the accounts that use the model as their default, in a chat
// or in an agent job. Favourites are handled with the rest (repointFavorites).
func (c *seedConversion) usersOf(ctx context.Context, modelID uuid.UUID) ([]uuid.UUID, error) {
	var users []uuid.UUID
	byDefault, err := c.tx.User.Query().
		Where(user.HasPreferencesWith(userpreference.DefaultModel(modelID))).
		IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read accounts defaulting to model %s: %w", modelID, err)
	}
	byChat, err := c.tx.User.Query().
		Where(user.HasChatsWith(chat.HasModelWith(model.ID(modelID)))).
		IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read accounts chatting with model %s: %w", modelID, err)
	}
	byJob, err := c.tx.User.Query().
		Where(user.HasAgentJobsWith(agentjob.ModelID(modelID))).
		IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("read accounts with jobs on model %s: %w", modelID, err)
	}
	byFavorite, err := c.favoritingUsers(ctx, modelID)
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	for _, ids := range [][]uuid.UUID{byDefault, byChat, byJob, byFavorite} {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				users = append(users, id)
			}
		}
	}
	return users, nil
}

// favoritingUsers lists the accounts with the model among their favourites.
func (c *seedConversion) favoritingUsers(ctx context.Context, modelID uuid.UUID) ([]uuid.UUID, error) {
	prefs, err := c.tx.UserPreference.Query().WithUser().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read favourite models: %w", err)
	}
	var out []uuid.UUID
	for _, p := range prefs {
		if p.Edges.User != nil && slices.Contains(p.FavoriteModelIds, modelID.String()) {
			out = append(out, p.Edges.User.ID)
		}
	}
	return out, nil
}

// modelOn is the connection's model with the seeded model's name, created from
// it when the account has not enabled it already, and brought back if the
// account had turned it off.
func (c *seedConversion) modelOn(ctx context.Context, conn *ent.ProviderConnection, m *ent.Model) (uuid.UUID, error) {
	err := c.tx.Model.Create().
		SetConnectionID(conn.ID).
		SetName(m.Name).
		SetDisplayName(m.DisplayName).
		SetDescription(m.Description).
		SetProvider(m.Provider).
		SetToolSupport(m.ToolSupport).
		SetNillableVisionSupport(m.VisionSupport).
		SetNillableReasoningSupportOverride(m.ReasoningSupportOverride).
		SetBaseCreditsPerSlab(m.BaseCreditsPerSlab).
		SetSubscriptionTier(m.SubscriptionTier).
		OnConflictColumns(model.FieldConnectionID, model.FieldName).
		Ignore().
		Exec(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("enable %s on connection %s: %w", m.Name, conn.ID, err)
	}
	target, err := c.tx.Model.Query().
		Where(model.ConnectionID(conn.ID), model.Name(m.Name)).
		Only(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read %s on connection %s: %w", m.Name, conn.ID, err)
	}
	if target.Deleted {
		if err := target.Update().SetDeleted(false).Exec(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("enable %s on connection %s: %w", m.Name, conn.ID, err)
		}
	}
	return target.ID, nil
}

// repoint moves one account's default, chats and agent jobs from the seeded
// model to its converted one.
func (c *seedConversion) repoint(ctx context.Context, userID, from, to uuid.UUID) error {
	if _, err := c.tx.UserPreference.Update().
		Where(userpreference.HasUserWith(user.ID(userID)), userpreference.DefaultModel(from)).
		SetDefaultModel(to).
		Save(ctx); err != nil {
		return fmt.Errorf("repoint user %s's default model: %w", userID, err)
	}
	if _, err := c.tx.Chat.Update().
		Where(chat.HasOwnerWith(user.ID(userID)), chat.HasModelWith(model.ID(from))).
		SetModelID(to).
		Save(ctx); err != nil {
		return fmt.Errorf("repoint user %s's chats: %w", userID, err)
	}
	if _, err := c.tx.AgentJob.Update().
		Where(agentjob.HasOwnerWith(user.ID(userID)), agentjob.ModelID(from)).
		SetModelID(to).
		Save(ctx); err != nil {
		return fmt.Errorf("repoint user %s's agent jobs: %w", userID, err)
	}
	return nil
}

// clearRest clears the defaults and chat models still on the seeded models:
// those of accounts with no connection for the model's provider. A chat with no
// model uses the account's default. Agent jobs keep the model they name, which
// no longer exists, so their runs fail clearly.
func (c *seedConversion) clearRest(ctx context.Context, seeded []uuid.UUID) error {
	if _, err := c.tx.UserPreference.Update().
		Where(userpreference.DefaultModelIn(seeded...)).
		ClearDefaultModel().
		Save(ctx); err != nil {
		return fmt.Errorf("clear default models: %w", err)
	}
	if _, err := c.tx.Chat.Update().
		Where(chat.HasModelWith(model.IDIn(seeded...))).
		ClearModel().
		Save(ctx); err != nil {
		return fmt.Errorf("clear chat models: %w", err)
	}
	return nil
}

// repointFavorites replaces each account's seeded favourites with their
// converted models, and drops the ones that were not converted.
func (c *seedConversion) repointFavorites(ctx context.Context, seeded []*ent.Model) error {
	// Favourites are stored as id strings: the seeded models by that string.
	seededIDs := make(map[string]uuid.UUID, len(seeded))
	for _, m := range seeded {
		seededIDs[m.ID.String()] = m.ID
	}
	prefs, err := c.tx.UserPreference.Query().WithUser().All(ctx)
	if err != nil {
		return fmt.Errorf("read favourite models: %w", err)
	}
	for _, p := range prefs {
		if p.Edges.User == nil || len(p.FavoriteModelIds) == 0 {
			continue
		}
		favorites, changed := c.convertFavorites(p.Edges.User.ID, p.FavoriteModelIds, seededIDs)
		if !changed {
			continue
		}
		if err := p.Update().SetFavoriteModelIds(favorites).Exec(ctx); err != nil {
			return fmt.Errorf("repoint user %s's favourite models: %w", p.Edges.User.ID, err)
		}
	}
	return nil
}

func (c *seedConversion) convertFavorites(userID uuid.UUID, favorites []string, seededIDs map[string]uuid.UUID) ([]string, bool) {
	out := make([]string, 0, len(favorites))
	changed := false
	for _, id := range favorites {
		seededID, isSeeded := seededIDs[id]
		if !isSeeded {
			out = append(out, id)
			continue
		}
		changed = true
		if to, ok := c.converted[seededID][userID]; ok && !slices.Contains(out, to.String()) {
			out = append(out, to.String())
		}
	}
	return out, changed
}

func seededNames() []string {
	names := make([]string, 0, len(appmodels.AvailableModels))
	for _, m := range appmodels.AvailableModels {
		names = append(names, m.Name)
	}
	return names
}

func modelIDs(rows []*ent.Model) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, m := range rows {
		out = append(out, m.ID)
	}
	return out
}
