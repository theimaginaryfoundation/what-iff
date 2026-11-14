package sharelink

import (
	"context"
	"time"
)

// The unlock limits (ADR 0x051 §4), per address across all shares.
const (
	// attemptsPerMinute is how many unlocks an address may try in a minute, right or wrong.
	attemptsPerMinute = 10
	// failuresBeforeBan wrong passcodes within failureWindow ban the address.
	failuresBeforeBan = 5
	failureWindow     = 15 * time.Minute
	// firstBan is how long a ban lasts; repeatBan, for one that follows another within repeatWindow.
	firstBan     = time.Hour
	repeatBan    = 24 * time.Hour
	repeatWindow = 24 * time.Hour
	// keepAttempts is how long attempts are kept: no limit looks further back.
	keepAttempts = 24 * time.Hour
)

// gateStore is what the limits are kept in: the database, so every server enforces the same ones.
type gateStore interface {
	RecordShareUnlockAttempt(ctx context.Context, addressKey string, failed bool, at time.Time, keep time.Duration) error
	CountShareUnlockAttempts(ctx context.Context, addressKey string, since time.Time, failedOnly bool) (int, error)
	ShareBan(ctx context.Context, addressKey string) (until time.Time, strikes int, err error)
	SetShareBan(ctx context.Context, addressKey string, until time.Time, strikes int) error
}

// waitBeforeUnlock is how long an address must wait before it may try a passcode: until its ban
// ends, or the minute's limit has room; zero when it may try now.
func waitBeforeUnlock(ctx context.Context, s gateStore, addr string, now time.Time) (time.Duration, error) {
	until, _, err := s.ShareBan(ctx, addr)
	if err != nil {
		return 0, err
	}
	if until.After(now) {
		return until.Sub(now), nil
	}
	n, err := s.CountShareUnlockAttempts(ctx, addr, now.Add(-time.Minute), false)
	if err != nil {
		return 0, err
	}
	if n >= attemptsPerMinute {
		return time.Minute, nil
	}
	return 0, nil
}

// recordUnlock records a try, and bans the address when it was the failure too many. It returns
// how long the address is now banned for (zero when it is not) and how many wrong passcodes it
// has left before it is.
func recordUnlock(ctx context.Context, s gateStore, addr string, failed bool, now time.Time) (ban time.Duration, left int, err error) {
	if err := s.RecordShareUnlockAttempt(ctx, addr, failed, now, keepAttempts); err != nil {
		return 0, 0, err
	}
	if !failed {
		return 0, failuresBeforeBan, nil
	}
	// Failures before the last ban ended were counted for it.
	until, strikes, err := s.ShareBan(ctx, addr)
	if err != nil {
		return 0, 0, err
	}
	since := now.Add(-failureWindow)
	if until.After(since) {
		since = until
	}
	n, err := s.CountShareUnlockAttempts(ctx, addr, since, true)
	if err != nil {
		return 0, 0, err
	}
	if n < failuresBeforeBan {
		return 0, failuresBeforeBan - n, nil
	}
	ban = firstBan
	next := 1
	if strikes > 0 && now.Sub(until) < repeatWindow {
		ban, next = repeatBan, strikes+1
	}
	if err := s.SetShareBan(ctx, addr, now.Add(ban), next); err != nil {
		return 0, 0, err
	}
	return ban, 0, nil
}
