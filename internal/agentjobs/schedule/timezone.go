package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// This file holds how a recurring job's schedule reads when its timezone moves (ADR 0x027). It is
// pure, so the scheduler, the job handlers and the agent's job tools all compute the same times.

// maxSkippedSlots bounds how many fire times a slot guard passes over. The cutoff is at most a
// day or so ahead and schedules run at most every 5 minutes, so real cases stay far below this.
const maxSkippedSlots = 10000

// maxTimezoneShift bounds how far back LostSlotOnMove looks: no two IANA zones are more than 26
// hours apart.
const maxTimezoneShift = 26 * time.Hour

// loadLocation loads an IANA timezone, falling back to UTC when it is empty or unknown.
func loadLocation(tz string) *time.Location {
	if tz = strings.TrimSpace(tz); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil && loaded != nil {
			return loaded
		}
	}
	return time.UTC
}

// CronTrigger builds the quartz trigger for a cron job in the timezone its schedule runs in now.
// For a job that follows the user and has moved since it last ran, the trigger skips the time
// slots the job already ran, but only those that would come before its next regular run in the
// zone it left: 8am in New York has run, the user moves to Los Angeles, and 8am there later that
// same day (before 8am New York tomorrow) does not run again. An hourly job that ran at 10:00 New
// York runs at 8:00 Los Angeles (11:00 New York), so it keeps running hourly.
func CronTrigger(j models.AgentJob) (quartz.Trigger, error) {
	if j.Schedule == nil || strings.TrimSpace(*j.Schedule) == "" {
		return nil, fmt.Errorf("schedule is required for schedule_type=cron")
	}
	tz := j.ScheduleTimezone()
	loc := loadLocation(tz)
	trigger, err := quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loc)
	if err != nil {
		return nil, err
	}
	if cutoff, ok := ranSlotCutoff(j, tz, loc); ok {
		notAfter := cutoff.UnixNano()
		if next, ok := nextRunInOldZone(j); ok && next.UnixNano()-1 < notAfter {
			notAfter = next.UnixNano() - 1
		}
		return &slotGuardTrigger{inner: trigger, notAfter: notAfter}, nil
	}
	return trigger, nil
}

// nextRunInOldZone is when the job would have run next had it stayed in the zone it last ran in.
// A slot that already ran is skipped only before then, so a move never leaves the job silent for
// longer than its own interval just because the new zone's times carry labels that ran.
func nextRunInOldZone(j models.AgentJob) (time.Time, bool) {
	if j.LastRunAt == nil || j.Schedule == nil {
		return time.Time{}, false
	}
	old, err := quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loadLocation(lastRunTimezone(j)))
	if err != nil {
		return time.Time{}, false
	}
	next, err := old.NextFireTime(j.LastRunAt.UnixNano())
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, next), true
}

// NextRunAt is a job's next scheduled run after the given time: nil for a job that is not active
// or not recurring (a one-off job's next run is its run_at).
func NextRunAt(j models.AgentJob, after time.Time) (*time.Time, error) {
	if j.Status != models.AgentJobStatusActive {
		return nil, nil
	}
	switch j.ScheduleType {
	case models.AgentJobScheduleTypeAt:
		return nil, nil
	case models.AgentJobScheduleTypeCron:
		trigger, err := CronTrigger(j)
		if err != nil {
			return nil, err
		}
		nextNs, err := trigger.NextFireTime(after.UTC().UnixNano())
		if err != nil {
			return nil, err
		}
		next := time.Unix(0, nextNs).UTC()
		return &next, nil
	default:
		return nil, fmt.Errorf("unsupported schedule_type %q", j.ScheduleType)
	}
}

// NextRunStore stores a job's next run; *datastore.Datastore satisfies it.
type NextRunStore interface {
	SetAgentJobNextRunAt(ctx context.Context, userID, id uuid.UUID, nextRunAt *time.Time) (*models.AgentJob, error)
}

// StoreNextRun recomputes an active recurring job's next run in the timezone it runs in now and
// stores it when it changed, returning the job as it then stands. Other jobs are returned as they
// are: their next run does not depend on the timezone they follow. A schedule that cannot be read
// is left to the scheduler, which pauses the job and says why; only a failed write is an error.
func StoreNextRun(ctx context.Context, store NextRunStore, j *models.AgentJob, now time.Time) (*models.AgentJob, error) {
	if j == nil || j.ScheduleType != models.AgentJobScheduleTypeCron || j.Status != models.AgentJobStatusActive {
		return j, nil
	}
	next, err := NextRunAt(*j, now)
	if err != nil || next == nil || (j.NextRunAt != nil && j.NextRunAt.Equal(*next)) {
		return j, nil
	}
	return store.SetAgentJobNextRunAt(ctx, j.UserID, j.ID, next)
}

// LostSlotOnMove reports whether moving a job that follows the user from the timezone it last ran
// in to the one it runs in now cost it a run. A slot is lost when it has already passed in the new
// timezone, had not come up yet in the old one, and had not run. That happens after moving east:
// 8am Los Angeles ran yesterday, the user signs in from New York at 9am, and today's 8am has
// passed in New York while 8am Los Angeles (11am New York) never fires. Several lost slots (an
// hourly job) still count once. Signing in late loses nothing: by then the old schedule has run
// the slot.
func LostSlotOnMove(j models.AgentJob, now time.Time) bool {
	if j.ScheduleType != models.AgentJobScheduleTypeCron || j.Schedule == nil {
		return false
	}
	tz := j.ScheduleTimezone()
	loc := loadLocation(tz)
	cutoff, ok := ranSlotCutoff(j, tz, loc)
	if !ok {
		return false
	}
	lastLoc := loadLocation(lastRunTimezone(j))
	trigger, err := quartz.NewCronTriggerWithLoc(strings.TrimSpace(*j.Schedule), loc)
	if err != nil {
		return false
	}

	from := cutoff
	if earliest := now.Add(-maxTimezoneShift); earliest.After(from) {
		from = earliest
	}
	prev := from.UnixNano()
	for i := 0; i < maxSkippedSlots; i++ {
		next, err := trigger.NextFireTime(prev)
		if err != nil || next <= prev || next > now.UnixNano() {
			return false
		}
		wall := time.Unix(0, next).In(loc)
		inOldZone := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), lastLoc)
		if inOldZone.After(now) {
			return true
		}
		prev = next
	}
	return false
}

// ranSlotCutoff returns the wall-clock time of the job's last run, read in the timezone it ran in
// and placed in loc. Fire times at or before it are slots that already ran. It applies only to a
// job that follows the user (a job pinned to its own timezone moves only when someone edits it,
// and then runs exactly as edited), and reports false when the job has not run or still runs in
// the timezone it last ran in.
func ranSlotCutoff(j models.AgentJob, tz string, loc *time.Location) (time.Time, bool) {
	if !j.FollowUserTimezone || j.LastRunAt == nil {
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

// NextFireTime passes over fire times at or before notAfter. It fails rather than return one of
// them, so a schedule that never gets past the cutoff (or stops advancing) is reported instead of
// running a slot twice.
func (t *slotGuardTrigger) NextFireTime(prev int64) (int64, error) {
	next, err := t.inner.NextFireTime(prev)
	for i := 0; err == nil && next <= t.notAfter; i++ {
		if i == maxSkippedSlots {
			return 0, fmt.Errorf("schedule did not get past the slots it already ran after %d fire times", maxSkippedSlots)
		}
		last := next
		if next, err = t.inner.NextFireTime(next); err == nil && next <= last {
			return 0, fmt.Errorf("schedule stopped advancing at %s", time.Unix(0, last).UTC().Format(time.RFC3339))
		}
	}
	return next, err
}

func (t *slotGuardTrigger) Description() string {
	return t.inner.Description()
}
