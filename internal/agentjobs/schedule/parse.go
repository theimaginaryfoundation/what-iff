package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/reugn/go-quartz/quartz"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.uber.org/zap"
)

type openAICaller interface {
	CallWithRetry(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error)
}

// agentJobScheduleOutput is the structured output shape for the schedule interpreter.
// All fields are required strings so the schema has no oneOf (OpenAI does not support oneOf).
// For schedule_type=at: run_at is set (ISO8601), schedule is empty.
// For schedule_type=cron: schedule is set (Quartz cron), run_at is empty.
type agentJobScheduleOutput struct {
	ScheduleType string `json:"schedule_type"` // "cron" or "at"; empty when error_message is set
	Schedule     string `json:"schedule"`      // Quartz cron expression; empty for one-off
	RunAt        string `json:"run_at"`        // ISO8601 date-time; empty for recurring
	Timezone     string `json:"timezone"`
	HumanSummary string `json:"human_summary"`
	ErrorMessage string `json:"error_message"` // Non-empty when input cannot be converted into a valid schedule.
}

var scheduleSchema = initScheduleSchema()

const recurringPreviewRunCount = 5
const minRecurringIntervalMinutes = 5
const minRecurringIntervalValidationRuns = 64

func initScheduleSchema() map[string]interface{} {
	schema := provider.GenerateSchema[agentJobScheduleOutput]()
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		if st, ok := props["schedule_type"].(map[string]interface{}); ok {
			st["enum"] = []string{string(models.AgentJobScheduleTypeCron), string(models.AgentJobScheduleTypeAt)}
		}
		// Intentionally do NOT force "format: date-time" for run_at at schema level.
		// For recurring schedules (schedule_type=cron), run_at must be an empty string.
		// We validate run_at as RFC3339 in code for schedule_type=at.
	}
	return schema
}

// Parse converts a natural language schedule into a validated schedule spec and preview.
func Parse(ctx context.Context, caller openAICaller, userID uuid.UUID, scheduleInput string, timezone string, now time.Time) (*models.AgentJobSchedulePreview, error) {
	scheduleInput = strings.TrimSpace(scheduleInput)
	if scheduleInput == "" {
		return nil, fmt.Errorf("schedule_input is required")
	}

	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil || loc == nil {
		return nil, fmt.Errorf("invalid timezone %q", timezone)
	}

	nowInLoc := now.In(loc)

	instructions := strings.Join([]string{
		"You are a scheduling interpreter for an AI assistant.",
		"Convert the user's schedule request into either:",
		"- schedule_type=at with a concrete run_at timestamp, or",
		"- schedule_type=cron with a Quartz cron expression (seconds minutes hours day-of-month month day-of-week [year optional]).",
		"",
		"Rules:",
		"- Use the provided timezone for interpreting ambiguous phrases like 'tomorrow morning'.",
		"- Decide schedule_type based on whether the user is asking for a ONE-OFF vs RECURRING schedule. Do NOT choose schedule_type based on the presence of the word 'at'.",
		"- ONE-OFF signals: 'in X minutes/hours', 'tomorrow', 'next <day>', 'at <time>' (without recurrence wording). Use schedule_type=at.",
		"- RECURRING signals: 'every', 'daily', 'each', 'weekly', 'monthly', 'yearly', 'weekdays', 'weekends', 'every X minutes'. Use schedule_type=cron.",
		"- If the user says 'every day at 12:20 PM' that is RECURRING and must be schedule_type=cron (even though it contains 'at').",
		"- For schedule_type=cron, set run_at to empty string and schedule to the Quartz cron expression.",
		"- Minimum recurring interval is 5 minutes. Do not produce schedules that run more frequently than every 5 minutes.",
		"- If the request is invalid or cannot be represented safely (including recurring intervals under 5 minutes), set error_message to a concise user-facing validation message and return empty strings for schedule_type, schedule, run_at, and human_summary.",
		"- If the request is valid, error_message must be an empty string.",
		"- For schedule_type=at, set schedule to empty string and run_at to the ISO8601 timestamp.",
		"- Set timezone exactly to the provided timezone (do not invent a different timezone).",
		"- Provide a concise human_summary of the schedule.",
		"",
		"Examples:",
		"- Input: 'in 1 minute' -> schedule_type=at, run_at=<now+1min ISO8601>, schedule=\"\"",
		"- Input: 'at 12:20 PM' -> schedule_type=at, run_at=<next occurrence ISO8601>, schedule=\"\"",
		"- Input: 'every day at 12:20 PM' -> schedule_type=cron, schedule='0 20 12 ? * *', run_at=\"\"",
		"- Input: 'every morning at 8 AM' -> schedule_type=cron, schedule='0 0 8 ? * *', run_at=\"\"",
		"- Input: 'every 5 minutes' -> schedule_type=cron, schedule='0 0/5 * * * ?', run_at=\"\"",
		"- Input: 'every five minutes' -> schedule_type=cron, schedule='0 0/5 * * * ?', run_at=\"\"",
		"",
		fmt.Sprintf("Now: %s", nowInLoc.Format(time.RFC3339)),
		fmt.Sprintf("Timezone: %s", timezone),
	}, "\n")

	params := responses.ResponseNewParams{
		Model:            "gpt-4.1-nano-2025-04-14",
		SafetyIdentifier: openai.String(userID.String()),
		Temperature:      openai.Float(0.2),
		MaxOutputTokens:  openai.Int(800),
		Instructions:     openai.String(instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(scheduleInput),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:        "AgentJobSchedule",
					Schema:      scheduleSchema,
					Strict:      openai.Bool(true),
					Description: openai.String("Agent job schedule JSON"),
					Type:        "json_schema",
				},
			},
		},
	}

	resp, err := caller.CallWithRetry(telemetry.WithCallPath(ctx, telemetry.CallPathScheduleParse), params)
	if err != nil {
		return nil, fmt.Errorf("failed to interpret schedule: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("schedule interpreter returned nil response")
	}

	raw := strings.TrimSpace(resp.OutputText())
	if raw == "" {
		return nil, fmt.Errorf("schedule interpreter returned empty output")
	}

	var parsed agentJobScheduleOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse schedule JSON: %w", err)
	}

	parsed.ScheduleType = strings.TrimSpace(parsed.ScheduleType)
	parsed.Schedule = strings.TrimSpace(parsed.Schedule)
	parsed.RunAt = strings.TrimSpace(parsed.RunAt)
	parsed.ErrorMessage = strings.TrimSpace(parsed.ErrorMessage)
	// Keep timezone as an input constraint (source of truth) to avoid the model
	// "choosing" a different timezone.
	parsed.Timezone = timezone

	if parsed.ErrorMessage != "" {
		return nil, errors.New(parsed.ErrorMessage)
	}

	preview := models.AgentJobSchedulePreview{
		ScheduleType: models.AgentJobScheduleType(parsed.ScheduleType),
		Timezone:     parsed.Timezone,
		HumanSummary: strings.TrimSpace(parsed.HumanSummary),
	}

	switch models.AgentJobScheduleType(parsed.ScheduleType) {
	case models.AgentJobScheduleTypeAt:
		if parsed.RunAt == "" {
			return nil, fmt.Errorf("schedule interpreter returned invalid one-off schedule (missing run_at)")
		}
		runAt, err := time.Parse(time.RFC3339, parsed.RunAt)
		if err != nil {
			return nil, fmt.Errorf("invalid run_at date-time: %w", err)
		}
		runAt = runAt.UTC()
		if !runAt.After(now) {
			return nil, fmt.Errorf("run_at must be in the future")
		}
		preview.RunAt = &runAt
		preview.NextRuns = []time.Time{runAt}
		return &preview, nil

	case models.AgentJobScheduleTypeCron:
		if parsed.Schedule == "" {
			zap.L().Warn("schedule interpreter returned invalid cron schedule (missing schedule)",
				zap.String("timezone", timezone),
				zap.String("schedule_input", scheduleInput),
				zap.String("raw", raw),
			)
			return nil, fmt.Errorf("schedule interpreter returned invalid recurring schedule (missing cron expression)")
		}

		loc, err := time.LoadLocation(parsed.Timezone)
		if err != nil || loc == nil {
			return nil, fmt.Errorf("invalid timezone %q", parsed.Timezone)
		}

		cron := parsed.Schedule
		trigger, err := quartz.NewCronTriggerWithLoc(cron, loc)
		if err != nil {
			return nil, fmt.Errorf("invalid cron schedule: %w", err)
		}
		if err := validateCronMinimumInterval(trigger, now, minRecurringIntervalMinutes); err != nil {
			return nil, err
		}

		scheduleStr := cron
		preview.Schedule = &scheduleStr

		prev := now.UTC().UnixNano()
		// Return a short preview window for UI/readability without over-fetching.
		nextRuns := make([]time.Time, 0, recurringPreviewRunCount)
		for i := 0; i < recurringPreviewRunCount; i++ {
			nextMs, err := trigger.NextFireTime(prev)
			if err != nil {
				return nil, fmt.Errorf("failed to compute next fire time: %w", err)
			}
			next := time.Unix(0, nextMs).UTC()
			nextRuns = append(nextRuns, next)
			prev = nextMs
		}
		preview.NextRuns = nextRuns
		return &preview, nil

	default:
		return nil, fmt.Errorf("invalid schedule_type %q", parsed.ScheduleType)
	}
}

func validateCronMinimumInterval(trigger quartz.Trigger, start time.Time, minimumMinutes int) error {
	if minimumMinutes <= 0 {
		return nil
	}
	if trigger == nil {
		return fmt.Errorf("invalid cron schedule: nil trigger")
	}

	var minGap time.Duration
	prev := start.UTC().UnixNano()
	seen := 0

	for i := 0; i < minRecurringIntervalValidationRuns; i++ {
		nextNs, err := trigger.NextFireTime(prev)
		if err != nil {
			return fmt.Errorf("invalid cron schedule: failed to compute next fire time: %w", err)
		}
		next := time.Unix(0, nextNs).UTC()

		if seen > 0 {
			gap := next.Sub(time.Unix(0, prev).UTC())
			if minGap == 0 || gap < minGap {
				minGap = gap
			}
		}
		seen++
		prev = nextNs
	}

	if minGap > 0 && minGap < time.Duration(minimumMinutes)*time.Minute {
		minGapMinutes := int(minGap / time.Minute)
		if minGapMinutes < 1 {
			minGapMinutes = 1
		}
		return fmt.Errorf(
			"minimum recurring interval is %d minutes; this schedule can run as often as every %d minutes",
			minimumMinutes,
			minGapMinutes,
		)
	}
	return nil
}
