package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlausibleTime(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"recent", now.Add(-time.Hour), true},
		{"floor", EarliestPlausibleTime, true},
		{"within skew", now.Add(MaxFutureSkew), true},
		{"zero", time.Time{}, false},
		{"unix epoch", time.Unix(0, 0), false},
		{"milliseconds read as seconds", time.Unix(now.UnixMilli(), 0), false},
		{"past skew", now.Add(MaxFutureSkew + time.Second), false},
		{"negative year", time.Date(-50, 1, 1, 0, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, PlausibleTime(tc.t, now))
		})
	}

	fallback := now.Add(-2 * time.Hour)
	require.Equal(t, fallback, PlausibleTimeOr(time.Unix(0, 0), fallback, now))
	require.Equal(t, now, PlausibleTimeOr(now, fallback, now))
}

func TestJSONSafeTimeAlwaysEncodes(t *testing.T) {
	ok := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		t    time.Time
		want time.Time
	}{
		{"in range is unchanged", ok, ok},
		{"zero is unchanged", time.Time{}, time.Time{}},
		{"year 50000", time.Unix(time.Now().UnixMilli(), 0), maxJSONTime},
		{"year -1", time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC), minJSONTime},
		// The year is checked in t's own zone, as MarshalJSON does: 23:30 UTC on 31 Dec 9999 is
		// already the year 10000 one hour east.
		{"year 10000 only in its own zone", time.Date(9999, 12, 31, 23, 30, 0, 0, time.UTC).In(time.FixedZone("UTC+1", 3600)), maxJSONTime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := JSONSafeTime(tc.t)
			require.True(t, tc.want.Equal(got), "got %v, want %v", got, tc.want)
			_, err := json.Marshal(got)
			require.NoError(t, err)
		})
	}
}

func TestJSONSafeTimePtr(t *testing.T) {
	require.Nil(t, JSONSafeTimePtr(nil))

	ok := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	require.Same(t, &ok, JSONSafeTimePtr(&ok), "a safe time is returned as is")

	bad := time.Date(12000, 1, 1, 0, 0, 0, 0, time.UTC)
	got := JSONSafeTimePtr(&bad)
	require.True(t, maxJSONTime.Equal(*got))
	require.Equal(t, 12000, bad.Year(), "the caller's value is not modified")
}
