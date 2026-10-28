package modeltypes

import (
	"testing"
	"time"
)

func TestTimezoneFlapping(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	change := func(tz string, at time.Time) TimezoneChange { return TimezoneChange{Timezone: tz, At: at} }
	const ny, la, chi = "America/New_York", "America/Los_Angeles", "America/Chicago"

	tests := []struct {
		name    string
		changes []TimezoneChange
		want    bool
	}{
		{"one move", []TimezoneChange{change(la, ago(2))}, false},
		{"a same-day round trip", []TimezoneChange{change(la, ago(10)), change(ny, ago(1))}, false},
		{"a trip with a stop", []TimezoneChange{change(chi, ago(8)), change(la, ago(3))}, false},
		{"three moves through three zones", []TimezoneChange{change(chi, ago(8)), change(la, ago(3)), change("Pacific/Honolulu", ago(1))}, false},
		{"back and forth between two zones", []TimezoneChange{change(la, ago(5)), change(ny, ago(4)), change(la, ago(1))}, true},
		{"four moves", []TimezoneChange{change(chi, ago(9)), change(la, ago(6)), change("Pacific/Honolulu", ago(3)), change("Asia/Tokyo", ago(1))}, true},
		{"back and forth spread over more than a day", []TimezoneChange{change(la, ago(30)), change(ny, ago(26)), change(la, ago(1))}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TimezoneFlapping(tt.changes, now); got != tt.want {
				t.Fatalf("TimezoneFlapping = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidTimezoneName(t *testing.T) {
	for tz, want := range map[string]bool{
		"America/New_York":  true,
		"UTC":               true,
		" Europe/London ":   true,
		"Local":             false,
		"local":             false,
		"Mars/Olympus_Mons": false,
	} {
		if got := ValidTimezoneName(tz); got != want {
			t.Errorf("ValidTimezoneName(%q) = %v, want %v", tz, got, want)
		}
	}
}
