package agent

import (
	"strings"
	"testing"
	"time"
)

func TestFormatUserMessageWithTime_UsesTimezoneWhenValid(t *testing.T) {
	t.Parallel()

	// 2024-01-15 15:04:05Z — a Monday.
	base := time.Date(2024, 1, 15, 15, 4, 5, 0, time.UTC)
	got := formatUserMessageWithTime(base, "America/Los_Angeles", "hello")

	// PST (UTC-8) in January; weekday short name is part of the stamp.
	want := "[sys:Mon 2024-01-15 07:04:05 -08:00] hello"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatUserMessageWithTime_FallsBackToUTC(t *testing.T) {
	t.Parallel()

	base := time.Date(2024, 1, 15, 15, 4, 5, 0, time.UTC)
	// Numeric offset (not "Z") so the stamp stays parseable the same way in every zone.
	want := "[sys:Mon 2024-01-15 15:04:05 +00:00] msg"

	gotInvalid := formatUserMessageWithTime(base, "Not/A_Timezone", "msg")
	if gotInvalid != want {
		t.Fatalf("invalid tz: got %q, want %q", gotInvalid, want)
	}

	gotEmpty := formatUserMessageWithTime(base, "", "msg")
	if gotEmpty != want {
		t.Fatalf("empty tz: got %q, want %q", gotEmpty, want)
	}
}

func TestFormatUserMessageWithTime_ZeroTimeReturnsBody(t *testing.T) {
	t.Parallel()

	got := formatUserMessageWithTime(time.Time{}, "America/New_York", "no timestamp please")
	want := "no timestamp please"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatUserMessageWithTime_IncludesWeekdayAndOffset(t *testing.T) {
	t.Parallel()

	// Matches the Vix-prod convention example: Sun 2026-09-13 08:46:51 -04:00 (EDT).
	base := time.Date(2026, 9, 13, 12, 46, 51, 0, time.UTC)
	got := formatUserMessageWithTime(base, "America/New_York", "ping")
	want := "[sys:Sun 2026-09-13 08:46:51 -04:00] ping"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatUserMessageWithTime_DateBoundaryUsesLocalWeekday(t *testing.T) {
	t.Parallel()

	// 2026-09-14 02:00 UTC is still Sunday evening in America/Los_Angeles (PDT, UTC-7).
	base := time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC)
	got := formatUserMessageWithTime(base, "America/Los_Angeles", "boundary")
	want := "[sys:Sun 2026-09-13 19:00:00 -07:00] boundary"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.Contains(got, "Sun ") {
		t.Fatalf("expected local Sunday weekday in %q", got)
	}
}
