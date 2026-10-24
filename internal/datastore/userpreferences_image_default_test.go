package datastore

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Reads report an unset image vendor as the default ("openai") and every caller PUTs the whole
// record back, so a plain theme or favorites save used to store "openai" for a user who never
// chose, pinning them to it if the default vendor ever changes (#347).

func TestUpdateUserPreferences_EchoedDefaultVendorLeavesItUnset(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*user_preferences.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "image_generation_provider"}).AddRow(uuid.NewString(), nil))
	mock.ExpectExec("UPDATE .*user_preferences.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .*user_preferences.*").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme:                   "light",
		ImageGenerationProvider: string(models.DefaultImageGenerationProvider),
	})

	updates := capturedUpdates(captured)
	require.Contains(t, updates, "theme", "the rest of the save still goes through")
	require.NotContains(t, updates, "image_generation_provider", "an echoed default over NULL stays NULL")
	// The column stays NULL, and the GET that follows (the same conversion) still reports the
	// default vendor, so callers see no difference.
	require.Equal(t, "openai", toUserPreferencesModel(&ent.UserPreference{}).ImageGenerationProvider)
}

func TestUpdateUserPreferences_ExplicitOtherVendorIsStoredOverUnset(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	// A non-default vendor needs no look at the stored row.
	expectUpdateUserPreferences(mock)

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{ImageGenerationProvider: "google"})

	require.Contains(t, capturedUpdates(captured), "image_generation_provider")
}

func TestUpdateUserPreferences_DefaultVendorOverAnotherPickIsStored(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	// Moving back from a stored "google" to "openai" is a real change and is written.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .*user_preferences.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "image_generation_provider"}).AddRow(uuid.NewString(), "google"))
	mock.ExpectExec("UPDATE .*user_preferences.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .*user_preferences.*").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{ImageGenerationProvider: "openai"})

	require.Contains(t, capturedUpdates(captured), "image_generation_provider")
}

func TestSetDefaultPersonality_WritesOnlyThePersonality(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	userID, personalityID := uuid.New(), uuid.New()
	mock.ExpectQuery("SELECT .*personalities.*").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(personalityID.String()))
	mock.ExpectExec("UPDATE .*user_preferences.*").WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, ds.SetDefaultPersonality(context.Background(), userID, personalityID))
	require.NoError(t, mock.ExpectationsWereMet())

	updates := capturedUpdates(captured)
	require.Contains(t, updates, "`default_personality`")
	for _, column := range []string{"default_model", "theme", "image_generation_provider", "favorite_model_ids"} {
		require.NotContains(t, updates, column, "the auto-default must not echo %s", column)
	}
}

func TestSetDefaultPersonality_RefusesAPersonalityTheUserDoesNotOwn(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	mock.ExpectQuery("SELECT .*personalities.*").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	err := ds.SetDefaultPersonality(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, ErrPersonalityNotFound)
	require.Empty(t, capturedUpdates(captured))
}
