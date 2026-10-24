package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// AgentJobScheduleType represents how an AgentJob is scheduled.
type AgentJobScheduleType string

const (
	AgentJobScheduleTypeCron AgentJobScheduleType = "cron"
	AgentJobScheduleTypeAt   AgentJobScheduleType = "at"
)

// AgentJobStatus represents the lifecycle state of an AgentJob.
type AgentJobStatus string

const (
	AgentJobStatusActive   AgentJobStatus = "active"
	AgentJobStatusPaused   AgentJobStatus = "paused"
	AgentJobStatusComplete AgentJobStatus = "complete"
	AgentJobStatusFailed   AgentJobStatus = "failed"
)

// AgentJob represents a scheduled, semi-autonomous task owned by a user.
type AgentJob struct {
	ID            uuid.UUID            `json:"id"`
	UserID        uuid.UUID            `json:"user_id"`
	ChatID        *uuid.UUID           `json:"chat_id,omitempty"`
	PersonalityID *uuid.UUID           `json:"personality_id,omitempty"`
	ModelID       *uuid.UUID           `json:"model_id,omitempty"`
	Title         *string              `json:"title,omitempty"`
	Prompt        string               `json:"prompt"`
	ScheduleInput string               `json:"schedule_input"`
	ScheduleType  AgentJobScheduleType `json:"schedule_type"`
	Schedule      *string              `json:"schedule,omitempty"`
	RunAt         *time.Time           `json:"run_at,omitempty"`
	Timezone      string               `json:"timezone"`
	// FollowUserTimezone makes a recurring schedule run in the owner's current timezone rather than Timezone.
	FollowUserTimezone bool `json:"follow_user_timezone"`
	// EffectiveTimezone is the timezone the schedule runs in right now: the owner's current
	// timezone for a job that follows it, else Timezone. Derived when the job is loaded.
	EffectiveTimezone string `json:"effective_timezone,omitempty"`
	// LastRunTimezone is the timezone the schedule was in when the job last ran (nil before the
	// first run recorded with it).
	LastRunTimezone *string        `json:"-"`
	Status          AgentJobStatus `json:"status"`
	NextRunAt       *time.Time     `json:"next_run_at,omitempty"`
	LastRunAt       *time.Time     `json:"last_run_at,omitempty"`
	LastError       string         `json:"last_error,omitempty"`
	RunCount        int            `json:"run_count"`
	// Rituals is populated when the job is loaded with ritual edges. May be nil if edges not loaded.
	Rituals   []*Ritual `json:"rituals,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ScheduleTimezone is the IANA timezone the job's schedule runs in: EffectiveTimezone when it
// was derived, else Timezone.
func (j AgentJob) ScheduleTimezone() string {
	if tz := strings.TrimSpace(j.EffectiveTimezone); tz != "" {
		return tz
	}
	return strings.TrimSpace(j.Timezone)
}

// AgentJobFilters defines filters for listing AgentJobs.
type AgentJobFilters struct {
	Status       *AgentJobStatus       `json:"status,omitempty"`
	ScheduleType *AgentJobScheduleType `json:"schedule_type,omitempty"`
	Query        *string               `json:"query,omitempty"`
}

// SetAgentJobOverridesPatch selects which override fields to change.
// UpdatePersonality with PersonalityID nil clears the personality association.
// UpdateModel with ModelID nil clears the model override.
// Omitted fields (UpdatePersonality / UpdateModel false) are left unchanged.
type SetAgentJobOverridesPatch struct {
	UpdatePersonality bool
	PersonalityID     *uuid.UUID
	UpdateModel       bool
	ModelID           *uuid.UUID
}
