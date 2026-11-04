package modeltypes

import "github.com/google/uuid"

// JobRun records the agent job an assistant turn ran for (ADR 0x040): saved on its reply
// (ChatMessage.JobRun) and, while the run is live, on its queue ticket (Job.JobRun), so the web app
// can mark the reply, show the job's prompt and link to the job. Display only: the context builder
// never replays it to the model, so the prompt stays out of the thread's history.
type JobRun struct {
	AgentJobID uuid.UUID `json:"agent_job_id"`
	// Title is the job's title when it ran; empty for an untitled job.
	Title string `json:"title,omitempty"`
	// Prompt is the job's prompt as it ran. It is never saved as a message.
	Prompt string `json:"prompt,omitempty"`
}
