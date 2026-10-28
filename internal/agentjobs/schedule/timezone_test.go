package schedule

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }
func cronPtr(expr string) *string    { return &expr }
func followingJob(cron, effective string) models.AgentJob {
	return models.AgentJob{
		ID:                 uuid.New(),
		UserID:             uuid.New(),
		ScheduleType:       models.AgentJobScheduleTypeCron,
		Schedule:           cronPtr(cron),
		Timezone:           "America/New_York",
		FollowUserTimezone: true,
		EffectiveTimezone:  effective,
		Status:             models.AgentJobStatusActive,
	}
}

func TestLostSlotOnMove(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	la := mustLoc(t, "America/Los_Angeles")

	tests := []struct {
		name      string
		cron      string
		effective string
		follows   bool
		lastRunAt *time.Time
		lastRunTZ *string
		now       time.Time
		want      bool
	}{
		{
			name: "east, signed in before the old zone's slot: 8am New York was lost",
			// 8am LA ran yesterday; at 9am New York 8am there has passed and 8am LA (11am New York) is to come.
			cron: "0 0 8 ? * *", effective: "America/New_York", follows: true,
			lastRunAt: timePtr(time.Date(2026, 10, 21, 8, 0, 4, 0, la)), lastRunTZ: strPtr("America/Los_Angeles"),
			now:  time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want: true,
		},
		{
			name: "east, signed in late: the old schedule already ran today's slot",
			cron: "0 0 8 ? * *", effective: "America/New_York", follows: true,
			lastRunAt: timePtr(time.Date(2026, 10, 22, 8, 0, 4, 0, la)), lastRunTZ: strPtr("America/Los_Angeles"),
			now:  time.Date(2026, 10, 22, 14, 0, 0, 0, ny),
			want: false,
		},
		{
			name: "east, signed in before today's slot in the new zone too: nothing lost",
			cron: "0 0 8 ? * *", effective: "America/New_York", follows: true,
			lastRunAt: timePtr(time.Date(2026, 10, 21, 8, 0, 4, 0, la)), lastRunTZ: strPtr("America/Los_Angeles"),
			now:  time.Date(2026, 10, 22, 7, 0, 0, 0, ny),
			want: false,
		},
		{
			name: "west never loses a slot",
			cron: "0 0 8 ? * *", effective: "America/Los_Angeles", follows: true,
			lastRunAt: timePtr(time.Date(2026, 10, 22, 8, 0, 4, 0, ny)), lastRunTZ: strPtr("America/New_York"),
			now:  time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want: false,
		},
		{
			name: "east, hourly: the hours lost to the move count once",
			// 11am LA ran; at 11:30 LA (2:30pm New York) noon-2pm New York have passed but noon LA is to come.
			cron: "0 0 9-17 ? * *", effective: "America/New_York", follows: true,
			lastRunAt: timePtr(time.Date(2026, 10, 22, 11, 0, 2, 0, la)), lastRunTZ: strPtr("America/Los_Angeles"),
			now:  time.Date(2026, 10, 22, 14, 30, 0, 0, ny),
			want: true,
		},
		{
			name: "same timezone as the last run",
			cron: "0 0 8 ? * *", effective: "America/New_York", follows: true,
			lastRunAt: timePtr(time.Date(2026, 10, 21, 8, 0, 4, 0, ny)), lastRunTZ: strPtr("America/New_York"),
			now:  time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want: false,
		},
		{
			name: "a job that has never run",
			cron: "0 0 8 ? * *", effective: "America/New_York", follows: true,
			now:  time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want: false,
		},
		{
			name: "a job that does not follow the user, edited into another zone",
			cron: "0 0 8 ? * *", effective: "America/New_York", follows: false,
			lastRunAt: timePtr(time.Date(2026, 10, 21, 8, 0, 4, 0, la)), lastRunTZ: strPtr("America/Los_Angeles"),
			now:  time.Date(2026, 10, 22, 9, 0, 0, 0, ny),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := followingJob(tt.cron, tt.effective)
			j.FollowUserTimezone = tt.follows
			j.LastRunAt, j.LastRunTimezone = tt.lastRunAt, tt.lastRunTZ
			if got := LostSlotOnMove(j, tt.now); got != tt.want {
				t.Fatalf("LostSlotOnMove = %v, want %v", got, tt.want)
			}
		})
	}
}

// stuckTrigger fires at fixed times, then repeats the last one (a trigger that stopped advancing).
type stuckTrigger struct{ times []int64 }

func (s *stuckTrigger) NextFireTime(prev int64) (int64, error) {
	for _, t := range s.times {
		if t > prev {
			return t, nil
		}
	}
	return s.times[len(s.times)-1], nil
}
func (s *stuckTrigger) Description() string { return "stuck" }

func TestSlotGuardTrigger_FailsInsteadOfFiringARanSlot(t *testing.T) {
	// A trigger that stops advancing before the cutoff is an error, not a fire time at or before it.
	guard := &slotGuardTrigger{inner: &stuckTrigger{times: []int64{10, 20}}, notAfter: 100}
	if _, err := guard.NextFireTime(0); err == nil || !strings.Contains(err.Error(), "stopped advancing") {
		t.Fatalf("expected a stopped-advancing error, got %v", err)
	}

	// A dense schedule that never gets past the cutoff within the limit is an error too.
	every5m, err := quartz.NewCronTrigger("0 */5 * ? * *")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	far := &slotGuardTrigger{inner: every5m, notAfter: start.Add(time.Duration(maxSkippedSlots+10) * 5 * time.Minute).UnixNano()}
	if _, err := far.NextFireTime(start.UnixNano()); err == nil {
		t.Fatal("expected an error once the guard passes over maxSkippedSlots fire times")
	}

	// Within the limit it returns the first fire time after the cutoff.
	near := &slotGuardTrigger{inner: every5m, notAfter: start.Add(time.Hour).UnixNano()}
	got, err := near.NextFireTime(start.UnixNano())
	if err != nil || time.Unix(0, got).UTC() != start.Add(time.Hour+5*time.Minute) {
		t.Fatalf("NextFireTime = %s, %v; want %s", time.Unix(0, got).UTC(), err, start.Add(time.Hour+5*time.Minute))
	}
}

type fakeNextRunStore struct{ writes []time.Time }

func (f *fakeNextRunStore) SetAgentJobNextRunAt(_ context.Context, _, _ uuid.UUID, next *time.Time) (*models.AgentJob, error) {
	if next == nil {
		return nil, errors.New("unexpected clear")
	}
	f.writes = append(f.writes, *next)
	return &models.AgentJob{NextRunAt: next}, nil
}

func TestStoreNextRun(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	la := mustLoc(t, "America/Los_Angeles")
	now := time.Date(2026, 10, 22, 6, 0, 0, 0, ny)

	// A recurring job now running in Los Angeles: its stored New York next run is replaced.
	j := followingJob("0 0 8 ? * *", "America/Los_Angeles")
	j.NextRunAt = timePtr(time.Date(2026, 10, 22, 8, 0, 0, 0, ny))
	store := &fakeNextRunStore{}
	got, err := StoreNextRun(context.Background(), store, &j, now)
	if err != nil || len(store.writes) != 1 || !store.writes[0].Equal(time.Date(2026, 10, 22, 8, 0, 0, 0, la)) {
		t.Fatalf("writes = %v, err = %v; want 8:00 Los Angeles", store.writes, err)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(store.writes[0]) {
		t.Fatalf("returned job next run = %v, want the stored one", got.NextRunAt)
	}

	// Already right, one-off, or paused: nothing is written.
	for _, mutate := range []func(*models.AgentJob){
		func(j *models.AgentJob) { j.EffectiveTimezone = "America/New_York" },
		func(j *models.AgentJob) { j.ScheduleType = models.AgentJobScheduleTypeAt },
		func(j *models.AgentJob) { j.Status = models.AgentJobStatusPaused },
	} {
		j := followingJob("0 0 8 ? * *", "America/Los_Angeles")
		j.NextRunAt = timePtr(time.Date(2026, 10, 22, 8, 0, 0, 0, ny))
		mutate(&j)
		store := &fakeNextRunStore{}
		if _, err := StoreNextRun(context.Background(), store, &j, now); err != nil || len(store.writes) != 0 {
			t.Fatalf("writes = %v, err = %v; want none", store.writes, err)
		}
	}
}
