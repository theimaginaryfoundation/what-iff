package datastore

import (
	"context"
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var defaultTheme = userpreference.ThemeDark

func toUserPreferencesModel(e *ent.UserPreference) *models.UserPreferences {

	prefs := models.UserPreferences{
		ID: e.ID,
	}

	if e.Theme.String() != "" {
		prefs.Theme = e.Theme.String()
	} else {
		prefs.Theme = defaultTheme.String()
	}

	if e.Edges.Model != nil {
		prefs.DefaultModelID = e.Edges.Model.ID
	}

	if e.Edges.User != nil {
		prefs.UserID = e.Edges.User.ID
	}

	if e.Edges.Personality != nil {
		prefs.DefaultPersonalityID = e.Edges.Personality.ID
	}

	// Normalise to a non-nil slice so the field always serialises as [] rather than
	// null. NOT NULL constrains the SQL value, not the JSON inside it — a jsonb column
	// can still hold the literal null, which unmarshals to a nil slice — so this is what
	// actually holds up the "always present" response contract.
	if e.FavoriteModelIds != nil {
		prefs.FavoriteModelIDs = e.FavoriteModelIds
	} else {
		prefs.FavoriteModelIDs = []string{}
	}

	prefs.ImageGenerationProvider = string(models.DefaultImageGenerationProvider)
	if e.ImageGenerationProvider != nil {
		prefs.ImageGenerationProvider = e.ImageGenerationProvider.String()
	}
	prefs.TurnSettingValues = turnSettingsFromEnt(e.ReasoningEffort, e.AgentLoop)

	return &prefs
}

func (d *Datastore) GetUserPreferences(ctx context.Context, userID uuid.UUID) (*models.UserPreferences, error) {
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

	userPreferences, err := tx.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		WithUser().
		WithModel().
		WithPersonality().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "user preferences"), zap.Error(err))
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toUserPreferencesModel(userPreferences), nil
}

func (d *Datastore) UpdateUserPreferences(ctx context.Context, userID uuid.UUID, prefs models.UserPreferences) (*models.UserPreferences, error) {
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

	update := tx.UserPreference.Update().
		Where(userpreference.HasUserWith(user.ID(userID)))

	// The stored row, read at most once and only when a rule below needs it, so a plain
	// theme or favorites save stays a single UPDATE.
	var stored *ent.UserPreference
	loadStored := func() (*ent.UserPreference, error) {
		if stored != nil {
			return stored, nil
		}
		row, err := tx.UserPreference.Query().
			Where(userpreference.HasUserWith(user.ID(userID))).
			Only(ctx)
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			d.logger.Error(i18n.T1("query.failed", "Entity", "user preferences"), zap.Error(err))
			return nil, err
		}
		stored = row
		return stored, nil
	}

	// Only write the default model when the caller actually supplied one. default_model is
	// a required foreign key, so writing the zero UUID unconditionally turned a request that
	// merely omitted the field into a constraint violation and a 500 — the guard below ran
	// after the value had already been staged, so it gated the permission check but not the
	// write. Absent now means "leave the current default alone", matching how theme
	// already behaves.
	//
	// Only a change is validated and written. Every caller sends the whole record back (PUT
	// replaces it), so the stored default rides along with a theme, favorites or personality
	// save; re-validating it then meant that once that model was retired, or the account lost
	// experimental access, every other preference save failed too (#347).
	if prefs.DefaultModelID != uuid.Nil {
		stored, err := loadStored()
		if err != nil {
			return nil, err
		}
		if stored.DefaultModel == nil || prefs.DefaultModelID != *stored.DefaultModel {
			if err := d.assertUserCanUseModel(ctx, tx, userID, prefs.DefaultModelID); err != nil {
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, err
			}
			update.SetModelID(prefs.DefaultModelID).SetDefaultModel(prefs.DefaultModelID)
		}
	}

	if prefs.Theme == "dark" || prefs.Theme == "light" || prefs.Theme == "system" {
		update.SetTheme(userpreference.Theme(prefs.Theme))
	}

	if prefs.DefaultPersonalityID != uuid.Nil {
		update.SetPersonalityID(prefs.DefaultPersonalityID)
	} else {
		update.ClearPersonality()
	}

	// Favorites use nil-vs-empty rather than a sentinel: a nil slice means the caller
	// omitted the field and the stored list is left alone, while an empty non-nil slice
	// is an explicit "remove all favorites". JSON gives us that distinction for free
	// (absent/null decode to nil, [] decodes to an empty slice), which is why this field
	// does not need the pointer treatment the scalar fields above would require.
	if prefs.FavoriteModelIDs != nil {
		update.SetFavoriteModelIds(prefs.FavoriteModelIDs)
	}

	// Absent ("") leaves the stored vendor alone, like theme. Unlike theme, an unknown
	// value is refused rather than dropped: the caller is picking a vendor and should
	// hear that the pick did not take.
	//
	// The default vendor over an unset (NULL) column also leaves it unset. Reads report
	// NULL as the default, and every caller writes the whole record back, so without
	// this a theme or favorites save stored "openai" for a user who never chose and
	// pinned them to it if the default vendor ever changes (#347). An explicit pick of
	// the default resolves the same as NULL today, so one rule covers both.
	if prefs.ImageGenerationProvider != "" {
		p := userpreference.ImageGenerationProvider(prefs.ImageGenerationProvider)
		if err := userpreference.ImageGenerationProviderValidator(p); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, fmt.Errorf("%w: unknown image_generation_provider %q", ErrInvalidRequestBody, prefs.ImageGenerationProvider)
		}
		keepUnset := false
		if string(p) == string(models.DefaultImageGenerationProvider) {
			stored, err := loadStored()
			if err != nil {
				return nil, err
			}
			keepUnset = stored.ImageGenerationProvider == nil
		}
		if !keepUnset {
			update.SetImageGenerationProvider(p)
		}
	}

	// Turn settings (ADR 0x026): an omitted field is left alone, "" clears it.
	if err := prefs.TurnSettingValues.Validate(); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequestBody, err)
	}
	if prefs.ReasoningEffort != nil {
		setReasoningEffort[userpreference.ReasoningEffort, userpreference.AgentLoop](update, prefs.ReasoningEffort)
	}
	if prefs.AgentLoop != nil {
		setAgentLoop[userpreference.ReasoningEffort, userpreference.AgentLoop](update, prefs.AgentLoop)
	}

	_, err = update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "user preferences"), zap.Error(err))
		return nil, err
	}

	updatedPrefs, err := tx.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		WithUser().
		WithModel().
		WithPersonality().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T("user_prefs.get_updated_failed"), zap.Error(err))
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toUserPreferencesModel(updatedPrefs), nil
}

// SetDefaultPersonality makes personalityID the user's default personality and writes nothing
// else. Used for the auto-default when a user creates their first personality: going through
// GetUserPreferences + UpdateUserPreferences instead echoed every other preference back in a
// second transaction, which could undo a concurrent change and stored the default image
// vendor for a user who never chose one (#347). ErrPersonalityNotFound unless the user owns it.
func (d *Datastore) SetDefaultPersonality(ctx context.Context, userID, personalityID uuid.UUID) error {
	owned, err := d.dbClient.Personality.Query().
		Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
		return err
	}
	if !owned {
		return ErrPersonalityNotFound
	}
	if _, err := d.dbClient.UserPreference.Update().
		Where(userpreference.HasUserWith(user.ID(userID))).
		SetPersonalityID(personalityID).
		Save(ctx); err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "user preferences"), zap.Error(err))
		return err
	}
	return nil
}
