package datastore

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestTryAcquireSchedulerLeaderLock_AcquiredAndReleased(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	lockKey := int64(80920031)

	mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
		WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))

	lock, acquired, err := ds.TryAcquireSchedulerLeaderLock(context.Background(), lockKey)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, lock)

	mock.ExpectQuery("SELECT 1").
		WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))
	healthy, err := lock.IsHealthy(context.Background())
	require.NoError(t, err)
	require.True(t, healthy)

	mock.ExpectQuery("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))
	require.NoError(t, lock.Release(context.Background()))

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTryAcquireSchedulerLeaderLock_NotAcquired(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	lockKey := int64(80920031)

	mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
		WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

	lock, acquired, err := ds.TryAcquireSchedulerLeaderLock(context.Background(), lockKey)
	require.NoError(t, err)
	require.False(t, acquired)
	require.Nil(t, lock)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerLeaderLockRelease_Idempotent(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	lockKey := int64(80920031)
	mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
		WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectQuery("SELECT pg_advisory_unlock\\(\\$1\\)").
		WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

	lock, acquired, err := ds.TryAcquireSchedulerLeaderLock(context.Background(), lockKey)
	require.NoError(t, err)
	require.True(t, acquired)

	require.NoError(t, lock.Release(context.Background()))
	// Second release is a no-op and should not hit DB again.
	require.NoError(t, lock.Release(context.Background()))

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerLeaderLock_IsHealthyFailureClosesSession(t *testing.T) {
	ds, mock, cleanup := newMockDatastore(t)
	defer cleanup()

	lockKey := int64(80920031)
	mock.ExpectQuery("SELECT pg_try_advisory_lock\\(\\$1\\)").
		WithArgs(lockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectQuery("SELECT 1").
		WillReturnError(errors.New("connection broken"))

	lock, acquired, err := ds.TryAcquireSchedulerLeaderLock(context.Background(), lockKey)
	require.NoError(t, err)
	require.True(t, acquired)

	healthy, err := lock.IsHealthy(context.Background())
	require.False(t, healthy)
	require.Error(t, err)

	// Connection/session already closed by health check failure; release is no-op.
	require.NoError(t, lock.Release(context.Background()))

	require.NoError(t, mock.ExpectationsWereMet())
}
