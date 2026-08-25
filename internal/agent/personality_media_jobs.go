package agent

import (
	"fmt"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Background job types for personality tooling.
// Kept separate from chat_message / agent_job_run in message.go.
const (
	JobTypeExpressionGrid        = "expression_grid"
	JobTypePersonalityPortrait   = "personality_portrait"
	JobTypePersonalityGeneration = "personality_generation"
)

// PersonalityMediaJobTypes lists job types that share the single active
// personality-background slot per user.
var PersonalityMediaJobTypes = []string{
	JobTypeExpressionGrid,
	JobTypePersonalityPortrait,
	JobTypePersonalityGeneration,
}

// ErrPersonalityMediaJobActive is returned when another personality generation/media job is in flight.
type ErrPersonalityMediaJobActive struct {
	Job *models.Job
}

func (e *ErrPersonalityMediaJobActive) Error() string {
	if e == nil || e.Job == nil {
		return "personality background job already active"
	}
	return fmt.Sprintf("personality background job already active: %s", e.Job.ID)
}
