package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agentjobs/schedule"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"go.uber.org/zap"
)

type createAgentJobToolArgs struct {
	Title            *string  `json:"title,omitempty"`
	ScheduleInput    string   `json:"schedule_input"`
	Prompt           string   `json:"prompt"`
	SkillIDs         []string `json:"skill_ids,omitempty"`
	ModelOverride    string   `json:"model_override,omitempty"`
	UseCurrentThread *bool    `json:"use_current_thread,omitempty"`
}

type createAgentJobToolResult struct {
	Success      bool                        `json:"success"`
	AgentJobID   string                      `json:"agent_job_id,omitempty"`
	ScheduleType models.AgentJobScheduleType `json:"schedule_type,omitempty"`
	Schedule     *string                     `json:"schedule,omitempty"`
	RunAt        *time.Time                  `json:"run_at,omitempty"`
	Timezone     string                      `json:"timezone,omitempty"`
	HumanSummary string                      `json:"human_summary,omitempty"`
	NextRunAt    *time.Time                  `json:"next_run_at,omitempty"`
	Error        string                      `json:"error,omitempty"`
}

func (a *Agent) createAgentJobTool(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	var toolArgs createAgentJobToolArgs
	if err := json.Unmarshal(args, &toolArgs); err != nil {
		return marshalAgentJobToolResult(createAgentJobToolResult{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		})
	}
	if !featuregate.IsEntitled(ctx, chat.UserID) {
		return marshalAgentJobToolResult(createAgentJobToolResult{
			Success: false,
			Error:   "agent jobs are not available for this account",
		})
	}
	scheduleInput := strings.TrimSpace(toolArgs.ScheduleInput)
	if scheduleInput == "" {
		return marshalAgentJobToolResult(createAgentJobToolResult{
			Success: false,
			Error:   "schedule_input is required",
		})
	}

	prompt := strings.TrimSpace(toolArgs.Prompt)
	if prompt == "" {
		return marshalAgentJobToolResult(createAgentJobToolResult{
			Success: false,
			Error:   "prompt is required",
		})
	}

	tz := "UTC"
	if clientTZ, ok := middleware.GetClientTimezoneFromContext(ctx); ok {
		if s := strings.TrimSpace(clientTZ); s != "" {
			tz = s
		}
	}

	now := time.Now()
	preview, err := schedule.Parse(ctx, a.OpenAIProvider, chat.UserID, scheduleInput, tz, now)
	if err != nil {
		return marshalAgentJobToolResult(createAgentJobToolResult{
			Success: false,
			Error:   err.Error(),
		})
	}

	var nextRunAt *time.Time
	if len(preview.NextRuns) > 0 {
		n := preview.NextRuns[0]
		nextRunAt = &n
	}

	newJob := models.AgentJob{
		Title:         toolArgs.Title,
		Prompt:        prompt,
		ScheduleInput: scheduleInput,
		ScheduleType:  preview.ScheduleType,
		Schedule:      preview.Schedule,
		RunAt:         preview.RunAt,
		Timezone:      preview.Timezone,
		Status:        models.AgentJobStatusActive,
		NextRunAt:     nextRunAt,
		RunCount:      0,
	}

	if toolArgs.UseCurrentThread != nil && *toolArgs.UseCurrentThread {
		chatID := chat.ID
		newJob.ChatID = &chatID
	}

	chosenModel := strings.TrimSpace(toolArgs.ModelOverride)
	if chosenModel != "" {
		m, err := a.resolveModelByReference(ctx, chosenModel, modelResolverOptions{
			AllowDisplayName: true,
			AllowPrefixMatch: true,
		})
		if err != nil {
			return marshalAgentJobToolResult(createAgentJobToolResult{
				Success: false,
				Error:   "model_override not found: " + chosenModel,
			})
		}
		modelID := m.ID
		newJob.ModelID = &modelID
	}

	created, err := a.ds.CreateAgentJob(ctx, chat.UserID, newJob)
	if err != nil {
		a.logger.Error("failed to create agent job via tool",
			zap.String("user_id", chat.UserID.String()),
			zap.Error(err),
		)
		return marshalAgentJobToolResult(createAgentJobToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to create agent job: %v", err),
		})
	}

	// Attach any requested skills to the newly created job so they are loaded at run time.
	skillUUIDs := parseSkillIDs(toolArgs.SkillIDs)
	for _, skillID := range skillUUIDs {
		if attachErr := a.ds.AddAgentJobRitual(ctx, chat.UserID, created.ID, skillID); attachErr != nil {
			a.logger.Warn("failed to attach skill to agent job",
				zap.String("job_id", created.ID.String()),
				zap.String("skill_id", skillID.String()),
				zap.Error(attachErr),
			)
		}
	}

	result := createAgentJobToolResult{
		Success:      true,
		AgentJobID:   created.ID.String(),
		ScheduleType: created.ScheduleType,
		Schedule:     created.Schedule,
		RunAt:        created.RunAt,
		Timezone:     created.Timezone,
		HumanSummary: preview.HumanSummary,
		NextRunAt:    created.NextRunAt,
	}

	return marshalAgentJobToolResult(result)
}

func marshalAgentJobToolResult(result createAgentJobToolResult) (string, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
