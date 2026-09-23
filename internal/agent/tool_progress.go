package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	// toolProgressPreviewRunes caps tool input/output previews in the live timeline. The full
	// values land on the persisted ToolCall rows; the timeline only needs enough to recognise
	// the call, and every write rewrites the whole payload.
	toolProgressPreviewRunes   = 1000
	toolProgressPersistTimeout = 2 * time.Second
)

// jobProgressWriter is the slice of the datastore the recorder needs.
type jobProgressWriter interface {
	UpdateJobProgress(ctx context.Context, userID, id uuid.UUID, progress string) error
}

// jobToolProgress records the agent loop's tool calls into a chat_message job's Progress
// (models.ChatTurnProgress) as they start and finish, so a polling client can show a live
// tool timeline instead of a bare typing indicator. All methods are nil-safe: paths without a
// job (sync agent runs, tests) simply don't record.
type jobToolProgress struct {
	persistParent context.Context
	ds            jobProgressWriter
	logger        *zap.Logger
	userID        uuid.UUID
	jobID         uuid.UUID
	// beforeTool runs before a call is recorded as started — used to flush buffered reply
	// text so a preamble ("let me check…") reaches the client ahead of the tool row.
	beforeTool func()
	now        func() time.Time

	mu      sync.Mutex
	entries []models.ChatTurnToolCall
}

// newChatToolProgress builds the recorder for a chat turn, or nil when there is no datastore.
// The nil check happens on the concrete pointer: a nil *Datastore wrapped in jobProgressWriter
// would be a non-nil interface and panic on the first write.
func (a *Agent) newChatToolProgress(job *models.Job, beforeTool func()) *jobToolProgress {
	if a.ds == nil {
		return nil
	}
	return newJobToolProgress(a.lifecycleCtx, a.ds, a.logger, job, beforeTool)
}

func newJobToolProgress(persistParent context.Context, ds jobProgressWriter, logger *zap.Logger, job *models.Job, beforeTool func()) *jobToolProgress {
	if job == nil || ds == nil || job.ID == uuid.Nil || job.UserID == uuid.Nil {
		return nil
	}
	if persistParent == nil {
		persistParent = context.Background()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &jobToolProgress{
		persistParent: persistParent,
		ds:            ds,
		logger:        logger,
		userID:        job.UserID,
		jobID:         job.ID,
		beforeTool:    beforeTool,
		now:           time.Now,
	}
}

// Started records use as running.
func (p *jobToolProgress) Started(round int, use provider.ToolUse) {
	if p == nil {
		return
	}
	if p.beforeTool != nil {
		p.beforeTool()
	}
	p.mu.Lock()
	p.entries = append(p.entries, models.ChatTurnToolCall{
		ID:        use.ID,
		Name:      use.Name,
		Input:     tools.TruncateRunes(string(use.Input), toolProgressPreviewRunes),
		Status:    models.ChatTurnToolRunning,
		Round:     round,
		StartedAt: p.now().UTC(),
	})
	payload := p.snapshotLocked()
	p.mu.Unlock()
	p.persist(payload)
}

// Finished marks the running entry for result.ID complete (or error) with an output preview.
func (p *jobToolProgress) Finished(result provider.ToolResult) {
	if p == nil {
		return
	}
	p.mu.Lock()
	idx := -1
	for i := len(p.entries) - 1; i >= 0; i-- {
		if p.entries[i].ID == result.ID && p.entries[i].Status == models.ChatTurnToolRunning {
			idx = i
			break
		}
	}
	if idx < 0 {
		p.mu.Unlock()
		return
	}
	finishedAt := p.now().UTC()
	entry := &p.entries[idx]
	entry.Status = models.ChatTurnToolComplete
	if result.IsErr {
		entry.Status = models.ChatTurnToolError
	}
	entry.Output = tools.TruncateRunes(result.Output, toolProgressPreviewRunes)
	entry.FinishedAt = &finishedAt
	payload := p.snapshotLocked()
	p.mu.Unlock()
	p.persist(payload)
}

func (p *jobToolProgress) snapshotLocked() string {
	raw, err := json.Marshal(models.ChatTurnProgress{ToolCalls: p.entries})
	if err != nil {
		p.logger.Warn("failed to encode chat turn progress", zap.String("job_id", p.jobID.String()), zap.Error(err))
		return ""
	}
	return string(raw)
}

// persist is best-effort: the timeline is cosmetic, so a failed write is logged and the turn
// carries on. It runs on the loop goroutine, bounded by toolProgressPersistTimeout.
func (p *jobToolProgress) persist(payload string) {
	if payload == "" {
		return
	}
	writeCtx, cancel := context.WithTimeout(p.persistParent, toolProgressPersistTimeout)
	defer cancel()
	if err := p.ds.UpdateJobProgress(writeCtx, p.userID, p.jobID, payload); err != nil {
		p.logger.Warn("failed to persist chat turn progress", zap.String("job_id", p.jobID.String()), zap.Error(err))
	}
}
