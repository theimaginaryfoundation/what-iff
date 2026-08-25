package models

// PersonalityMediaJobResponse is returned when a personality generation/media job is enqueued (HTTP 202).
type PersonalityMediaJobResponse struct {
	JobID   string `json:"job_id"`
	JobType string `json:"job_type"`
}

// ActivePersonalityMediaJob describes an in-flight personality-related background job, if any.
type ActivePersonalityMediaJob struct {
	JobID           string  `json:"job_id"`
	JobType         string  `json:"job_type"`
	Reference       string  `json:"reference"`
	Status          string  `json:"status"`
	PersonalityID   *string `json:"personality_id,omitempty"`
	PersonalityName *string `json:"personality_name,omitempty"`
	FlowID          *string `json:"flow_id,omitempty"`
	Error           string  `json:"error,omitempty"`
}

// PersonalityMediaJobConflict is returned on HTTP 409 when a media job is already running.
type PersonalityMediaJobConflict struct {
	Message string                    `json:"message"`
	Active  ActivePersonalityMediaJob `json:"active"`
}
