package datastore

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func newSafetyViolationTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))

	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)

	cleanup := func() {
		_ = client.Close()
		_ = db.Close()
	}
	return ds, cleanup
}

func TestCreateSafetyViolationEventCreateFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
		_ = db.Close()
	}()

	userID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO .*safety_violation_events.*").
		WillReturnError(assertAnError("insert failed"))
	mock.ExpectRollback()

	_, err = ds.CreateSafetyViolationEvent(context.Background(), models.CreateSafetyViolationEventInput{
		Provider:        models.SafetyViolationProviderOpenAI,
		ProviderMessage: "blocked",
		RawError:        "raw",
		UserID:          userID,
		ChatName:        "chat",
		ChatMessageText: "text",
	})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminListSafetyViolationEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
		_ = db.Close()
	}()

	userID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COUNT\\(`safety_violation_events`\\.`id`\\) FROM `safety_violation_events`.*").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT .* FROM `safety_violation_events`.*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "occurred_at", "provider", "provider_message", "raw_error", "chat_name", "chat_message_text", "created_at", "updated_at", "user_safety_violation_events"}).
			AddRow(uuid.New().String(), time.Now(), "openai", "blocked", "raw", "chat", "text", time.Now(), time.Now(), userID.String()))
	mock.ExpectQuery("SELECT .* FROM `users` WHERE `users`\\.`id` IN .*").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "timezone", "status", "created_at", "updated_at"}).
			AddRow(userID.String(), "u1", "u1@example.com", "hash", "UTC", "active", time.Now(), time.Now()))
	mock.ExpectCommit()

	providerFilter := "openai"
	result, err := ds.AdminListSafetyViolationEvents(context.Background(), 1, 10, AdminListSafetyViolationEventsFilters{
		Provider: &providerFilter,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.TotalCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminGetSafetyViolationEventNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	ds, err := NewDatastore(client, db, zap.NewNop(), "12345678901234567890123456789012", nil)
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
		_ = db.Close()
	}()

	mock.ExpectQuery("SELECT .* FROM `safety_violation_events`.*").
		WillReturnRows(sqlmock.NewRows([]string{}))

	_, err = ds.AdminGetSafetyViolationEvent(context.Background(), uuid.New())
	require.ErrorIs(t, err, ErrSafetyViolationEventNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func assertAnError(msg string) error {
	return &mockErr{msg: msg}
}

type mockErr struct {
	msg string
}

func (e *mockErr) Error() string {
	return e.msg
}
