package datastore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/utilitymodelsetting"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ErrInvalidUtilitySetting is a utility model setting that can't exist: an unknown role, scope,
// reasoning effort or service tier, a scope written with the wrong owner (the deployment's has
// no user; an account's has one), or Off with an effort or tier. Handlers check these before
// calling and choose the scope and owner themselves, so reaching it means a caller has a bug.
var ErrInvalidUtilitySetting = errors.New("invalid utility model setting")

// validateUtilitySetting refuses a role, scope and owner no setting can have. Writing and
// clearing both check, so neither can touch a row that shouldn't exist.
func validateUtilitySetting(scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) error {
	if !role.Valid() || (scope != models.UtilityScopeDeployment && scope != models.UtilityScopeAccount) ||
		(scope == models.UtilityScopeDeployment) != (userID == uuid.Nil) {
		return fmt.Errorf("%w: role %q, scope %q, user %s", ErrInvalidUtilitySetting, role, scope, userID)
	}
	return nil
}

// utilityScopeOwner selects the settings of one scope: an account's, or, for
// userID uuid.Nil, the deployment's.
func utilityScopeOwner(scope models.UtilityScope, userID uuid.UUID) predicate.UtilityModelSetting {
	if userID == uuid.Nil {
		return utilitymodelsetting.And(utilitymodelsetting.Scope(string(scope)), utilitymodelsetting.UserIDIsNil())
	}
	return utilitymodelsetting.And(utilitymodelsetting.Scope(string(scope)), utilitymodelsetting.UserID(userID))
}

// ListUtilityModelSettings returns the deployment's utility model settings and,
// for a userID other than uuid.Nil, that account's.
func (ds *Datastore) ListUtilityModelSettings(ctx context.Context, userID uuid.UUID) ([]models.UtilityModelSetting, error) {
	where := utilityScopeOwner(models.UtilityScopeDeployment, uuid.Nil)
	if userID != uuid.Nil {
		where = utilitymodelsetting.Or(where, utilityScopeOwner(models.UtilityScopeAccount, userID))
	}
	rows, err := ds.dbClient.UtilityModelSetting.Query().Where(where).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list utility model settings: %w", err)
	}
	out := make([]models.UtilityModelSetting, 0, len(rows))
	for _, r := range rows {
		s := models.UtilityModelSetting{
			Scope:   models.UtilityScope(r.Scope),
			Role:    models.UtilityRole(r.Role),
			ModelID: r.ModelID,
		}
		if r.ReasoningEffort != nil {
			s.ReasoningEffort = models.ReasoningEffort(*r.ReasoningEffort)
		}
		if r.ServiceTier != nil {
			s.ServiceTier = models.UtilityServiceTier(*r.ServiceTier)
		}
		out = append(out, s)
	}
	return out, nil
}

// SetUtilityModel saves s, a choice for s.Role at s.Scope, for the account userID,
// or for the deployment when userID is uuid.Nil, replacing any earlier choice. A
// nil s.ModelID switches the role off.
//
// The model must be one the chooser can use, or ErrModelNotFound: an account
// chooses from the catalog and its own connections' models, and the deployment,
// whose choice applies to every account, from the catalog only.
func (ds *Datastore) SetUtilityModel(ctx context.Context, userID uuid.UUID, s models.UtilityModelSetting) error {
	err := ds.setUtilityModel(ctx, userID, s)
	if ent.IsConstraintError(err) {
		// Two writes for the same role raced: both cleared nothing, and the other one saved
		// first. Once more replaces it, so the last write wins.
		err = ds.setUtilityModel(ctx, userID, s)
	}
	return err
}

func (ds *Datastore) setUtilityModel(ctx context.Context, userID uuid.UUID, s models.UtilityModelSetting) error {
	tx, err := ds.dbClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin utility model setting: %w", err)
	}
	if err := setUtilityModelTx(ctx, tx, userID, s); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit utility model setting: %w", err)
	}
	return nil
}

func setUtilityModelTx(ctx context.Context, tx *ent.Tx, userID uuid.UUID, s models.UtilityModelSetting) error {
	scope, role, modelID := s.Scope, s.Role, s.ModelID
	if err := validateUtilitySetting(scope, userID, role); err != nil {
		return err
	}
	badEffort := s.ReasoningEffort != "" && !s.ReasoningEffort.Valid()
	offWithOptions := modelID == nil && (s.ReasoningEffort != "" || s.ServiceTier != "")
	if badEffort || !s.ServiceTier.Valid() || offWithOptions {
		return fmt.Errorf("%w: reasoning effort %q, service tier %q, model %v", ErrInvalidUtilitySetting, s.ReasoningEffort, s.ServiceTier, modelID)
	}
	switch {
	case modelID == nil:
		// Off: no model to check.
	case userID == uuid.Nil:
		catalog, err := tx.Model.Query().
			Where(entmodel.ID(*modelID), entmodel.Deleted(false), entmodel.ConnectionIDIsNil()).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("check utility model: %w", err)
		}
		if !catalog {
			return ErrModelNotFound
		}
	default:
		if err := assertUserCanUseModelTx(ctx, tx, userID, *modelID); err != nil {
			return err
		}
	}
	if _, err := tx.UtilityModelSetting.Delete().
		Where(utilityScopeOwner(scope, userID), utilitymodelsetting.Role(string(role))).
		Exec(ctx); err != nil {
		return fmt.Errorf("replace utility model setting: %w", err)
	}
	create := tx.UtilityModelSetting.Create().
		SetScope(string(scope)).
		SetRole(string(role)).
		SetNillableModelID(modelID)
	if s.ReasoningEffort != "" {
		create.SetReasoningEffort(string(s.ReasoningEffort))
	}
	if s.ServiceTier != "" {
		create.SetServiceTier(string(s.ServiceTier))
	}
	if userID != uuid.Nil {
		create.SetUserID(userID)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("save utility model setting: %w", err)
	}
	return nil
}

// ClearUtilityModel removes the choice for role at scope, so the role falls back
// to the next scope's choice, or is not set.
func (ds *Datastore) ClearUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) error {
	if err := validateUtilitySetting(scope, userID, role); err != nil {
		return err
	}
	if _, err := ds.dbClient.UtilityModelSetting.Delete().
		Where(utilityScopeOwner(scope, userID), utilitymodelsetting.Role(string(role))).
		Exec(ctx); err != nil {
		return fmt.Errorf("clear utility model setting: %w", err)
	}
	return nil
}
