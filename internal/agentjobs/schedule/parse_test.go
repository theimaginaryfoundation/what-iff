package schedule

import (
	"strings"
	"testing"
	"time"

	"github.com/reugn/go-quartz/quartz"
)

func TestValidateCronMinimumInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cron       string
		minMinutes int
		wantErr    bool
	}{
		{
			name:       "passes every five minutes",
			cron:       "0 0/5 * * * ?",
			minMinutes: 5,
			wantErr:    false,
		},
		{
			name:       "fails every two minutes wildcard",
			cron:       "0 */2 * * * ?",
			minMinutes: 5,
			wantErr:    true,
		},
		{
			name:       "fails complex monthly window with two minute cadence",
			cron:       "0 10-40/2 8 ? * 3#1",
			minMinutes: 5,
			wantErr:    true,
		},
		{
			name:       "passes sparse one minute value",
			cron:       "0 10 8 ? * 3#1",
			minMinutes: 5,
			wantErr:    false,
		},
		{
			name:       "fails wrap around minute list",
			cron:       "0 58,0 * * * ?",
			minMinutes: 5,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			trigger, err := quartz.NewCronTriggerWithLoc(tc.cron, time.UTC)
			if err != nil {
				t.Fatalf("failed to build trigger: %v", err)
			}
			err = validateCronMinimumInterval(trigger, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), tc.minMinutes)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "minimum recurring interval is 5 minutes") {
				t.Fatalf("expected minimum interval message, got %q", err.Error())
			}
		})
	}
}
