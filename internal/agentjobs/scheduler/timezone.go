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
	lastTZ := lastRunTimezone(j)
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

// lastRunTimezone is the timezone the job last ran in. Runs recorded before last_run_timezone
// existed ran in the job's own timezone.
func lastRunTimezone(j models.AgentJob) string {
	if j.LastRunTimezone != nil && strings.TrimSpace(*j.LastRunTimezone) != "" {
		return strings.TrimSpace(*j.LastRunTimezone)
	}
	return strings.TrimSpace(j.Timezone)
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

// maxTimezoneShift bounds how far back lostSlotOnMove looks: no two IANA zones are more than 26
// hours apart.
const maxTimezoneShift = 26 * time.Hour

// lostSlotOnMove reports whether moving a cron job from the timezone it last ran in to the one it
// runs in now cost it a run. A slot is lost when it has already passed in the new timezone, had not
// come up yet in the old one, and had not run. That happens after moving east: 8am Los Angeles ran
// yesterday, the user signs in from New York at 9am, and today's 8am has passed in New York while
// 8am Los Angeles (11am New York) never fires. Several lost slots (an hourly job) still count once.
// Signing in late loses nothing: by then the old schedule has run the slot.
func lostSlotOnMove(j models.AgentJob, now time.Time) bool {
	if j.ScheduleType != models.AgentJobScheduleTypeCron || j.Schedule == nil {
		return false
	}
	tz := j.ScheduleTimezone()
	loc := loadScheduleLocation(tz)
	cutoff, ok := ranSlotCutoff(j, tz, loc)
	if !ok {
		return false
	}
	lastLoc := loadScheduleLocation(lastRunTimezone(j))
	trigger, err := quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loc)
	if err != nil {
		return false
	}

	from := cutoff
	if earliest := now.Add(-maxTimezoneShift); earliest.After(from) {
		from = earliest
	}
	next, err := trigger.NextFireTime(from.UnixNano())
	for i := 0; err == nil && next <= now.UnixNano() && i < maxSkippedSlots; i++ {
		wall := time.Unix(0, next).In(loc)
		inOldZone := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), lastLoc)
		if inOldZone.After(now) {
			return true
		}
		next, err = trigger.NextFireTime(next)
	}
	return false
}
