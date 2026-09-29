package agent

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// chatTurnPhases writes the coarse pre-inference phase of a chat turn into its job's Progress
// (models.ChatTurnProgress.Phase), so a polling client can say "loading memories" instead of a
// bare typing indicator while retrieval runs. Writes are synchronous but bounded and
// best-effort: progress is cosmetic and never fails the turn. Nil-safe: paths without a chat
// job (agent-job runs, tests) pass nil and record nothing.
type chatTurnPhases struct {
	ds     jobProgressWriter
	logger *zap.Logger
	userID uuid.UUID
	jobID  uuid.UUID
	// loadingReported is set once loading_memories was written, so the follow-up inference
	// write only happens for turns that showed the loading phase.
	loadingReported bool
}

// newChatTurnPhases builds the phase writer for a chat job, or nil when there is nothing to
// write to. The nil check is on the concrete *Datastore (see newChatToolProgress).
func (a *Agent) newChatTurnPhases(job *models.Job) *chatTurnPhases {
	if a.ds == nil || job == nil || job.ID == uuid.Nil || job.UserID == uuid.Nil {
		return nil
	}
	logger := a.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return &chatTurnPhases{ds: a.ds, logger: logger, userID: job.UserID, jobID: job.ID}
}

// LoadingMemories records that memory retrieval has started.
func (p *chatTurnPhases) LoadingMemories(ctx context.Context) {
	if p == nil {
		return
	}
	if p.write(ctx, models.ChatTurnPhaseLoadingMemories) {
		p.loadingReported = true
	}
}

// MemoriesLoaded moves the turn on to inference, but only if loading was shown: a turn that
// never showed the loading phase has nothing to clear and needs no extra write.
func (p *chatTurnPhases) MemoriesLoaded(ctx context.Context) {
	if p == nil || !p.loadingReported {
		return
	}
	p.write(ctx, models.ChatTurnPhaseInference)
	p.loadingReported = false
}

func (p *chatTurnPhases) write(ctx context.Context, phase models.ChatTurnPhase) bool {
	raw, err := json.Marshal(models.ChatTurnProgress{Phase: phase, ToolCalls: []models.ChatTurnToolCall{}})
	if err != nil {
		return false
	}
	// Detached from the turn's cancellation so a cancel mid-retrieval can't strand the phase
	// half-written; the terminal job status is what ends the display anyway.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolProgressPersistTimeout)
	defer cancel()
	if err := p.ds.UpdateJobProgress(writeCtx, p.userID, p.jobID, string(raw)); err != nil {
		p.logger.Warn("failed to persist chat turn phase",
			zap.String("job_id", p.jobID.String()), zap.String("phase", string(phase)), zap.Error(err))
		return false
	}
	return true
}
