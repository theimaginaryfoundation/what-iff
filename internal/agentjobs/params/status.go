package params

import "github.com/theimaginaryfoundation/what-iff/internal/models"

// StatusAfterSave is a job's status once it is saved with its parameters complete or not
// (ADR 0x044, decision 4): an active job with a missing value waits for it as needs_values, and a
// needs_values job whose values are all set becomes active. Paused, complete and failed jobs keep
// their status: filling in values never resumes a job the user paused.
func StatusAfterSave(current models.AgentJobStatus, missing bool) models.AgentJobStatus {
	switch {
	case missing && current == models.AgentJobStatusActive:
		return models.AgentJobStatusNeedsValues
	case !missing && current == models.AgentJobStatusNeedsValues:
		return models.AgentJobStatusActive
	default:
		return current
	}
}
