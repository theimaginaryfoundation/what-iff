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

// ErrInvalidUtilitySetting is a utility model setting that can't exist: an unknown role or
// scope, or a scope written with the wrong owner (the deployment's has no user; an account's
// has one). It is always a programming error, never the caller's input.
var ErrInvalidUtilitySetting = errors.New("invalid utility model setting")

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
		out = append(out, models.UtilityModelSetting{
			Scope:   models.UtilityScope(r.Scope),
			Role:    models.UtilityRole(r.Role),
			ModelID: r.ModelID,
		})
	}
	return out, nil
}

// SetUtilityModel chooses modelID for role at scope, for the account userID, or
// for the deployment when userID is uuid.Nil, replacing any earlier choice.
//
// The model must be one the chooser can use, or ErrModelNotFound: an account
// chooses from the catalog and its own connections' models, and the deployment,
// whose choice applies to every account, from the catalog only.
func (ds *Datastore) SetUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) error {
	err := ds.setUtilityModel(ctx, scope, userID, role, modelID)
	if ent.IsConstraintError(err) {
		// Two writes for the same role raced: both cleared nothing, and the other one saved
		// first. Once more replaces it, so the last write wins.
		err = ds.setUtilityModel(ctx, scope, userID, role, modelID)
	}
	return err
}

func (ds *Datastore) setUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) error {
	tx, err := ds.dbClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin utility model setting: %w", err)
	}
	if err := setUtilityModelTx(ctx, tx, scope, userID, role, modelID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit utility model setting: %w", err)
	}
	return nil
}

func setUtilityModelTx(ctx context.Context, tx *ent.Tx, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole, modelID uuid.UUID) error {
	if !role.Valid() || (scope != models.UtilityScopeDeployment && scope != models.UtilityScopeAccount) ||
		(scope == models.UtilityScopeDeployment) != (userID == uuid.Nil) {
		return fmt.Errorf("%w: role %q, scope %q, user %s", ErrInvalidUtilitySetting, role, scope, userID)
	}
	if userID == uuid.Nil {
		catalog, err := tx.Model.Query().
			Where(entmodel.ID(modelID), entmodel.Deleted(false), entmodel.ConnectionIDIsNil()).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("check utility model: %w", err)
		}
		if !catalog {
			return ErrModelNotFound
		}
	} else if err := assertUserCanUseModelTx(ctx, tx, userID, modelID); err != nil {
		return err
	}
	if _, err := tx.UtilityModelSetting.Delete().
		Where(utilityScopeOwner(scope, userID), utilitymodelsetting.Role(string(role))).
		Exec(ctx); err != nil {
		return fmt.Errorf("replace utility model setting: %w", err)
	}
	create := tx.UtilityModelSetting.Create().
		SetScope(string(scope)).
		SetRole(string(role)).
		SetModelID(modelID)
	if userID != uuid.Nil {
		create.SetUserID(userID)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("save utility model setting: %w", err)
	}
	return nil
}

// ClearUtilityModel removes the choice for role at scope, so the role falls back
// to the next scope's choice or its built-in default.
func (ds *Datastore) ClearUtilityModel(ctx context.Context, scope models.UtilityScope, userID uuid.UUID, role models.UtilityRole) error {
	if _, err := ds.dbClient.UtilityModelSetting.Delete().
		Where(utilityScopeOwner(scope, userID), utilitymodelsetting.Role(string(role))).
		Exec(ctx); err != nil {
		return fmt.Errorf("clear utility model setting: %w", err)
	}
	return nil
}
