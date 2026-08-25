package datastore

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestNormalizeModelSubscriptionTier(t *testing.T) {
	t.Parallel()

	tier, err := normalizeModelSubscriptionTier("ultra")
	require.NoError(t, err)
	require.Equal(t, entmodel.SubscriptionTierUltra, tier)

	tier, err = normalizeModelSubscriptionTier(" ULTRA ")
	require.NoError(t, err)
	require.Equal(t, entmodel.SubscriptionTierUltra, tier)

	_, err = normalizeModelSubscriptionTier("flagship")
	require.ErrorIs(t, err, ErrInvalidSubscriptionTier)
}

func newModelMockDatastore(t *testing.T) (*Datastore, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	cleanup := func() {
		_ = client.Close()
		_ = db.Close()
	}

	return ds, mock, cleanup
}

func TestAdminDeleteModelNotFound(t *testing.T) {
	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	id := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*models.*").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := ds.AdminDeleteModel(context.Background(), id)
	require.True(t, errors.Is(err, ErrModelNotFound), "expected ErrModelNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminDeleteModelSuccess(t *testing.T) {
	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	id := uuid.New()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE .*models.*").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectExec("INSERT INTO .*audit_logs.*").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := ds.AdminDeleteModel(context.Background(), id)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminCreateModelRestoresSoftDeletedModel(t *testing.T) {
	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	id := uuid.New()
	req := models.CreateModelRequest{
		Name:        "gpt-5.1",
		DisplayName: "GPT 5.1 (Custom)",
		Description: "custom",
		Provider:    "openai",
		ToolSupport: true,
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*name.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "description", "provider", "tool_support", "deleted"}).
			AddRow(id.String(), req.Name, "Old", "old", "openai", true, true))
	mock.ExpectExec("UPDATE .*models.*").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "description", "provider", "tool_support", "deleted"}).
			AddRow(id.String(), req.Name, req.DisplayName, req.Description, req.Provider, req.ToolSupport, false))
	mock.ExpectCommit()
	mock.ExpectExec("INSERT INTO .*audit_logs.*").
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := ds.AdminCreateModel(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, req.Name, got.Name)
	require.Equal(t, req.DisplayName, got.DisplayName)
	require.Equal(t, req.Provider, got.Provider)
	require.False(t, got.Deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetModelNotFound(t *testing.T) {
	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	id := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "description", "provider", "tool_support", "deleted"}))
	mock.ExpectRollback()

	got, err := ds.GetModel(context.Background(), id)
	require.Nil(t, got)
	require.True(t, errors.Is(err, ErrModelNotFound), "expected ErrModelNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetModelNotFoundRollbackFailure(t *testing.T) {
	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	id := uuid.New()
	rollbackErr := errors.New("rollback failed")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "description", "provider", "tool_support", "deleted"}))
	mock.ExpectRollback().WillReturnError(rollbackErr)

	got, err := ds.GetModel(context.Background(), id)
	require.Nil(t, got)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrModelNotFound), "expected non-ErrModelNotFound when rollback fails")
	require.Contains(t, err.Error(), "rollback failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetModelSuccess(t *testing.T) {
	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	id := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.* WHERE .*id.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "display_name", "description", "provider", "tool_support", "deleted"}).
			AddRow(id.String(), "gpt-5.1", "GPT-5.1", "desc", "openai", true, false))
	mock.ExpectCommit()

	got, err := ds.GetModel(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, id, got.ID)
	require.Equal(t, "gpt-5.1", got.Name)
	require.False(t, got.Deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFilterVisibleModels_NoProvidersCurrentlyExperimental(t *testing.T) {
	t.Parallel()

	all := []*models.Model{
		{Name: "gpt-4o-mini", Provider: string(models.ModelProviderOpenAI)},
		{Name: "gemini-3.5", Provider: string(models.ModelProviderGoogle)},
		{Name: "glm-5.2", Provider: string(models.ModelProviderZAI)},
	}

	// Graduated vendors are no longer classified as experimental, so the
	// catalog is unfiltered whether or not the per-user flag is set.
	require.Len(t, filterVisibleModels(all, false), 3)
	require.Len(t, filterVisibleModels(all, true), 3)
}

func TestAssertUserCanUseModelTx_AllowsGraduatedProvidersWithoutFlag(t *testing.T) {
	t.Parallel()

	ds, mock, cleanup := newModelMockDatastore(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New()
	modelID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM .*models.*").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "display_name", "description", "provider", "tool_support", "deleted",
		}).AddRow(modelID.String(), "gemini-3.5", "Gemini", "", "google", true, false))
	// No user lookup: non-experimental models short-circuit before the flag check.

	tx, err := ds.dbClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	err = assertUserCanUseModelTx(ctx, tx, userID, modelID)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
