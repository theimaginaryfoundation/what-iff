package memory

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// exportRateLimiter is a simple in-process per-user sliding-window rate limiter
// for the memory export endpoint. It records the timestamps of recent allowed
// calls per user and rejects requests that exceed limit within window.
//
// The buckets map grows at most one entry per distinct authenticated user that
// has called the endpoint, which is expected to be small. Entries are pruned on
// each call so old data does not accumulate indefinitely.
type exportRateLimiter struct {
	mu      sync.Mutex
	buckets map[uuid.UUID][]time.Time
	limit   int
	window  time.Duration
}

func newExportRateLimiter(limit int, window time.Duration) *exportRateLimiter {
	return &exportRateLimiter{
		buckets: make(map[uuid.UUID][]time.Time),
		limit:   limit,
		window:  window,
	}
}

// allow returns (true, 0) when the request is permitted, or (false, retryAfter)
// when the user has exceeded the limit. retryAfter is the duration until the
// oldest in-window call expires and the user may retry.
func (l *exportRateLimiter) allow(userID uuid.UUID) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	// Retain only timestamps within the current window.
	prev := l.buckets[userID]
	valid := prev[:0]
	for _, t := range prev {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.limit {
		retryAfter = valid[0].Add(l.window).Sub(now)
		l.buckets[userID] = valid
		return false, retryAfter
	}

	l.buckets[userID] = append(valid, now)
	return true, 0
}
