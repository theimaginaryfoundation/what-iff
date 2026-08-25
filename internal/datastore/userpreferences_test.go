package datastore

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// newCapturingMockDatastore is newMockDatastore with a query matcher that records every
// statement Ent issues, sharing its wiring via newDatastoreOverMockDB. UpdateUserPreferences
// decides per field whether to write it at all, so the assertions here are about which
// columns appear in the emitted UPDATE — something a return value cannot show.
//
// Also used by userpreferences_default_model_test.go.
func newCapturingMockDatastore(t *testing.T) (*Datastore, sqlmock.Sqlmock, *[]string, func()) {
	t.Helper()

	captured := &[]string{}
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		*captured = append(*captured, actualSQL)
		return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	})

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)

	ds, cleanup := newDatastoreOverMockDB(t, db)
	return ds, mock, captured, cleanup
}

func TestToUserPreferencesModel_FavoritesNormalisedToEmptySlice(t *testing.T) {
	// A nil column value must surface as [] rather than nil so the field serialises as
	// an empty array. The response schema documents favorite_model_ids as always
	// present, and a nil slice would marshal to null instead.
	got := toUserPreferencesModel(&ent.UserPreference{FavoriteModelIds: nil})

	require.NotNil(t, got.FavoriteModelIDs)
	require.Empty(t, got.FavoriteModelIDs)
}

func TestToUserPreferencesModel_FavoritesPreserveOrder(t *testing.T) {
	stored := []string{"model-c", "model-a", "model-b"}

	got := toUserPreferencesModel(&ent.UserPreference{FavoriteModelIds: stored})

	require.Equal(t, stored, got.FavoriteModelIDs)
}

// expectUpdateUserPreferences primes the mock for one UpdateUserPreferences round trip:
// the UPDATE itself, then the re-read that builds the returned model. The re-read
// deliberately returns no rows: these tests assert on the statements Ent produced, and an
// empty row set short-circuits edge loading without changing the UPDATE under test.
//
// That makes UpdateUserPreferences return a not-found error, so callers below discard the
// result rather than asserting NoError — the error is a property of this harness, not of
// the behaviour under test. Asserting on it would test the mock.
func expectUpdateUserPreferences(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*user_preferences.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .*user_preferences.*").WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

// capturedUpdates returns only the UPDATE statements Ent issued. The re-read that follows
// every update selects every column by name, so asserting against all captured SQL would
// match a column whether or not it was actually written.
func capturedUpdates(captured *[]string) string {
	var updates []string
	for _, q := range *captured {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(q)), "UPDATE") {
			updates = append(updates, q)
		}
	}
	return strings.Join(updates, "\n")
}

func TestUpdateUserPreferences_NilFavoritesLeavesColumnUntouched(t *testing.T) {
	// The read-modify-write callers in internal/handlers/personality send the whole
	// preferences struct back. A caller that never learned about favorites — or any
	// client that omits the field — decodes to a nil slice, which must not clear the
	// user's stored list.
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	expectUpdateUserPreferences(mock)

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme:            "dark",
		FavoriteModelIDs: nil,
	})

	require.NotContains(t, capturedUpdates(captured), "favorite_model_ids",
		"a nil slice means the field was omitted and the stored list must be left alone")
}

func TestUpdateUserPreferences_EmptyFavoritesClearsTheList(t *testing.T) {
	// An empty but non-nil slice is the explicit "remove all favorites" signal, and has
	// to be distinguishable from the omitted case above.
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	expectUpdateUserPreferences(mock)

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme:            "dark",
		FavoriteModelIDs: []string{},
	})

	require.Contains(t, capturedUpdates(captured), "favorite_model_ids",
		"an empty non-nil slice is an explicit clear and must be written")
}

func TestUpdateUserPreferences_PopulatedFavoritesAreWritten(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	expectUpdateUserPreferences(mock)

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme:            "dark",
		FavoriteModelIDs: []string{"model-a", "model-b"},
	})

	require.Contains(t, capturedUpdates(captured), "favorite_model_ids")
}
