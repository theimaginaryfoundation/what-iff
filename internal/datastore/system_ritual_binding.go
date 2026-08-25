package datastore

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/systemritualbinding"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

var ErrHotkeyConflict = errors.New("hotkey already in use by another ritual")

// withTx runs fn inside a transaction. On error or panic, the transaction is rolled back.
func (d *Datastore) withTx(ctx context.Context, fn func(tx *ent.Tx) error) error {
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
	if err := fn(tx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}
	return nil
}

// GetSystemBindingsForUser returns all system ritual bindings for a user.
func (d *Datastore) GetSystemBindingsForUser(ctx context.Context, userID uuid.UUID) ([]*models.SystemRitualBinding, error) {
	var entBindings []*ent.SystemRitualBinding
	err := d.withTx(ctx, func(tx *ent.Tx) error {
		var err error
		entBindings, err = tx.SystemRitualBinding.Query().
			Where(systemritualbinding.HasUserWith(user.ID(userID))).
			All(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "system ritual bindings"), zap.Error(err))
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	modelsOut := make([]*models.SystemRitualBinding, 0, len(entBindings))
	for _, b := range entBindings {
		hot := ""
		if b.Hotkeys != nil {
			hot = *b.Hotkeys
		}
		// ritual_id is stored as uuid string in the schema; ent provides RitualID field
		modelsOut = append(modelsOut, &models.SystemRitualBinding{
			ID:        b.ID,
			UserID:    userID,
			RitualID:  b.RitualID,
			Hotkeys:   hot,
			CreatedAt: b.CreatedAt,
			UpdatedAt: b.UpdatedAt,
		})
	}

	return modelsOut, nil
}

// UpsertSystemBinding creates or updates a system ritual binding for a user.
func (d *Datastore) UpsertSystemBinding(ctx context.Context, userID, ritualID uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error) {
	var result *models.SystemRitualBinding
	err := d.withTx(ctx, func(tx *ent.Tx) error {
		var err error
		result, err = d.upsertSystemBinding(ctx, tx, userID, ritualID, hotkeys)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// upsertSystemBinding performs the upsert logic inside an existing transaction.
func (d *Datastore) upsertSystemBinding(ctx context.Context, tx *ent.Tx, userID, ritualID uuid.UUID, hotkeys string) (*models.SystemRitualBinding, error) {
	if strings.TrimSpace(hotkeys) != "" {
		conflict, cErr := tx.SystemRitualBinding.Query().
			Where(
				systemritualbinding.HasUserWith(user.ID(userID)),
				systemritualbinding.HotkeysEQ(hotkeys),
			).
			Only(ctx)
		if cErr == nil {
			if conflict.RitualID != ritualID {
				return nil, ErrHotkeyConflict
			}
		} else if !ent.IsNotFound(cErr) {
			d.logger.Error(i18n.T("system_binding.hotkey_check_failed"), zap.Error(cErr))
			return nil, cErr
		}
	}

	existing, err := tx.SystemRitualBinding.Query().
		Where(
			systemritualbinding.HasUserWith(user.ID(userID)),
			systemritualbinding.RitualIDEQ(ritualID),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		d.logger.Error(i18n.T1("query.failed", "Entity", "existing system ritual binding"), zap.Error(err))
		return nil, err
	}

	var hkPtr *string
	if trimmed := strings.TrimSpace(hotkeys); trimmed != "" {
		hkPtr = &trimmed
	}

	var entBinding *ent.SystemRitualBinding
	if existing != nil {
		entBinding, err = tx.SystemRitualBinding.UpdateOneID(existing.ID).
			SetNillableHotkeys(hkPtr).
			Save(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("update.failed", "Entity", "system ritual binding"), zap.Error(err))
			return nil, err
		}
	} else {
		entBinding, err = tx.SystemRitualBinding.Create().
			SetRitualID(ritualID).
			SetNillableHotkeys(hkPtr).
			SetUserID(userID).
			Save(ctx)
		if err != nil {
			d.logger.Warn(i18n.T("system_binding.create_race_failed"), zap.Error(err))
			retryExisting, rerr := tx.SystemRitualBinding.Query().
				Where(
					systemritualbinding.HasUserWith(user.ID(userID)),
					systemritualbinding.RitualIDEQ(ritualID),
				).
				Only(ctx)
			if rerr == nil && retryExisting != nil {
				entBinding, err = tx.SystemRitualBinding.UpdateOneID(retryExisting.ID).
					SetNillableHotkeys(hkPtr).
					Save(ctx)
				if err != nil {
					d.logger.Error(i18n.T("system_binding.create_race_update_failed"), zap.Error(err))
					return nil, err
				}
			} else {
				d.logger.Error(i18n.T1("create.failed", "Entity", "system ritual binding"), zap.Error(err))
				return nil, err
			}
		}
	}

	entBinding, err = tx.SystemRitualBinding.Query().Where(systemritualbinding.ID(entBinding.ID)).Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("reload.failed", "Entity", "system ritual binding"), zap.Error(err))
		return nil, err
	}

	hot := ""
	if entBinding.Hotkeys != nil {
		hot = *entBinding.Hotkeys
	}
	return &models.SystemRitualBinding{
		ID:        entBinding.ID,
		UserID:    userID,
		RitualID:  entBinding.RitualID,
		Hotkeys:   hot,
		CreatedAt: entBinding.CreatedAt,
		UpdatedAt: entBinding.UpdatedAt,
	}, nil
}

// DeleteSystemBinding removes a system ritual binding for a user.
func (d *Datastore) DeleteSystemBinding(ctx context.Context, userID, ritualID uuid.UUID) error {
	return d.withTx(ctx, func(tx *ent.Tx) error {
		_, err := tx.SystemRitualBinding.Delete().
			Where(
				systemritualbinding.HasUserWith(user.ID(userID)),
				systemritualbinding.RitualIDEQ(ritualID),
			).
			Exec(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("delete.failed", "Entity", "system ritual binding"), zap.Error(err))
			return err
		}
		return nil
	})
}
