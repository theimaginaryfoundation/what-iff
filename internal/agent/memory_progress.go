package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	// memoryProgressToolID identifies the memory-load row in a turn's live tool timeline.
	memoryProgressToolID = "memory-enrichment"
	// memoryProgressPreviewRunes caps the retrieved-memories preview on that row. It is larger
	// than the general tool preview because the memories are the point of the row; the full text
	// is still saved on the persisted "Load Memory" tool call.
	memoryProgressPreviewRunes = 4000
)

// memoryLoadProgress shows memory retrieval in a chat turn's live tool timeline
// (models.ChatTurnProgress): a "Load Memory" row that is running while retrieval runs and then
// completes with the retrieved memories, exactly like the row saved with the finished reply.
// Retrieval that finds nothing removes the row again, matching the saved reply, which has none.
//
// Writes are synchronous but bounded and best-effort: progress is cosmetic and never fails the
// turn. Nil-safe: paths without a chat job (agent-job runs, tests) pass nil and record nothing.
type memoryLoadProgress struct {
	ds     jobProgressWriter
	logger *zap.Logger
	userID uuid.UUID
	jobID  uuid.UUID
	now    func() time.Time

	// entry is the memory-load row, nil until retrieval starts and again if it found nothing.
	entry *models.ChatTurnToolCall
	// written is set once the row reached the job, so an empty result knows to clear it.
	written bool
}

// newMemoryLoadProgress builds the recorder for a chat job, or nil when there is nothing to write
// to. The nil check is on the concrete *Datastore (see newChatToolProgress).
func (a *Agent) newMemoryLoadProgress(job *models.Job) *memoryLoadProgress {
	if a.ds == nil || job == nil || job.ID == uuid.Nil || job.UserID == uuid.Nil {
		return nil
	}
	logger := a.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return &memoryLoadProgress{ds: a.ds, logger: logger, userID: job.UserID, jobID: job.ID, now: time.Now}
}

// Started shows the memory-load row as running. Call it only when retrieval really runs.
func (p *memoryLoadProgress) Started(ctx context.Context) {
	if p == nil {
		return
	}
	p.entry = &models.ChatTurnToolCall{
		ID:        memoryProgressToolID,
		Name:      memoryEnrichmentToolCallName,
		Status:    models.ChatTurnToolRunning,
		StartedAt: p.now().UTC(),
	}
	p.written = p.write(ctx)
}

// Finished completes the row with what retrieval returned: the memories, an error, or (when it
// found nothing) no row at all.
func (p *memoryLoadProgress) Finished(ctx context.Context, memories []string, failed bool) {
	if p == nil || p.entry == nil {
		return
	}
	finishedAt := p.now().UTC()
	switch {
	case failed:
		p.entry.Status = models.ChatTurnToolError
		p.entry.Output = memoryEnrichmentFailureMessage
	case len(memories) > 0:
		p.entry.Status = models.ChatTurnToolComplete
		p.entry.Output = tools.TruncateRunes(memoryToolCall(memories).ToolOutput, memoryProgressPreviewRunes)
	default:
		p.entry = nil
		if p.written {
			p.write(ctx)
		}
		return
	}
	p.entry.FinishedAt = &finishedAt
	p.write(ctx)
}

// Entries is the memory-load row to start the turn's tool recorder with, so its snapshots keep
// the row instead of replacing it. Nil when retrieval showed nothing.
func (p *memoryLoadProgress) Entries() []models.ChatTurnToolCall {
	if p == nil || p.entry == nil {
		return nil
	}
	return []models.ChatTurnToolCall{*p.entry}
}

func (p *memoryLoadProgress) write(ctx context.Context) bool {
	calls := p.Entries()
	if calls == nil {
		calls = []models.ChatTurnToolCall{} // encode as [] so clients can read it
	}
	raw, err := json.Marshal(models.ChatTurnProgress{ToolCalls: calls})
	if err != nil {
		return false
	}
	// Detached from the turn's cancellation so a cancel mid-retrieval can't strand the row
	// half-written; the terminal job status is what ends the display anyway.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolProgressPersistTimeout)
	defer cancel()
	if err := p.ds.UpdateJobProgress(writeCtx, p.userID, p.jobID, string(raw)); err != nil {
		p.logger.Warn("failed to persist memory load progress", zap.String("job_id", p.jobID.String()), zap.Error(err))
		return false
	}
	return true
}
