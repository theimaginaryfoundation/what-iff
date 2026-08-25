package models

import "time"

// AgentJobSchedulePreview is a validated schedule parse result with run previews.
type AgentJobSchedulePreview struct {
	ScheduleType AgentJobScheduleType `json:"schedule_type"`
	Schedule     *string              `json:"schedule"`
	RunAt        *time.Time           `json:"run_at"`
	Timezone     string               `json:"timezone"`
	HumanSummary string               `json:"human_summary"`
	NextRuns     []time.Time          `json:"next_runs"`
}
