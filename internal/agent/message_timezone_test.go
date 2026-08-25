package agent

import (
	"testing"
	"time"
)

func TestFormatUserMessageWithTime_UsesTimezoneWhenValid(t *testing.T) {
	t.Parallel()

	// 2024-01-15 15:04:05Z
	base := time.Date(2024, 1, 15, 15, 4, 5, 0, time.UTC)
	got := formatUserMessageWithTime(base, "America/Los_Angeles", "hello")

	// PST (UTC-8) in January.
	want := "[sys:2024-01-15T07:04:05-08:00] hello"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatUserMessageWithTime_FallsBackToUTC(t *testing.T) {
	t.Parallel()

	base := time.Date(2024, 1, 15, 15, 4, 5, 0, time.UTC)

	gotInvalid := formatUserMessageWithTime(base, "Not/A_Timezone", "msg")
	wantInvalid := "[sys:2024-01-15T15:04:05Z] msg"
	if gotInvalid != wantInvalid {
		t.Fatalf("invalid tz: got %q, want %q", gotInvalid, wantInvalid)
	}

	gotEmpty := formatUserMessageWithTime(base, "", "msg")
	if gotEmpty != wantInvalid {
		t.Fatalf("empty tz: got %q, want %q", gotEmpty, wantInvalid)
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
