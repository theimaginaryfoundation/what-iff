package datastore

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entchatmessage "github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessagecontextitem"
	"github.com/theimaginaryfoundation/what-iff/ent/job"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// SteerJobErrorCode marks a steer job a running turn took in (ADR 0x042): it ends cancelled with
// this code, because its message was answered by that turn rather than by a turn of its own.
const SteerJobErrorCode = "delivered_to_running_turn"

// SteerMessage is a follow-up the person sent while a turn was running, claimed by that turn.
type SteerMessage struct {
	MessageID uuid.UUID
	Message   string
	SentAt    time.Time
	// FileComments is the document comment the message was sent from (ADR 0x046), as the context
	// its own turn would have rendered after its text.
	FileComments []string
}

// ClaimSteerMessages takes the steer messages queued in chatID after the running turn's job
// (created after runningCreatedAt, oldest first) for that turn to read at its next step (ADR 0x042).
//
// Each one's job is claimed with a conditional update (still pending or processing, so still
// waiting in the turn gate behind the running turn), which ends it cancelled with
// SteerJobErrorCode; its waiting worker then stops (watchChatJobCancel) instead of running a turn
// of its own. A job whose claim lost a race (Stop, or the gate let it through) is left alone, so
// every message is either taken in by the running turn or answered by its own turn, never both.
func (d *Datastore) ClaimSteerMessages(ctx context.Context, userID, chatID, runningJobID uuid.UUID, runningCreatedAt time.Time) ([]SteerMessage, error) {
	jobs, err := d.dbClient.Job.Query().
		Where(
			job.StatusIn(job.StatusPending, job.StatusProcessing),
			job.JobType("chat_message"),
			job.Steer(true),
			ownedByUser(userID),
			job.IDNEQ(runningJobID),
			job.CreatedAtGT(runningCreatedAt),
		).
		Order(job.ByCreatedAt(), job.ByID()).
		Limit(turnJobScanLimit).
		Select(job.FieldID, job.FieldReference, job.FieldStatus, job.FieldCreatedAt).
		All(ctx)
	if err != nil || len(jobs) == 0 {
		return nil, err
	}
	inChat, err := d.turnJobsInChat(ctx, userID, chatID, jobs)
	if err != nil || len(inChat) == 0 {
		return nil, err
	}
	var claimedRefs []uuid.UUID
	for _, j := range inChat {
		ref, perr := uuid.Parse(j.Reference)
		if perr != nil {
			continue
		}
		n, err := d.dbClient.Job.Update().
			Where(job.ID(j.ID), ownedByUser(userID), job.StatusIn(job.StatusPending, job.StatusProcessing)).
			SetStatus(job.StatusCancelled).
			SetErrorCode(SteerJobErrorCode).
			SetProgress("").
			Save(ctx)
		if err != nil {
			return nil, err
		}
		if n == 1 {
			claimedRefs = append(claimedRefs, ref)
		}
	}
	if len(claimedRefs) == 0 {
		return nil, nil
	}
	rows, err := d.dbClient.ChatMessage.Query().
		Where(
			entchatmessage.IDIn(claimedRefs...),
			entchatmessage.HasChatWith(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))),
		).
		Order(entchatmessage.BySentAt(), entchatmessage.ByID()).
		WithContextItems(func(q *ent.ChatMessageContextItemQuery) {
			q.Where(chatmessagecontextitem.Type(models.AdditionalContextTypeFileComment))
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SteerMessage, 0, len(rows))
	for _, m := range rows {
		sm := SteerMessage{MessageID: m.ID, Message: m.Message, SentAt: m.SentAt}
		for _, it := range m.Edges.ContextItems {
			sm.FileComments = append(sm.FileComments, it.Content)
		}
		out = append(out, sm)
	}
	return out, nil
}
