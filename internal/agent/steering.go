package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"go.uber.org/zap"
)

// Steering (ADR 0x042): a message the person sends while their turn is still running is saved as
// usual and queued behind it as a steer job. The running turn takes it in at its next step instead
// of making them stop and resend: before the next model call it claims the queued steer jobs
// (ClaimSteerMessages) and adds their text to the conversation. A message it never reaches (the
// turn ends first) simply runs as the next turn, so nothing is lost either way.

// steerStore is the slice of the datastore steering needs (a test hook can replace it).
type steerStore interface {
	ClaimSteerMessages(ctx context.Context, userID, chatID, runningJobID uuid.UUID, runningCreatedAt time.Time) ([]datastore.SteerMessage, error)
}

func (a *Agent) steerStore() steerStore {
	if a.testHooks.SteerStore != nil {
		return a.testHooks.SteerStore
	}
	if a.ds == nil {
		return nil
	}
	return a.ds
}

// maxSteerNoteChars bounds the steering text one note carries into the model's context.
const maxSteerNoteChars = 8000

// takeSteering claims the follow-ups sent since the turn began and returns them as one note for
// the model, or "" when there are none. Only an interactive web turn (promptJob) takes them in. A
// claim error is logged and the messages stay queued (they run as the next turn).
func (a *Agent) takeSteering(ctx context.Context, chatCtx *chatContext) string {
	store := a.steerStore()
	if store == nil || chatCtx == nil || chatCtx.promptJob == nil || chatCtx.chat == nil || ctx.Err() != nil {
		return ""
	}
	job := chatCtx.promptJob
	msgs, err := store.ClaimSteerMessages(ctx, job.UserID, chatCtx.chat.ID, job.ID, job.CreatedAt)
	if err != nil {
		if ctx.Err() == nil {
			a.logger.Warn("failed to claim follow-up messages for the running turn; they will run as the next turn",
				zap.String("job_id", job.ID.String()), zap.Error(err))
		}
		return ""
	}
	if len(msgs) == 0 {
		return ""
	}
	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if t := strings.TrimSpace(m.Message); t != "" {
			texts = append(texts, t)
		}
	}
	a.logger.Info("running turn took in follow-up messages",
		zap.String("job_id", job.ID.String()), zap.Int("messages", len(msgs)))
	if len(texts) == 0 {
		return ""
	}
	chatCtx.steerNotes = append(chatCtx.steerNotes, texts...)
	return steerNote(texts)
}

// steerNote frames follow-ups for the model: they come from the person (not a tool), mid-work.
func steerNote(texts []string) string {
	var b strings.Builder
	if len(texts) == 1 {
		b.WriteString("[The user sent this message while you were working. It comes from them, not from a tool.]\n")
	} else {
		fmt.Fprintf(&b, "[The user sent these %d messages while you were working. They come from them, not from a tool.]\n", len(texts))
	}
	for _, t := range texts {
		b.WriteString("<user_message>\n")
		b.WriteString(t)
		b.WriteString("\n</user_message>\n")
	}
	b.WriteString("Take this into account from here on: if it changes what they want, change course (update your plan if you keep one); if it is a question, answer it in your reply.")
	return truncateRunesKeepTail(b.String(), maxSteerNoteChars)
}

// withSteerNote adds note to the last of a round's tool results, so the next call carries the
// person's words right after the results it is about to read. Every provider accepts tool results,
// whereas a user message between tool results and the next call is refused by some. The saved
// tool calls are built before this and keep the tool's own output.
func withSteerNote(results []provider.ToolResult, note string) {
	if note == "" || len(results) == 0 {
		return
	}
	last := &results[len(results)-1]
	if last.Output == "" {
		last.Output = note
		return
	}
	last.Output += "\n\n" + note
}
