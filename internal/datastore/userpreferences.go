package datastore

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/ent"
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

	prefs.LastSeenAnnouncement = e.LastSeenAnnouncement

	// Normalise to a non-nil slice so the field always serialises as [] rather than
	// null. NOT NULL constrains the SQL value, not the JSON inside it — a jsonb column
	// can still hold the literal null, which unmarshals to a nil slice — so this is what
	// actually holds up the "always present" response contract.
	if e.FavoriteModelIds != nil {
		prefs.FavoriteModelIDs = e.FavoriteModelIds
	} else {
		prefs.FavoriteModelIDs = []string{}
	}

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

	// Only write the default model when the caller actually supplied one. default_model is
	// a required foreign key, so writing the zero UUID unconditionally turned a request that
	// merely omitted the field into a constraint violation and a 500 — the guard below ran
	// after the value had already been staged, so it gated the permission check but not the
	// write. Absent now means "leave the current default alone", matching how theme and
	// last_seen_announcement already behave.
	if prefs.DefaultModelID != uuid.Nil {
		if err := d.assertUserCanUseModel(ctx, tx, userID, prefs.DefaultModelID); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
		update.SetModelID(prefs.DefaultModelID).SetDefaultModel(prefs.DefaultModelID)
	}

	if prefs.Theme == "dark" || prefs.Theme == "light" || prefs.Theme == "system" {
		update.SetTheme(userpreference.Theme(prefs.Theme))
	}

	if prefs.DefaultPersonalityID != uuid.Nil {
		update.SetPersonalityID(prefs.DefaultPersonalityID)
	} else {
		update.ClearPersonality()
	}

	if prefs.LastSeenAnnouncement != "" {
		update.SetLastSeenAnnouncement(prefs.LastSeenAnnouncement)
	}

	// Favorites use nil-vs-empty rather than a sentinel: a nil slice means the caller
	// omitted the field and the stored list is left alone, while an empty non-nil slice
	// is an explicit "remove all favorites". JSON gives us that distinction for free
	// (absent/null decode to nil, [] decodes to an empty slice), which is why this field
	// does not need the pointer treatment the scalar fields above would require.
	if prefs.FavoriteModelIDs != nil {
		update.SetFavoriteModelIds(prefs.FavoriteModelIDs)
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
