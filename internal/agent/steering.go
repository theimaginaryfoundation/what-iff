package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
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

// takeSteering claims the follow-ups sent since the turn began and returns their texts, or nil
// when there are none. Only an interactive web turn (promptJob) takes them in. A claim error is
// logged and the messages stay queued (they run as the next turn).
func (a *Agent) takeSteering(ctx context.Context, chatCtx *chatContext) []string {
	store := a.steerStore()
	if store == nil || chatCtx == nil || chatCtx.promptJob == nil || chatCtx.chat == nil || ctx.Err() != nil {
		return nil
	}
	job := chatCtx.promptJob
	msgs, err := store.ClaimSteerMessages(ctx, job.UserID, chatCtx.chat.ID, job.ID, job.CreatedAt)
	if err != nil {
		if ctx.Err() == nil {
			a.logger.Warn("failed to claim follow-up messages for the running turn; they will run as the next turn",
				zap.String("job_id", job.ID.String()), zap.Error(err))
		}
		return nil
	}
	if len(msgs) == 0 {
		return nil
	}
	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if t := strings.TrimSpace(m.Message); t != "" {
			// A comment sent mid-run keeps what it is about (ADR 0x046), as its own turn would.
			comments := make([]models.AdditionalContextItem, 0, len(m.FileComments))
			for _, c := range m.FileComments {
				comments = append(comments, models.AdditionalContextItem{Type: models.AdditionalContextTypeFileComment, Content: c})
			}
			texts = append(texts, withFileCommentContext(t, &models.ChatMessage{AdditionalContext: comments}))
		}
	}
	a.logger.Info("running turn took in follow-up messages",
		zap.String("job_id", job.ID.String()), zap.Int("messages", len(msgs)))
	if len(texts) == 0 {
		return nil
	}
	chatCtx.steerNotes = append(chatCtx.steerNotes, texts...)
	return texts
}

// steerMidRunTag marks a follow-up as sent mid-run, so the model reads it as the user adding to
// (or changing) the request it is working on, not as a new request after a finished one.
const steerMidRunTag = "[Sent mid-run, while you were still working on my request]"

// steerUserTurn is the user turn the follow-ups go in as: the user's own words, lightly tagged.
func steerUserTurn(texts []string) string {
	var b strings.Builder
	b.WriteString(steerMidRunTag)
	for _, t := range texts {
		b.WriteString("\n\n")
		b.WriteString(t)
	}
	return truncateRunesKeepTail(b.String(), maxSteerNoteChars)
}

// steerToolNote frames follow-ups folded into a tool result, for an adapter that can't add a user
// turn: there they sit in a tool's output, so the framing says they come from the person.
func steerToolNote(texts []string) string {
	var b strings.Builder
	if len(texts) == 1 {
		b.WriteString("[The user sent this message mid-run, while you were working. It comes from them, not from a tool.]\n")
	} else {
		fmt.Fprintf(&b, "[The user sent these %d messages mid-run, while you were working. They come from them, not from a tool.]\n", len(texts))
	}
	for _, t := range texts {
		b.WriteString("<user_message>\n")
		b.WriteString(t)
		b.WriteString("\n</user_message>\n")
	}
	b.WriteString("Take this into account from here on: if it changes what they want, change course (update your plan if you keep one); if it is a question, answer it in your reply.")
	return truncateRunesKeepTail(b.String(), maxSteerNoteChars)
}

// appendToolResultsWithSteering appends a round's tool results, then the follow-ups the person
// sent meanwhile: as a user turn of their own when the adapter can add one (provider.UserNoteAppender),
// otherwise folded into the last result. The saved tool calls are built before this and keep the
// tools' own output either way.
func appendToolResultsWithSteering(adapter provider.AgentAdapter, results []provider.ToolResult, texts []string) {
	if len(texts) == 0 {
		adapter.AppendToolResults(results)
		return
	}
	if appender, ok := adapter.(provider.UserNoteAppender); ok {
		adapter.AppendToolResults(results)
		appender.AppendUserNote(steerUserTurn(texts))
		return
	}
	withSteerNote(results, steerToolNote(texts))
	adapter.AppendToolResults(results)
}

// withSteerNote adds note to the last of a round's tool results.
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
