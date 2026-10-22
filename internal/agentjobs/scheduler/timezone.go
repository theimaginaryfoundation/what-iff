package scheduler

import (
	"fmt"
	"strings"
	"time"

	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// maxSkippedSlots bounds how many fire times slotGuardTrigger passes over. The cutoff is at most a
// day or so ahead and schedules run at most every 5 minutes, so real cases stay far below this.
const maxSkippedSlots = 10000

// loadScheduleLocation loads an IANA timezone, falling back to UTC when it is empty or unknown.
func loadScheduleLocation(tz string) *time.Location {
	if tz = strings.TrimSpace(tz); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil && loaded != nil {
			return loaded
		}
	}
	return time.UTC
}

// cronTrigger builds the quartz trigger for a cron job in the timezone its schedule runs in now.
// When that timezone differs from the one the job last ran in, the trigger skips the time slots
// the job already ran: 8am in New York has run, the user moves to Los Angeles, and 8am there
// later that same day does not run again.
func cronTrigger(j models.AgentJob) (quartz.Trigger, error) {
	if j.Schedule == nil || strings.TrimSpace(*j.Schedule) == "" {
		return nil, fmt.Errorf("schedule is required for schedule_type=cron")
	}
	tz := j.ScheduleTimezone()
	loc := loadScheduleLocation(tz)
	trigger, err := quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loc)
	if err != nil {
		return nil, err
	}
	if cutoff, ok := ranSlotCutoff(j, tz, loc); ok {
		return &slotGuardTrigger{inner: trigger, notAfter: cutoff.UnixNano()}, nil
	}
	return trigger, nil
}

// ranSlotCutoff returns the wall-clock time of the job's last run, read in the timezone it ran in
// and placed in loc. Fire times at or before it are slots that already ran. It reports false when
// the job has not run or still runs in the timezone it last ran in.
func ranSlotCutoff(j models.AgentJob, tz string, loc *time.Location) (time.Time, bool) {
	if j.LastRunAt == nil {
		return time.Time{}, false
	}
	// Runs recorded before last_run_timezone existed ran in the job's own timezone.
	lastTZ := strings.TrimSpace(j.Timezone)
	if j.LastRunTimezone != nil && strings.TrimSpace(*j.LastRunTimezone) != "" {
		lastTZ = strings.TrimSpace(*j.LastRunTimezone)
	}
	if lastTZ == "" || lastTZ == tz {
		return time.Time{}, false
	}
	lastLoc, err := time.LoadLocation(lastTZ)
	if err != nil {
		return time.Time{}, false
	}
	wall := j.LastRunAt.In(lastLoc)
	return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), loc), true
}

// slotGuardTrigger wraps a trigger and never fires at or before notAfter (Unix nanoseconds).
type slotGuardTrigger struct {
	inner    quartz.Trigger
	notAfter int64
}

func (t *slotGuardTrigger) NextFireTime(prev int64) (int64, error) {
	next, err := t.inner.NextFireTime(prev)
	for i := 0; err == nil && next <= t.notAfter && i < maxSkippedSlots; i++ {
		next, err = t.inner.NextFireTime(next)
	}
	return next, err
}

func (t *slotGuardTrigger) Description() string {
	return t.inner.Description()
}
