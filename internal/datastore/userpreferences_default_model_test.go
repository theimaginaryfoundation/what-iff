package datastore

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// These tests share newCapturingMockDatastore, expectUpdateUserPreferences and
// capturedUpdates with userpreferences_test.go. They were originally duplicated here under
// different names so two in-flight PRs touching UpdateUserPreferences would not collide;
// both have landed, so the copies are gone.

func TestUpdateUserPreferences_OmittedDefaultModelIsNotWritten(t *testing.T) {
	// default_model is a required foreign key, so writing the zero UUID because the caller
	// omitted the field turned an otherwise valid request into a constraint violation and a
	// 500. An absent value has to leave the stored default alone instead.
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	expectUpdateUserPreferences(mock)

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme: "dark",
	})

	require.NotContains(t, capturedUpdates(captured), "default_model",
		"omitting default_model_id must preserve the stored value rather than writing the zero UUID")
}

func TestUpdateUserPreferences_SuppliedDefaultModelIsStillWritten(t *testing.T) {
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	modelID := uuid.New()

	mock.ExpectBegin()
	// assertUserCanUseModel runs first when a model is actually supplied.
	mock.ExpectQuery("SELECT .*").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(modelID.String()))
	mock.ExpectExec("UPDATE .*user_preferences.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .*user_preferences.*").WillReturnRows(sqlmock.NewRows([]string{"id"}))

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme:          "dark",
		DefaultModelID: modelID,
	})

	require.Contains(t, capturedUpdates(captured), "default_model",
		"a supplied default_model_id must still be written")
}

func TestUpdateUserPreferences_OmittedDefaultModelSkipsPermissionCheck(t *testing.T) {
	// The entitlement check is only meaningful for a model the caller actually asked for.
	// Running it against the zero UUID would reject the request for the wrong reason.
	ds, mock, captured, cleanup := newCapturingMockDatastore(t)
	defer cleanup()

	expectUpdateUserPreferences(mock)

	_, _ = ds.UpdateUserPreferences(context.Background(), uuid.New(), models.UserPreferences{
		Theme: "light",
	})

	for _, q := range *captured {
		require.NotContains(t, strings.ToLower(q), "from `models`",
			"no model lookup should happen when default_model_id was omitted")
	}
}
