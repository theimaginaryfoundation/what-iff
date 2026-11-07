package datastore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entagentprompt "github.com/theimaginaryfoundation/what-iff/ent/agentprompt"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Agent prompts (ADR 0x042): a question a running turn is waiting on. The turn's worker creates
// the row and polls it; the person answers through the API, which may run on another instance.

var (
	// ErrAgentPromptNotFound: no prompt with that id belongs to the account (and chat).
	ErrAgentPromptNotFound = errors.New("prompt not found")
	// ErrAgentPromptClosed: the prompt was already answered, expired or cancelled.
	ErrAgentPromptClosed = errors.New("prompt is no longer waiting for an answer")
)

// CreateAgentPrompt stores a new pending prompt and returns it with its id.
func (d *Datastore) CreateAgentPrompt(ctx context.Context, p *models.AgentPrompt) (*models.AgentPrompt, error) {
	create := d.dbClient.AgentPrompt.Create().
		SetUserID(p.UserID).
		SetChatID(p.ChatID).
		SetJobID(p.JobID).
		SetSource(string(p.Source)).
		SetPayload(p.Payload).
		SetExpiresAt(p.ExpiresAt)
	if p.ID != uuid.Nil {
		create.SetID(p.ID)
	}
	if p.ToolCallID != "" {
		create.SetToolCallID(p.ToolCallID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return nil, err
	}
	return agentPromptFromEnt(row), nil
}

// GetAgentPrompt returns the account's prompt.
func (d *Datastore) GetAgentPrompt(ctx context.Context, userID, id uuid.UUID) (*models.AgentPrompt, error) {
	row, err := d.dbClient.AgentPrompt.Query().
		Where(entagentprompt.ID(id), entagentprompt.UserID(userID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrAgentPromptNotFound
	}
	if err != nil {
		return nil, err
	}
	return agentPromptFromEnt(row), nil
}

// AnswerAgentPrompt records the person's answer to a pending prompt in chatID. Only a pending
// prompt that has not passed its expiry takes an answer, and only once: the update is conditional,
// so two answers racing (two tabs) cannot both land, and an answer after the turn gave up is
// refused rather than silently ignored.
func (d *Datastore) AnswerAgentPrompt(ctx context.Context, userID, chatID, id uuid.UUID, resp models.AgentPromptResponse) (*models.AgentPrompt, error) {
	now := time.Now()
	n, err := d.dbClient.AgentPrompt.Update().
		Where(
			entagentprompt.ID(id),
			entagentprompt.UserID(userID),
			entagentprompt.ChatID(chatID),
			entagentprompt.StatusEQ(entagentprompt.StatusPending),
			entagentprompt.ExpiresAtGT(now),
		).
		SetStatus(entagentprompt.StatusAnswered).
		SetResponse(&resp).
		SetAnsweredAt(now).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		existing, gerr := d.GetAgentPrompt(ctx, userID, id)
		if gerr != nil {
			return nil, gerr
		}
		if existing.ChatID != chatID {
			return nil, ErrAgentPromptNotFound
		}
		return existing, ErrAgentPromptClosed
	}
	return d.GetAgentPrompt(ctx, userID, id)
}

// CloseAgentPrompt ends a still-pending prompt without an answer (expired, or cancelled because
// the turn ended). It reports whether it closed it: false means it was no longer pending (an
// answer landed first, which the caller should then read).
func (d *Datastore) CloseAgentPrompt(ctx context.Context, userID, id uuid.UUID, status models.AgentPromptStatus) (bool, error) {
	n, err := d.dbClient.AgentPrompt.Update().
		Where(
			entagentprompt.ID(id),
			entagentprompt.UserID(userID),
			entagentprompt.StatusEQ(entagentprompt.StatusPending),
		).
		SetStatus(entagentprompt.Status(status)).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListPendingAgentPrompts returns the chat's prompts still waiting for an answer, oldest first.
func (d *Datastore) ListPendingAgentPrompts(ctx context.Context, userID, chatID uuid.UUID) ([]*models.AgentPrompt, error) {
	rows, err := d.dbClient.AgentPrompt.Query().
		Where(
			entagentprompt.UserID(userID),
			entagentprompt.ChatID(chatID),
			entagentprompt.StatusEQ(entagentprompt.StatusPending),
			entagentprompt.ExpiresAtGT(time.Now()),
		).
		Order(ent.Asc(entagentprompt.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*models.AgentPrompt, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentPromptFromEnt(row))
	}
	return out, nil
}

// CancelPendingAgentPromptsForJob cancels every prompt of a job still pending (the turn ended).
func (d *Datastore) CancelPendingAgentPromptsForJob(ctx context.Context, userID, jobID uuid.UUID) error {
	_, err := d.dbClient.AgentPrompt.Update().
		Where(
			entagentprompt.UserID(userID),
			entagentprompt.JobID(jobID),
			entagentprompt.StatusEQ(entagentprompt.StatusPending),
		).
		SetStatus(entagentprompt.StatusCancelled).
		Save(ctx)
	return err
}

func agentPromptFromEnt(row *ent.AgentPrompt) *models.AgentPrompt {
	if row == nil {
		return nil
	}
	return &models.AgentPrompt{
		ID:         row.ID,
		UserID:     row.UserID,
		ChatID:     row.ChatID,
		JobID:      row.JobID,
		Source:     models.AgentPromptSource(row.Source),
		ToolCallID: row.ToolCallID,
		Payload:    row.Payload,
		Status:     models.AgentPromptStatus(row.Status),
		Response:   row.Response,
		ExpiresAt:  row.ExpiresAt,
		AnsweredAt: row.AnsweredAt,
		CreatedAt:  row.CreatedAt,
	}
}
