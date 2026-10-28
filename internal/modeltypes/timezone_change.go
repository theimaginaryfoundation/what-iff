package modeltypes

import (
	"strings"
	"time"
)

// TimezoneChange is one change of a user's browser-reported timezone (ADR 0x027), kept for a day
// so the server can tell travel from devices that keep disagreeing.
type TimezoneChange struct {
	// Timezone is the zone the user's timezone changed to.
	Timezone string    `json:"timezone"`
	At       time.Time `json:"at"`
}

// TimezoneFlapWindow is how far back TimezoneFlapping looks.
const TimezoneFlapWindow = 24 * time.Hour

// RecentTimezoneChanges keeps the changes within TimezoneFlapWindow of now, oldest first.
func RecentTimezoneChanges(changes []TimezoneChange, now time.Time) []TimezoneChange {
	out := make([]TimezoneChange, 0, len(changes))
	for _, c := range changes {
		if now.Sub(c.At) < TimezoneFlapWindow {
			out = append(out, c)
		}
	}
	return out
}

// TimezoneFlapping reports whether a day's changes look like devices disagreeing rather than
// travel: four or more changes, or three that go back and forth between two zones
// (New York → Los Angeles → New York → Los Angeles). A trip, even one with a stop on the way
// (New York → Chicago → Los Angeles) or a same-day return (New York → Los Angeles → New York),
// stays below both.
func TimezoneFlapping(changes []TimezoneChange, now time.Time) bool {
	recent := RecentTimezoneChanges(changes, now)
	if len(recent) >= 4 {
		return true
	}
	if len(recent) < 3 {
		return false
	}
	zones := map[string]bool{}
	for _, c := range recent {
		zones[c.Timezone] = true
	}
	return len(zones) <= 2
}

// ValidTimezoneName reports whether tz names a timezone the app accepts: one time.LoadLocation
// knows, other than "Local", which LoadLocation accepts but which means the server's own zone.
func ValidTimezoneName(tz string) bool {
	if strings.EqualFold(strings.TrimSpace(tz), "Local") {
		return false
	}
	_, err := time.LoadLocation(strings.TrimSpace(tz))
	return err == nil
}
