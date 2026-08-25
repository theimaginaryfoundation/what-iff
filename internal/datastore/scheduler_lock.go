package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const schedulerLockQueryTimeout = 2 * time.Second

// SchedulerLeaderLock represents a held distributed scheduler lock.
type SchedulerLeaderLock interface {
	// IsHealthy verifies the lock connection is still alive.
	IsHealthy(ctx context.Context) (bool, error)
	// Release unlocks and closes the lock session.
	Release(ctx context.Context) error
	// Key returns the advisory lock key.
	Key() int64
}

type pgSchedulerLeaderLock struct {
	conn   *sql.Conn
	key    int64
	logger *zap.Logger

	mu       sync.Mutex
	released bool
}

// TryAcquireSchedulerLeaderLock attempts to acquire a Postgres advisory lock on a dedicated
// DB session. The returned bool indicates whether leadership was acquired.
func (d *Datastore) TryAcquireSchedulerLeaderLock(ctx context.Context, lockKey int64) (SchedulerLeaderLock, bool, error) {
	if d.sqlDB == nil {
		return nil, false, fmt.Errorf("scheduler lock: sql DB is not configured")
	}

	lockCtx, cancel := context.WithTimeout(ctx, schedulerLockQueryTimeout)
	defer cancel()

	conn, err := d.sqlDB.Conn(lockCtx)
	if err != nil {
		return nil, false, fmt.Errorf("scheduler lock: acquire connection: %w", err)
	}

	var acquired bool
	if err := conn.QueryRowContext(lockCtx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("scheduler lock: try advisory lock: %w", err)
	}

	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}

	d.logger.Info("scheduler leader lock acquired", zap.Int64("lock_key", lockKey))
	return &pgSchedulerLeaderLock{
		conn:   conn,
		key:    lockKey,
		logger: d.logger,
	}, true, nil
}

func (l *pgSchedulerLeaderLock) Key() int64 {
	return l.key
}

func (l *pgSchedulerLeaderLock) IsHealthy(ctx context.Context) (bool, error) {
	l.mu.Lock()
	if l.released || l.conn == nil {
		l.mu.Unlock()
		return false, nil
	}
	conn := l.conn
	l.mu.Unlock()

	checkCtx, cancel := context.WithTimeout(ctx, schedulerLockQueryTimeout)
	defer cancel()

	var one int
	if err := conn.QueryRowContext(checkCtx, "SELECT 1").Scan(&one); err != nil {
		// Health-check failure means this lock session should not be reused.
		// Close the pinned connection so we do not keep a broken session around.
		if closeErr := l.markReleasedAndCloseConn(); closeErr != nil {
			return false, fmt.Errorf("scheduler lock: health check failed: %w (close failed: %v)", err, closeErr)
		}
		return false, fmt.Errorf("scheduler lock: health check failed: %w", err)
	}
	return true, nil
}

func (l *pgSchedulerLeaderLock) Release(ctx context.Context) error {
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	conn := l.conn
	l.conn = nil
	l.mu.Unlock()
	if conn == nil {
		return nil
	}

	releaseCtx, cancel := context.WithTimeout(ctx, schedulerLockQueryTimeout)
	defer cancel()

	var unlocked bool
	err := conn.QueryRowContext(releaseCtx, "SELECT pg_advisory_unlock($1)", l.key).Scan(&unlocked)
	closeErr := conn.Close()

	if err != nil {
		return fmt.Errorf("scheduler lock: advisory unlock query failed: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("scheduler lock: connection close failed: %w", closeErr)
	}
	if !unlocked {
		l.logger.Warn("scheduler leader lock release returned false", zap.Int64("lock_key", l.key))
	}

	l.logger.Info("scheduler leader lock released", zap.Int64("lock_key", l.key))
	return nil
}

func (l *pgSchedulerLeaderLock) markReleasedAndCloseConn() error {
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	conn := l.conn
	l.conn = nil
	l.mu.Unlock()

	if conn == nil {
		return nil
	}
	return conn.Close()
}
