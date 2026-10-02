package agent

import (
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/replyhook"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// fireReplyHook tells registered reply hooks (see internal/replyhook) that a reply
// finished. It runs once the reply is saved and its job is complete, so a hook
// reads the final message. Hooks run detached on the lifecycle context: they never
// delay or fail the turn, and they stop on shutdown.
func (a *Agent) fireReplyHook(userID uuid.UUID, agentMessage *models.ChatMessage, trigger *models.ChatMessage, job *models.Job, callPath telemetry.CallPath) {
	if !replyhook.Enabled() || agentMessage == nil {
		return
	}
	ev := replyhook.Event{
		UserID:    userID,
		ChatID:    agentMessage.ChatID,
		MessageID: agentMessage.ID,
		CallPath:  callPath,
	}
	if trigger != nil && trigger.ID != uuid.Nil {
		id := trigger.ID
		ev.TriggerMessageID = &id
	}
	if job != nil {
		id := job.ID
		ev.JobID = &id
	}
	replyhook.Fire(a.lifecycleCtx, a.logger, ev)
}

// userTurnCallPath labels a turn that went through handleUserMessage: the sync
// webhook path runs agent_job_run jobs through it too, and those are jobs.
func userTurnCallPath(job *models.Job) telemetry.CallPath {
	if job != nil && job.JobType == JobTypeAgentJobRun {
		return telemetry.CallPathAgentJob
	}
	return telemetry.CallPathUserChat
}
