package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func strPtr(s string) *string { return &s }

// followJob is an active cron job homed in New York that follows its owner.
func followJob(cron, effectiveTZ string, lastRunAt *time.Time, lastRunTZ *string) models.AgentJob {
	return models.AgentJob{
		ID:                 uuid.New(),
		UserID:             uuid.New(),
		ScheduleType:       models.AgentJobScheduleTypeCron,
		Schedule:           &cron,
		Timezone:           "America/New_York",
		FollowUserTimezone: true,
		EffectiveTimezone:  effectiveTZ,
		Status:             models.AgentJobStatusActive,
		LastRunAt:          lastRunAt,
		LastRunTimezone:    lastRunTZ,
	}
}

func nextFire(t *testing.T, j models.AgentJob, after time.Time) time.Time {
	t.Helper()
	next, err := computeNextRunAt(j, after)
	if err != nil {
		t.Fatalf("computeNextRunAt: %v", err)
	}
	if next == nil {
		t.Fatal("computeNextRunAt: expected a next run")
	}
	// buildTrigger must agree with the stored preview.
	trigger, err := buildTrigger(j, after)
	if err != nil {
		t.Fatalf("buildTrigger: %v", err)
	}
	ns, err := trigger.NextFireTime(after.UnixNano())
	if err != nil {
		t.Fatalf("NextFireTime: %v", err)
	}
	if got := time.Unix(0, ns).UTC(); !got.Equal(*next) {
		t.Fatalf("buildTrigger fires at %s, computeNextRunAt says %s", got, next)
	}
	return *next
}

func TestScheduleTimezone_FollowsTheUser(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	la := mustLoc(t, "America/Los_Angeles")
	daily8 := "0 0 8 ? * *"

	tests := []struct {
		name      string
		effective string
		lastRunAt *time.Time
		lastRunTZ *string
		now       time.Time
		want      time.Time
	}{
		{
			name: "runs in the effective timezone",
			// 06:00 in LA, nothing run yet today.
			effective: "America/Los_Angeles",
			now:       time.Date(2026, 10, 22, 6, 0, 0, 0, la),
			want:      time.Date(2026, 10, 22, 8, 0, 0, 0, la),
		},
		{
			name: "moving west does not re-run a slot that ran in the old timezone",
			// 8am ran in New York; at 9am New York the user is now in LA. 8am LA (11am New York)
			// that day is the same slot, so the next run is 8am LA tomorrow.
			effective: "America/Los_Angeles",
			lastRunAt: ptrTime(time.Date(2026, 10, 22, 8, 0, 4, 0, ny)),
			lastRunTZ: strPtr("America/New_York"),
			now:       time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want:      time.Date(2026, 10, 23, 8, 0, 0, 0, la),
		},
		{
			name:      "a run recorded before last_run_timezone existed ran in the job's own timezone",
			effective: "America/Los_Angeles",
			lastRunAt: ptrTime(time.Date(2026, 10, 22, 8, 0, 4, 0, ny)),
			now:       time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want:      time.Date(2026, 10, 23, 8, 0, 0, 0, la),
		},
		{
			name: "moving east still runs a slot the job has not run",
			// Ran 8am LA yesterday; at 7am New York the user is home. 8am New York today runs.
			effective: "America/New_York",
			lastRunAt: ptrTime(time.Date(2026, 10, 21, 8, 0, 4, 0, la)),
			lastRunTZ: strPtr("America/Los_Angeles"),
			now:       time.Date(2026, 10, 22, 7, 0, 0, 0, ny),
			want:      time.Date(2026, 10, 22, 8, 0, 0, 0, ny),
		},
		{
			name:      "same timezone as the last run is unaffected",
			effective: "America/New_York",
			lastRunAt: ptrTime(time.Date(2026, 10, 21, 8, 0, 4, 0, ny)),
			lastRunTZ: strPtr("America/New_York"),
			now:       time.Date(2026, 10, 22, 7, 0, 0, 0, ny),
			want:      time.Date(2026, 10, 22, 8, 0, 0, 0, ny),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := followJob(daily8, tt.effective, tt.lastRunAt, tt.lastRunTZ)
			if got := nextFire(t, j, tt.now); !got.Equal(tt.want) {
				t.Fatalf("next run = %s, want %s", got.In(tt.want.Location()), tt.want)
			}
		})
	}
}

func TestScheduleTimezone_SkipsEveryRanSlotOfAnHourlyJob(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	la := mustLoc(t, "America/Los_Angeles")
	// Hourly 9-17. 10am ran in New York; at 10:30 New York (7:30 LA) the user is in LA. 9am and
	// 10am LA already ran as New York hours, so 11am LA is next.
	j := followJob("0 0 9-17 ? * *", "America/Los_Angeles",
		ptrTime(time.Date(2026, 10, 22, 10, 0, 2, 0, ny)), strPtr("America/New_York"))

	got := nextFire(t, j, time.Date(2026, 10, 22, 10, 30, 0, 0, ny))
	if want := time.Date(2026, 10, 22, 11, 0, 0, 0, la); !got.Equal(want) {
		t.Fatalf("next run = %s, want %s", got.In(la), want)
	}
}

func TestFingerprint_ChangesWithTheEffectiveTimezone(t *testing.T) {
	home := followJob("0 0 8 ? * *", "America/New_York", nil, nil)
	away := home
	away.EffectiveTimezone = "America/Los_Angeles"
	if fingerprint(home) == fingerprint(away) {
		t.Fatal("expected a different fingerprint once the job runs in another timezone")
	}

	// A job loaded without a derived effective timezone keeps the stored one.
	legacy := home
	legacy.EffectiveTimezone = ""
	if fingerprint(legacy) != fingerprint(home) {
		t.Fatal("expected an underived effective timezone to fall back to the job's timezone")
	}
}

func TestReconcile_StoresTheNextRunWhenAFollowingJobMoves(t *testing.T) {
	ds := &fakeDatastoreProvider{}
	m, err := NewManager(ds, nil, zap.NewNop(), Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.sched = &fakeScheduler{}

	following := followJob("0 0 8 ? * *", "America/New_York", nil, nil)
	pinned := following
	pinned.ID = uuid.New()
	pinned.FollowUserTimezone = false
	ds.jobs = []*models.AgentJob{&following, &pinned}

	// Startup scheduling does not rewrite stored next runs.
	m.reconcileOnce(context.Background())
	if len(ds.nextRunWrites) != 0 {
		t.Fatalf("expected no next-run writes on first schedule, got %d", len(ds.nextRunWrites))
	}

	// The owner's timezone changes: only the following job moves, and its next run is stored.
	movedFollowing, movedPinned := following, pinned
	movedFollowing.EffectiveTimezone = "America/Los_Angeles"
	movedPinned.Timezone = "America/Los_Angeles"
	ds.jobs = []*models.AgentJob{&movedFollowing, &movedPinned}
	m.reconcileOnce(context.Background())

	next, ok := ds.nextRunWrites[following.ID]
	if !ok {
		t.Fatal("expected the following job's next run to be stored")
	}
	la := mustLoc(t, "America/Los_Angeles")
	if l := next.In(la); l.Hour() != 8 || l.Minute() != 0 {
		t.Fatalf("stored next run = %s, want 8:00 in Los Angeles", l)
	}
	if _, ok := ds.nextRunWrites[pinned.ID]; ok {
		t.Fatal("expected no write for a job that does not follow the user (its editor stores next_run_at)")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
