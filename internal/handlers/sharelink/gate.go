package sharelink

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	// keepAttempts is how long attempts, and bans that have ended, are kept: no limit looks further back.
	keepAttempts = 24 * time.Hour
)

// gateStore is what the limits are kept in: the database, so every server enforces the same ones.
type gateStore interface {
	RecordShareUnlockAttempt(ctx context.Context, addressKey string, at time.Time, keep time.Duration) (uuid.UUID, error)
	ClearShareUnlockFailure(ctx context.Context, id uuid.UUID) error
	DiscardShareUnlockAttempt(ctx context.Context, id uuid.UUID) error
	ShareUnlockAttempts(ctx context.Context, addressKey string, since time.Time, failedOnly bool) (n int, oldest time.Time, err error)
	ShareBan(ctx context.Context, addressKey string) (until time.Time, strikes int, err error)
	SetShareBan(ctx context.Context, addressKey string, until time.Time, strikes int) error
}

// admit records a try at a passcode, as a failure until it is known to be right, and says how
// long the address must wait (zero: it may try now). Recording first is what holds the limits
// under concurrency: every request counts the ones that arrived before it, finished or not.
func admit(ctx context.Context, s gateStore, addr string, now time.Time) (id uuid.UUID, wait time.Duration, err error) {
	if until, _, err := s.ShareBan(ctx, addr); err != nil || until.After(now) {
		return uuid.Nil, until.Sub(now), err
	}
	if id, err = s.RecordShareUnlockAttempt(ctx, addr, now, keepAttempts); err != nil {
		return uuid.Nil, 0, err
	}
	// A ban written by a request that finished meanwhile holds for this one too.
	until, strikes, err := s.ShareBan(ctx, addr)
	if err != nil || until.After(now) {
		return id, until.Sub(now), err
	}
	n, oldest, err := s.ShareUnlockAttempts(ctx, addr, now.Add(-time.Minute), false)
	if err != nil {
		return id, 0, err
	}
	if n > attemptsPerMinute {
		// Refused unchecked: not a try at all, so it neither counts as a guess nor keeps the
		// limit from easing.
		if err := s.DiscardShareUnlockAttempt(ctx, id); err != nil {
			return id, 0, err
		}
		return id, max(time.Second, oldest.Add(time.Minute).Sub(now)), nil
	}
	failures, _, err := s.ShareUnlockAttempts(ctx, addr, failuresSince(until, now), true)
	if err != nil {
		return id, 0, err
	}
	if failures > failuresBeforeBan {
		// The ban is being written by the try that reached the limit; tries racing it wait too.
		ban, err := banFor(ctx, s, addr, until, strikes, now)
		return id, ban, err
	}
	return id, 0, nil
}

// conclude records what a try admitted turned out to be: a right passcode is no failure; a wrong
// one that is the failure too many bans the address. It returns how long the address is banned for
// (zero when it is not) and how many wrong passcodes it has left before it is.
func conclude(ctx context.Context, s gateStore, addr string, id uuid.UUID, right bool, now time.Time) (ban time.Duration, left int, err error) {
	if right {
		return 0, failuresBeforeBan, s.ClearShareUnlockFailure(ctx, id)
	}
	until, strikes, err := s.ShareBan(ctx, addr)
	if err != nil {
		return 0, 0, err
	}
	failures, _, err := s.ShareUnlockAttempts(ctx, addr, failuresSince(until, now), true)
	if err != nil {
		return 0, 0, err
	}
	if failures < failuresBeforeBan {
		return 0, failuresBeforeBan - failures, nil
	}
	ban, err = banFor(ctx, s, addr, until, strikes, now)
	return ban, 0, err
}

// failuresSince is where the failure window starts: failures before the last ban ended were
// counted for it.
func failuresSince(banEnded, now time.Time) time.Time {
	since := now.Add(-failureWindow)
	if banEnded.After(since) && !banEnded.After(now) {
		since = banEnded
	}
	return since
}

// banFor bans the address: for an hour, or a day when its last ban ended within a day.
func banFor(ctx context.Context, s gateStore, addr string, lastEnded time.Time, strikes int, now time.Time) (time.Duration, error) {
	ban, next := firstBan, 1
	if strikes > 0 && now.Sub(lastEnded) < repeatWindow {
		ban, next = repeatBan, strikes+1
	}
	return ban, s.SetShareBan(ctx, addr, now.Add(ban), next)
}
