package scheduler

import (
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestBuildTrigger_RunOnce(t *testing.T) {
	now := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)
	runAt := now.Add(1 * time.Hour)

	j := models.AgentJob{
		ScheduleType: models.AgentJobScheduleTypeAt,
		RunAt:        &runAt,
		Timezone:     "UTC",
		Status:       models.AgentJobStatusActive,
	}

	trigger, err := buildTrigger(j, now)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}

	next, err := trigger.NextFireTime(now.UnixNano())
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}

	got := time.Unix(0, next).UTC()
	if !got.Equal(runAt) {
		t.Fatalf("expected %s, got %s", runAt.Format(time.RFC3339Nano), got.Format(time.RFC3339Nano))
	}
}

func TestComputeNextRunAt_CronUTC(t *testing.T) {
	cron := "0 0 8 ? * *"
	after := time.Date(2026, 2, 23, 7, 0, 0, 0, time.UTC)

	j := models.AgentJob{
		ScheduleType: models.AgentJobScheduleTypeCron,
		Schedule:     &cron,
		Timezone:     "UTC",
		Status:       models.AgentJobStatusActive,
	}

	next, err := computeNextRunAt(j, after)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if next == nil {
		t.Fatalf("expected non-nil next run time")
	}
	if !next.After(after) {
		t.Fatalf("expected next > after (%s), got %s", after.Format(time.RFC3339), next.Format(time.RFC3339))
	}
}
