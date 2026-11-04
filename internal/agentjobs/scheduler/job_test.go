package scheduler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
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

func TestDeriveStatusAndErrorText_RecurringScheduleFailurePausesWithMarker(t *testing.T) {
	bad := "not a cron"
	job := models.AgentJob{
		ScheduleType: models.AgentJobScheduleTypeCron,
		Schedule:     &bad,
		Timezone:     "UTC",
		Status:       models.AgentJobStatusActive,
	}
	_, nextErr := computeNextRunAt(job, time.Now().UTC())
	if nextErr == nil {
		t.Fatal("expected an invalid cron schedule to fail computeNextRunAt")
	}
	scheduleErrText := ScheduleErrorMarker + " " + nextErr.Error()

	status, errText := deriveStatusAndErrorText(&job, nil, scheduleErrText)
	if status == nil || *status != models.AgentJobStatusPaused {
		t.Fatalf("expected recurring job to be paused, got %v", status)
	}
	if !strings.HasPrefix(errText, ScheduleErrorMarker) || !strings.Contains(errText, nextErr.Error()) {
		t.Fatalf("expected last_error to start with the marker and carry the reason, got %q", errText)
	}

	// A run error is kept ahead of the schedule error; the marker must stay findable.
	_, errText = deriveStatusAndErrorText(&job, errors.New("agent blew up"), scheduleErrText)
	if !strings.HasPrefix(errText, "agent blew up") || !strings.Contains(errText, ScheduleErrorMarker) {
		t.Fatalf("expected run error followed by the marked schedule error, got %q", errText)
	}
}

// A run that failed for want of a provider key records that plainly, not the
// transport error wrapping it.
func TestDeriveStatusAndErrorText_MissingKeyIsStatedPlainly(t *testing.T) {
	job := models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron, Status: models.AgentJobStatusActive}
	runErr := fmt.Errorf("OpenAI API call failed: %w", &url.Error{
		Op: "Post", URL: "https://api.openai.com/v1/responses",
		Err: &providerkeys.NotConfiguredError{Provider: models.ModelProviderOpenAI},
	})

	_, errText := deriveStatusAndErrorText(&job, runErr, "")
	if errText != "No OpenAI API key is set." {
		t.Fatalf("expected the missing key stated plainly, got %q", errText)
	}
}

// A run stopped from its thread records that it was stopped, not the cancellation error. A run
// cancelled for another reason (a worker shutting down) is still a failure.
func TestDeriveStatusAndErrorText_StoppedRun(t *testing.T) {
	job := models.AgentJob{ScheduleType: models.AgentJobScheduleTypeCron, Status: models.AgentJobStatusActive}
	runErr := fmt.Errorf("%w: OpenAI API call failed", agent.ErrTurnStopped)

	status, errText := deriveStatusAndErrorText(&job, runErr, "")
	if status != nil {
		t.Fatalf("expected a recurring job's status unchanged, got %v", *status)
	}
	if errText != StoppedRunText {
		t.Fatalf("expected the stop recorded plainly, got %q", errText)
	}

	_, errText = deriveStatusAndErrorText(&job, fmt.Errorf("OpenAI API call failed: %w", context.Canceled), "")
	if errText == StoppedRunText {
		t.Fatal("a run cancelled without a Stop must not be recorded as stopped")
	}

	// A stopped one-off job is done, not failed; one cancelled without a Stop still fails.
	oneOff := models.AgentJob{ScheduleType: models.AgentJobScheduleTypeAt, Status: models.AgentJobStatusActive}
	status, errText = deriveStatusAndErrorText(&oneOff, runErr, "")
	if status == nil || *status != models.AgentJobStatusComplete {
		t.Fatalf("expected a stopped one-off job complete, got %v", status)
	}
	if errText != StoppedRunText {
		t.Fatalf("expected the stop recorded plainly, got %q", errText)
	}
	status, _ = deriveStatusAndErrorText(&oneOff, fmt.Errorf("OpenAI API call failed: %w", context.Canceled), "")
	if status == nil || *status != models.AgentJobStatusFailed {
		t.Fatalf("expected a one-off job cancelled without a Stop failed, got %v", status)
	}
}
