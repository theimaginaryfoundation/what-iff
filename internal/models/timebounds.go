package models

import "time"

// Timestamps that arrive from outside the server (an import file, an account backup, a remote-sync
// peer, a model provider's created_at) are untrusted. A millisecond value read as seconds lands
// around the year 50000, a zero lands in 1970, and either one stored as sent_at or
// last_message_time used to break every response that included it: encoding/json refuses to
// encode a time.Time outside the years 0-9999.
//
// Writes go through PlausibleTimeOr so new data stays inside a sane window. Reads go through
// JSONSafeTime so data that is already bad, or that arrives some other way, cannot break a
// response.

// EarliestPlausibleTime is the oldest time accepted from outside the server. Nothing this app
// stores can predate it, and it sits well clear of the zero and Unix-epoch values that broken
// sources fall back to.
var EarliestPlausibleTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// MaxFutureSkew is how far past now an outside time may be, to allow for clock skew.
const MaxFutureSkew = 24 * time.Hour

// PlausibleTime reports whether t is between EarliestPlausibleTime and now plus MaxFutureSkew.
func PlausibleTime(t, now time.Time) bool {
	return !t.Before(EarliestPlausibleTime) && !t.After(now.Add(MaxFutureSkew))
}

// PlausibleTimeOr returns t when it is plausible (see PlausibleTime) and fallback otherwise.
func PlausibleTimeOr(t, fallback, now time.Time) time.Time {
	if PlausibleTime(t, now) {
		return t
	}
	return fallback
}

var (
	minJSONTime = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	maxJSONTime = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
)

// JSONSafeTime returns t unchanged when encoding/json can encode it, and otherwise clamps it to
// the nearest end of the years 0-9999. The clamped value is in UTC, because the year is checked in
// t's own location, just as time.Time.MarshalJSON checks it.
func JSONSafeTime(t time.Time) time.Time {
	switch y := t.Year(); {
	case y < 0:
		return minJSONTime
	case y > 9999:
		return maxJSONTime
	}
	return t
}

// JSONSafeTimePtr is JSONSafeTime for an optional time. It returns t itself when no clamping is
// needed, and nil for nil.
func JSONSafeTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	if safe := JSONSafeTime(*t); !safe.Equal(*t) {
		return &safe
	}
	return t
}
