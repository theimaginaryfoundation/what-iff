package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/schedule"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// agentJobUpdateStore is the datastore surface update_agent_job uses; *datastore.Datastore
// satisfies it. Every call is scoped to the owner, so a job id that is not theirs is not found.
type agentJobUpdateStore interface {
	jobThreadChats
	GetAgentJob(ctx context.Context, userID, id uuid.UUID) (*models.AgentJob, error)
	UpdateAgentJobTitle(ctx context.Context, userID, id uuid.UUID, title *string) (*models.AgentJob, error)
	UpdateAgentJobPrompt(ctx context.Context, userID, id uuid.UUID, prompt string) (*models.AgentJob, error)
	SetAgentJobChat(ctx context.Context, userID, id uuid.UUID, chatID *uuid.UUID) (*models.AgentJob, error)
	SetAgentJobOverrides(ctx context.Context, userID, id uuid.UUID, patch models.SetAgentJobOverridesPatch) (*models.AgentJob, error)
	SetAgentJobFollowUserTimezone(ctx context.Context, userID, id uuid.UUID, follow bool) (*models.AgentJob, error)
	UpdateAgentJobSchedule(ctx context.Context, userID, id uuid.UUID, scheduleInput string, scheduleType models.AgentJobScheduleType, schedule *string, runAt *time.Time, timezone string, nextRunAt *time.Time) (*models.AgentJob, error)
	UpdateAgentJobStatus(ctx context.Context, userID, id uuid.UUID, status models.AgentJobStatus, lastError string) (*models.AgentJob, error)
	AddAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error
	RemoveAgentJobRitual(ctx context.Context, userID, jobID, ritualID uuid.UUID) error
}

// agentJobUpdater carries what update_agent_job needs, so tests can supply fakes for the store,
// the schedule parser (a model call) and model lookup.
type agentJobUpdater struct {
	store         agentJobUpdateStore
	parseSchedule func(ctx context.Context, userID uuid.UUID, input, timezone string, now time.Time) (*models.AgentJobSchedulePreview, error)
	resolveModel  func(ctx context.Context, ref string) (uuid.UUID, error)
	logger        *zap.Logger
}

// updateAgentJobToolArgs is a patch: a field left out (nil) is left as it is.
type updateAgentJobToolArgs struct {
	JobID         string    `json:"job_id"`
	Title         *string   `json:"title"`
	Prompt        *string   `json:"prompt"`
	ScheduleInput *string   `json:"schedule_input"`
	ModelOverride *string   `json:"model_override"`
	ThreadID      *string   `json:"thread_id"`
	SkillIDs      *[]string `json:"skill_ids"`
	Status        *string   `json:"status"`
	// FollowUserTimezone is set only when the user asks for the job to follow them (or stop).
	FollowUserTimezone *bool `json:"follow_user_timezone"`
}

type updateAgentJobToolResult struct {
	Success      bool     `json:"success"`
	AgentJobID   string   `json:"agent_job_id,omitempty"`
	Updated      []string `json:"updated,omitempty"`
	Title        string   `json:"title,omitempty"`
	Prompt       string   `json:"prompt,omitempty"`
	Schedule     string   `json:"schedule_input,omitempty"`
	HumanSummary string   `json:"human_summary,omitempty"`
	Timezone     string   `json:"timezone,omitempty"`
	// FollowsUserTimezone is true when the job follows the user's current timezone.
	FollowsUserTimezone bool       `json:"follows_user_timezone,omitempty"`
	Status              string     `json:"status,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	ThreadID            string     `json:"thread_id,omitempty"`
	ModelID             string     `json:"model_id,omitempty"`
	SkillIDs            []string   `json:"skill_ids,omitempty"`
	Note                string     `json:"note,omitempty"`
	Error               string     `json:"error,omitempty"`
}

func (a *Agent) updateAgentJobTool(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	u := agentJobUpdater{
		store: a.ds,
		parseSchedule: func(ctx context.Context, userID uuid.UUID, input, tz string, now time.Time) (*models.AgentJobSchedulePreview, error) {
			return schedule.Parse(ctx, a.OpenAIProvider, userID, input, tz, now)
		},
		resolveModel: func(ctx context.Context, ref string) (uuid.UUID, error) {
			m, err := a.resolveModelByReference(ctx, ref, modelResolverOptions{AllowDisplayName: true, AllowPrefixMatch: true})
			if err != nil {
				return uuid.Nil, err
			}
			return m.ID, nil
		},
		logger: a.logger,
	}
	return u.update(ctx, chat, args)
}

// update applies an update_agent_job call. Everything the call asks for is checked first (ids,
// thread, model, schedule), so a bad argument changes nothing; then the fields are written one
// datastore call at a time, like the job editor in the app does. A bad argument is reported to the
// model as an error result, not returned as a Go error.
func (u *agentJobUpdater) update(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	fail := func(msg string) (string, error) {
		return marshalUpdateAgentJobToolResult(updateAgentJobToolResult{Error: msg})
	}
	// Same reasons as create_agent_job: a job runs outside the sandbox and may attach the owner's
	// skills and MCP servers. The tool is not offered in a sandbox; this refuses a call anyway.
	if chat.IsSandboxed() {
		return fail(sandboxedAgentJobNote)
	}
	if !featuregate.IsEntitled(ctx, chat.UserID) {
		return fail("agent jobs are not available for this account")
	}

	var a updateAgentJobToolArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return fail(fmt.Sprintf("invalid arguments: %v", err))
	}
	jobID, err := uuid.Parse(strings.TrimSpace(a.JobID))
	if err != nil {
		return fail("job_id must be a job id from list (kind=\"jobs\")")
	}
	if a.Title == nil && a.Prompt == nil && a.ScheduleInput == nil && a.ModelOverride == nil &&
		a.ThreadID == nil && a.SkillIDs == nil && a.Status == nil && a.FollowUserTimezone == nil {
		return fail("nothing to update: pass at least one field to change (to read a job in full, use list with kind=\"jobs\" and the job's id as filter)")
	}

	userID := chat.UserID
	current, err := u.store.GetAgentJob(ctx, userID, jobID)
	if errors.Is(err, datastore.ErrAgentJobNotFound) {
		return fail("no job with that id; use list (kind=\"jobs\", include_completed=true) to find it")
	}
	if err != nil {
		u.logger.Error("update_agent_job: load job", zap.String("user_id", userID.String()), zap.Error(err))
		return fail("could not load the job; try again")
	}

	// Check every requested change before writing any of them.
	var prompt string
	if a.Prompt != nil {
		if prompt = strings.TrimSpace(*a.Prompt); prompt == "" {
			return fail("prompt cannot be empty")
		}
	}
	var status models.AgentJobStatus
	if a.Status != nil {
		status = models.AgentJobStatus(strings.ToLower(strings.TrimSpace(*a.Status)))
		if status != models.AgentJobStatusActive && status != models.AgentJobStatusPaused {
			return fail("status must be \"active\" or \"paused\"")
		}
	}
	var thread *uuid.UUID
	if a.ThreadID != nil {
		if thread, err = resolveJobThread(ctx, u.store, chat, *a.ThreadID); err != nil {
			if errors.Is(err, errJobThreadLookup) {
				return fail("could not check that conversation; try again")
			}
			return fail(err.Error())
		}
	}
	var overrides models.SetAgentJobOverridesPatch
	if a.ModelOverride != nil {
		overrides.UpdateModel = true
		if ref := strings.TrimSpace(*a.ModelOverride); ref != "" {
			id, err := u.resolveModel(ctx, ref)
			if err != nil {
				return fail("model_override not found: " + ref)
			}
			overrides.ModelID = &id
		}
	}
	var skillIDs []uuid.UUID
	if a.SkillIDs != nil {
		for _, raw := range *a.SkillIDs {
			id, err := uuid.Parse(strings.TrimSpace(raw))
			if err != nil {
				return fail(fmt.Sprintf("%q is not a skill id; use list (kind=\"skills\")", raw))
			}
			skillIDs = append(skillIDs, id)
		}
	}
	var preview *models.AgentJobSchedulePreview
	var scheduleInput string
	if a.ScheduleInput != nil {
		if scheduleInput = strings.TrimSpace(*a.ScheduleInput); scheduleInput == "" {
			return fail("schedule_input cannot be empty")
		}
		tz := current.Timezone
		if clientTZ, ok := middleware.GetClientTimezoneFromContext(ctx); ok && strings.TrimSpace(clientTZ) != "" {
			tz = strings.TrimSpace(clientTZ)
		}
		if tz == "" {
			tz = "UTC"
		}
		if preview, err = u.parseSchedule(ctx, userID, scheduleInput, tz, time.Now()); err != nil {
			return fail(err.Error())
		}
	}

	// Apply. A write that fails after others succeeded says what did change.
	var done []string
	failWrite := func(field string, err error) (string, error) {
		u.logger.Error("update_agent_job: write", zap.String("user_id", userID.String()),
			zap.String("agent_job_id", jobID.String()), zap.String("field", field), zap.Error(err))
		msg := fmt.Sprintf("could not update %s; try again", field)
		if len(done) > 0 {
			msg = fmt.Sprintf("updated %s, but could not update %s; try %s again", strings.Join(done, ", "), field, field)
		}
		return fail(msg)
	}
	job := current
	if a.Title != nil {
		var title *string
		if t := strings.TrimSpace(*a.Title); t != "" {
			title = &t
		}
		if job, err = u.store.UpdateAgentJobTitle(ctx, userID, jobID, title); err != nil {
			return failWrite("title", err)
		}
		done = append(done, "title")
	}
	if a.Prompt != nil {
		if job, err = u.store.UpdateAgentJobPrompt(ctx, userID, jobID, prompt); err != nil {
			return failWrite("prompt", err)
		}
		done = append(done, "prompt")
	}
	if a.ThreadID != nil {
		if job, err = u.store.SetAgentJobChat(ctx, userID, jobID, thread); err != nil {
			return failWrite("thread_id", err)
		}
		done = append(done, "thread_id")
	}
	if overrides.UpdateModel {
		if job, err = u.store.SetAgentJobOverrides(ctx, userID, jobID, overrides); err != nil {
			return failWrite("model_override", err)
		}
		done = append(done, "model_override")
	}
	if a.SkillIDs != nil {
		if err := u.replaceSkills(ctx, userID, jobID, current.Rituals, skillIDs); err != nil {
			return failWrite("skill_ids", err)
		}
		done = append(done, "skill_ids")
	}
	if a.FollowUserTimezone != nil {
		if job, err = u.store.SetAgentJobFollowUserTimezone(ctx, userID, jobID, *a.FollowUserTimezone); err != nil {
			return failWrite("follow_user_timezone", err)
		}
		done = append(done, "follow_user_timezone")
	}
	if preview != nil {
		var nextRunAt *time.Time
		if len(preview.NextRuns) > 0 {
			n := preview.NextRuns[0]
			nextRunAt = &n
		}
		if job, err = u.store.UpdateAgentJobSchedule(ctx, userID, jobID, scheduleInput, preview.ScheduleType,
			preview.Schedule, preview.RunAt, preview.Timezone, nextRunAt); err != nil {
			return failWrite("schedule_input", err)
		}
		done = append(done, "schedule_input")
	}
	// Status goes last, so pausing in the same call as a new schedule leaves the job paused (a new
	// schedule reactivates a finished job).
	if a.Status != nil {
		if job, err = u.store.UpdateAgentJobStatus(ctx, userID, jobID, status, ""); err != nil {
			return failWrite("status", err)
		}
		done = append(done, "status")
	}

	// Skills are edges the single-field updates do not return; reload so the result is current.
	if a.SkillIDs != nil {
		if reloaded, err := u.store.GetAgentJob(ctx, userID, jobID); err == nil {
			job = reloaded
		}
	}
	res := jobUpdateResult(job, done)
	if preview != nil {
		res.HumanSummary = preview.HumanSummary
	}
	if a.ThreadID != nil && thread == nil {
		res.Note = "The job gets a thread of its own on its next run."
	}
	return marshalUpdateAgentJobToolResult(res)
}

// replaceSkills makes the job's attached skills exactly want: it detaches the ones not wanted and
// attaches the new ones, leaving those in both alone.
func (u *agentJobUpdater) replaceSkills(ctx context.Context, userID, jobID uuid.UUID, have []*models.Ritual, want []uuid.UUID) error {
	wanted := make(map[uuid.UUID]bool, len(want))
	for _, id := range want {
		wanted[id] = true
	}
	attached := make(map[uuid.UUID]bool, len(have))
	for _, r := range have {
		if r == nil {
			continue
		}
		attached[r.ID] = true
		if !wanted[r.ID] {
			if err := u.store.RemoveAgentJobRitual(ctx, userID, jobID, r.ID); err != nil {
				return err
			}
		}
	}
	for _, id := range want {
		if attached[id] {
			continue
		}
		attached[id] = true // a skill listed twice is attached once
		if err := u.store.AddAgentJobRitual(ctx, userID, jobID, id); err != nil {
			return err
		}
	}
	return nil
}

// jobUpdateResult reports the job as it now stands, so the model can confirm the change.
func jobUpdateResult(job *models.AgentJob, updated []string) updateAgentJobToolResult {
	res := updateAgentJobToolResult{
		Success:             true,
		AgentJobID:          job.ID.String(),
		Updated:             updated,
		Prompt:              job.Prompt,
		Schedule:            job.ScheduleInput,
		Timezone:            job.Timezone,
		Status:              string(job.Status),
		FollowsUserTimezone: job.FollowUserTimezone,
		NextRunAt:           job.NextRunAt,
	}
	if job.Title != nil {
		res.Title = *job.Title
	}
	if job.ChatID != nil {
		res.ThreadID = job.ChatID.String()
	}
	if job.ModelID != nil {
		res.ModelID = job.ModelID.String()
	}
	for _, r := range job.Rituals {
		if r != nil {
			res.SkillIDs = append(res.SkillIDs, r.ID.String())
		}
	}
	sort.Strings(res.SkillIDs)
	return res
}

func marshalUpdateAgentJobToolResult(result updateAgentJobToolResult) (string, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
