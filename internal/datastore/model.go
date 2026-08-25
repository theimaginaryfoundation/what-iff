package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrModelNotFound               = errors.New("model not found")
	ErrExperimentalModelNotAllowed = errors.New("experimental model not allowed for this user")
	ErrModelNameExists             = errors.New("model name already exists")
	ErrInvalidModelName            = errors.New("invalid model name")
	ErrInvalidProvider             = errors.New("invalid model provider")
	ErrInvalidSubscriptionTier     = errors.New("invalid model subscription tier")
)

func toModelModel(e *ent.Model) *models.Model {
	return &models.Model{
		ID:                 e.ID,
		Name:               e.Name,
		DisplayName:        e.DisplayName,
		Description:        e.Description,
		Provider:           string(e.Provider),
		ToolSupport:        e.ToolSupport,
		BaseCreditsPerSlab: e.BaseCreditsPerSlab,
		SubscriptionTier:   string(e.SubscriptionTier),
		Deleted:            e.Deleted,
		IsDefault:          e.IsDefault,
	}
}

// defaultBaseCreditsPerSlab is the fallback per-slab base credit for a model row
// that has none configured. It must match the ent schema default for
// models.base_credits_per_slab (see ent/schema/model.go, Default(1)).
const defaultBaseCreditsPerSlab int64 = 1

func normalizeBaseCreditsPerSlab(v int64) int64 {
	if v < 1 {
		return defaultBaseCreditsPerSlab
	}
	return v
}

func normalizeModelSubscriptionTier(raw string) (entmodel.SubscriptionTier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return entmodel.SubscriptionTierHigh, nil
	case string(entmodel.SubscriptionTierLow):
		return entmodel.SubscriptionTierLow, nil
	case string(entmodel.SubscriptionTierMedium):
		return entmodel.SubscriptionTierMedium, nil
	case string(entmodel.SubscriptionTierHigh):
		return entmodel.SubscriptionTierHigh, nil
	case string(entmodel.SubscriptionTierUltra):
		return entmodel.SubscriptionTierUltra, nil
	default:
		return "", ErrInvalidSubscriptionTier
	}
}

func normalizeModelProvider(raw string) (entmodel.Provider, error) {
	provider := strings.TrimSpace(strings.ToLower(raw))
	if provider == "" {
		return entmodel.ProviderOpenai, nil
	}
	switch provider {
	case string(entmodel.ProviderOpenai):
		return entmodel.ProviderOpenai, nil
	case string(entmodel.ProviderAnthropic):
		return entmodel.ProviderAnthropic, nil
	case string(entmodel.ProviderZai):
		return entmodel.ProviderZai, nil
	case string(entmodel.ProviderGoogle):
		return entmodel.ProviderGoogle, nil
	case string(entmodel.ProviderMistral):
		return entmodel.ProviderMistral, nil
	case string(entmodel.ProviderDeepseek):
		return entmodel.ProviderDeepseek, nil
	case string(entmodel.ProviderQwen):
		return entmodel.ProviderQwen, nil
	case string(entmodel.ProviderXiaomi):
		return entmodel.ProviderXiaomi, nil
	default:
		return "", ErrInvalidProvider
	}
}

func (d *Datastore) ListModels(ctx context.Context) ([]*models.Model, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	dbModels, err := tx.Model.Query().
		Where(entmodel.Deleted(false)).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "models"), zap.Error(err))
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	models := make([]*models.Model, len(dbModels))
	for i, dbModel := range dbModels {
		models[i] = toModelModel(dbModel)
	}

	return models, nil
}

func filterVisibleModels(all []*models.Model, allowExperimental bool) []*models.Model {
	if allowExperimental {
		return all
	}
	filtered := make([]*models.Model, 0, len(all))
	for _, m := range all {
		if !models.IsExperimentalModel(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// ListModelsDefault returns the standard catalog for callers without experimental access
// (signed-out users and signed-in users with enable_experimental_models=false).
func (d *Datastore) ListModelsDefault(ctx context.Context) ([]*models.Model, error) {
	all, err := d.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	return filterVisibleModels(all, false), nil
}

// ListModelsForUser returns active catalog models visible to the given user.
// Experimental providers are omitted unless enable_experimental_models is set.
// The gated set lives in models.IsExperimentalModelRecord (currently empty —
// graduated vendors are generally available; add new dogfood vendors there).
func (d *Datastore) ListModelsForUser(ctx context.Context, userID uuid.UUID) ([]*models.Model, error) {
	all, err := d.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	u, err := d.dbClient.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrUserNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "user"), zap.Error(err))
		return nil, err
	}
	if u.EnableExperimentalModels {
		return all, nil
	}
	return filterVisibleModels(all, false), nil
}

func assertUserCanUseModelTx(ctx context.Context, tx *ent.Tx, userID uuid.UUID, modelID uuid.UUID) error {
	if modelID == uuid.Nil {
		return nil
	}
	dbModel, err := tx.Model.Query().
		Where(entmodel.ID(modelID), entmodel.Deleted(false)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrModelNotFound
		}
		return err
	}
	if !models.IsExperimentalModel(toModelModel(dbModel)) {
		return nil
	}
	u, err := tx.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return ErrUserNotFound
		}
		return err
	}
	if !u.EnableExperimentalModels {
		return ErrExperimentalModelNotAllowed
	}
	return nil
}

func (d *Datastore) assertUserCanUseModel(ctx context.Context, tx *ent.Tx, userID uuid.UUID, modelID uuid.UUID) error {
	return assertUserCanUseModelTx(ctx, tx, userID, modelID)
}

// AdminPatchUserExperimentalModels toggles experimental model access for a user.
func (d *Datastore) AdminPatchUserExperimentalModels(ctx context.Context, userID uuid.UUID, enabled bool) (*models.UserResponse, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	u, err := tx.User.Query().
		Where(user.ID(userID)).
		WithRoles().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrUserNotFound
		}
		d.logger.Error(i18n.T("admin.user_get_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	for _, role := range u.Edges.Roles {
		if role.Name == "super_admin" {
			d.logger.Warn(i18n.T1("admin.super_admin_modify_attempted", "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrCannotModifySuperAdmin
		}
	}

	if _, err := tx.User.UpdateOneID(userID).
		SetEnableExperimentalModels(enabled).
		Save(ctx); err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "user"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	u, err = tx.User.Query().
		Where(user.ID(userID)).
		WithPreferences().
		WithRoles().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("admin.user_prefs_load_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	d.logger.Info(i18n.T1("admin.user_updated", "UserID", userID.String()))
	return toUserModel(u), nil
}

// GetModel retrieves an active (non-deleted) model by ID.
func (d *Datastore) GetModel(ctx context.Context, id uuid.UUID) (*models.Model, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	dbModel, err := tx.Model.Query().
		Where(entmodel.ID(id), entmodel.Deleted(false)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				return nil, fmt.Errorf("model lookup failed and rollback failed: %w", errors.Join(err, rerr))
			}
			return nil, ErrModelNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toModelModel(dbModel), nil
}

// GetModelByName returns a non-deleted model by API name, or ErrModelNotFound.
func (d *Datastore) GetModelByName(ctx context.Context, name string) (*models.Model, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrModelNotFound
	}
	dbModel, err := d.dbClient.Model.Query().
		Where(entmodel.Name(name), entmodel.Deleted(false)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrModelNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(err))
		return nil, err
	}
	return toModelModel(dbModel), nil
}

func (d *Datastore) AdminListModels(ctx context.Context, deleted bool) ([]*models.Model, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	dbModels, err := tx.Model.Query().
		Where(entmodel.Deleted(deleted)).
		Order(ent.Asc(entmodel.FieldDisplayName)).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "models"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	result := make([]*models.Model, len(dbModels))
	for i, dbModel := range dbModels {
		result[i] = toModelModel(dbModel)
	}

	return result, nil
}

func (d *Datastore) AdminGetModel(ctx context.Context, id uuid.UUID) (*models.Model, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	dbModel, err := tx.Model.Query().
		Where(entmodel.ID(id), entmodel.Deleted(false)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrModelNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toModelModel(dbModel), nil
}

func (d *Datastore) AdminCreateModel(ctx context.Context, req models.CreateModelRequest) (*models.Model, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	name := strings.TrimSpace(req.Name)
	displayName := strings.TrimSpace(req.DisplayName)
	if name == "" || displayName == "" {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrInvalidModelName
	}
	providerValue, err := normalizeModelProvider(req.Provider)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	subscriptionTier, err := normalizeModelSubscriptionTier(req.SubscriptionTier)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	existing, err := tx.Model.Query().
		Where(entmodel.Name(name)).
		Only(ctx)
	if err == nil {
		if existing.Deleted {
			restored, rerr := tx.Model.UpdateOneID(existing.ID).
				SetDeleted(false).
				SetDisplayName(displayName).
				SetDescription(req.Description).
				SetProvider(providerValue).
				SetToolSupport(req.ToolSupport).
				SetBaseCreditsPerSlab(normalizeBaseCreditsPerSlab(req.BaseCreditsPerSlab)).
				SetSubscriptionTier(subscriptionTier).
				Save(ctx)
			if rerr != nil {
				d.logger.Error(i18n.T1("update.failed", "Entity", "model"), zap.Error(rerr))
				if rbErr := tx.Rollback(); rbErr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rbErr))
				}
				return nil, rerr
			}
			if cerr := tx.Commit(); cerr != nil {
				d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(cerr))
				return nil, cerr
			}
			out := toModelModel(restored)
			d.writeAuditLog(ctx, auditEntry{
				Category: auditCategoryModel,
				Action:   "restore",
				Message:  fmt.Sprintf("admin restored soft-deleted model id=%s name=%s", out.ID, out.Name),
				Metadata: map[string]any{
					"model_id":     out.ID.String(),
					"name":         out.Name,
					"display_name": out.DisplayName,
				},
			})
			return out, nil
		}
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrModelNameExists
	}
	if !ent.IsNotFound(err) {
		d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	dbModel, err := tx.Model.Create().
		SetName(name).
		SetDisplayName(displayName).
		SetDescription(req.Description).
		SetProvider(providerValue).
		SetToolSupport(req.ToolSupport).
		SetBaseCreditsPerSlab(normalizeBaseCreditsPerSlab(req.BaseCreditsPerSlab)).
		SetSubscriptionTier(subscriptionTier).
		SetDeleted(false).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	out := toModelModel(dbModel)
	d.writeAuditLog(ctx, auditEntry{
		Category: auditCategoryModel,
		Action:   "create",
		Message:  fmt.Sprintf("admin created model id=%s name=%s", out.ID, out.Name),
		Metadata: map[string]any{
			"model_id":     out.ID.String(),
			"name":         out.Name,
			"display_name": out.DisplayName,
		},
	})
	return out, nil
}

func (d *Datastore) AdminUpdateModel(ctx context.Context, id uuid.UUID, req models.UpdateModelRequest) (*models.Model, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	current, err := tx.Model.Query().
		Where(entmodel.ID(id), entmodel.Deleted(false)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrModelNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	update := tx.Model.UpdateOneID(current.ID)

	if req.Name != "" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrInvalidModelName
		}
		if name != current.Name {
			exists, qerr := tx.Model.Query().
				Where(entmodel.Name(name), entmodel.Deleted(false)).
				Exist(ctx)
			if qerr != nil {
				d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(qerr))
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, qerr
			}
			if exists {
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, ErrModelNameExists
			}
			update.SetName(name)
		}
	}

	if req.DisplayName != "" {
		displayName := strings.TrimSpace(req.DisplayName)
		if displayName != "" {
			update.SetDisplayName(displayName)
		}
	}

	if req.Description != "" {
		update.SetDescription(req.Description)
	}
	if req.Provider != "" {
		providerValue, perr := normalizeModelProvider(req.Provider)
		if perr != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, perr
		}
		update.SetProvider(providerValue)
	}

	if req.ToolSupport != nil {
		update.SetToolSupport(*req.ToolSupport)
	}
	if req.BaseCreditsPerSlab != nil {
		update.SetBaseCreditsPerSlab(normalizeBaseCreditsPerSlab(*req.BaseCreditsPerSlab))
	}
	if req.SubscriptionTier != nil {
		subscriptionTier, err := normalizeModelSubscriptionTier(*req.SubscriptionTier)
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
		update.SetSubscriptionTier(subscriptionTier)
	}

	updated, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	out := toModelModel(updated)
	d.writeAuditLog(ctx, auditEntry{
		Category: auditCategoryModel,
		Action:   "update",
		Message:  fmt.Sprintf("admin updated model id=%s name=%s", out.ID, out.Name),
		Metadata: map[string]any{
			"model_id":     out.ID.String(),
			"name":         out.Name,
			"display_name": out.DisplayName,
		},
	})
	return out, nil
}

func (d *Datastore) AdminDeleteModel(ctx context.Context, id uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	affected, err := tx.Model.Update().
		Where(entmodel.ID(id), entmodel.Deleted(false)).
		SetDeleted(true).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if affected == 0 {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrModelNotFound
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	d.writeAuditLog(ctx, auditEntry{
		Category: auditCategoryModel,
		Action:   "soft_delete",
		Message:  fmt.Sprintf("admin soft-deleted model id=%s", id),
		Metadata: map[string]any{"model_id": id.String()},
	})
	return nil
}

// AdminSetDefaultModel marks the given active model as the application-wide default.
// It clears the is_default flag from every other model in the same transaction so
// that at most one default is ever active, satisfying the "only one at a time" rule
// without relying on a partial unique index.
func (d *Datastore) AdminSetDefaultModel(ctx context.Context, id uuid.UUID) (*models.Model, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// The target must exist and be active (soft-deleted models cannot be the default).
	if _, err := tx.Model.Query().
		Where(entmodel.ID(id), entmodel.Deleted(false)).
		Only(ctx); err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrModelNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Clear the flag from any other model currently marked default.
	if _, err := tx.Model.Update().
		Where(entmodel.IsDefault(true), entmodel.IDNEQ(id)).
		SetIsDefault(false).
		Save(ctx); err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	updated, err := tx.Model.UpdateOneID(id).SetIsDefault(true).Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "model"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	out := toModelModel(updated)
	d.writeAuditLog(ctx, auditEntry{
		Category: auditCategoryModel,
		Action:   "set_default",
		Message:  fmt.Sprintf("admin set application default model id=%s name=%s", out.ID, out.Name),
		Metadata: map[string]any{
			"model_id": out.ID.String(),
			"name":     out.Name,
		},
	})
	return out, nil
}

// ResolveDefaultModelName returns the name of the application default model. The
// admin-configured default (is_default=true) wins; when none is set it falls back to
// the env/const bootstrap default. This is the single source of truth for "what is
// the default model" outside of a user-creation transaction.
func (d *Datastore) ResolveDefaultModelName(ctx context.Context) string {
	if m, err := d.dbClient.Model.Query().
		Where(entmodel.IsDefault(true), entmodel.Deleted(false)).
		First(ctx); err == nil && m != nil {
		return m.Name
	}
	return getDefaultModelName()
}

// resolveDefaultModelTx resolves the model to seed a new user's preferences with,
// inside the user-creation transaction. Preference order: admin-configured default,
// then the env/const named default, then any active model.
func resolveDefaultModelTx(ctx context.Context, tx *ent.Tx) (*ent.Model, error) {
	if m, err := tx.Model.Query().
		Where(entmodel.IsDefault(true), entmodel.Deleted(false)).
		First(ctx); err == nil && m != nil {
		return m, nil
	}
	name := getDefaultModelName()
	m, err := tx.Model.Query().
		Where(entmodel.Name(name), entmodel.Deleted(false)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return tx.Model.Query().
			Where(entmodel.Deleted(false)).
			First(ctx)
	}
	return m, err
}
