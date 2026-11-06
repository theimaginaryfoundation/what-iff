package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/backgroundnotice"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ErrBackgroundNoticeNotFound is returned for a notice that doesn't exist or isn't the
// caller's.
var ErrBackgroundNoticeNotFound = errors.New("background notice not found")

// SettleBackgroundNotices records what a run of background steps on a chat found: a failed
// step's notice is created or updated, and a succeeded step's is deleted. A notice the account
// dismissed stays dismissed while its cause (the code) stays the same.
func (ds *Datastore) SettleBackgroundNotices(ctx context.Context, userID, chatID uuid.UUID, outcome models.BackgroundOutcome) error {
	if len(outcome.Failed) == 0 && len(outcome.Succeeded) == 0 {
		return nil
	}
	tx, err := ds.dbClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin background notices: %w", err)
	}
	if err := settleBackgroundNoticesTx(ctx, tx, userID, chatID, outcome); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit background notices: %w", err)
	}
	return nil
}

func settleBackgroundNoticesTx(ctx context.Context, tx *ent.Tx, userID, chatID uuid.UUID, outcome models.BackgroundOutcome) error {
	if len(outcome.Succeeded) > 0 {
		steps := make([]string, 0, len(outcome.Succeeded))
		for _, s := range outcome.Succeeded {
			steps = append(steps, string(s))
		}
		if _, err := tx.BackgroundNotice.Delete().
			Where(backgroundnotice.UserID(userID), backgroundnotice.ChatID(chatID), backgroundnotice.StepIn(steps...)).
			Exec(ctx); err != nil {
			return fmt.Errorf("clear background notices: %w", err)
		}
	}
	for _, n := range outcome.Failed {
		if err := upsertBackgroundNoticeTx(ctx, tx, userID, chatID, n); err != nil {
			return fmt.Errorf("record background notice: %w", err)
		}
	}
	return nil
}

// upsertBackgroundNoticeTx creates a step's notice, counts it again, or replaces its cause.
func upsertBackgroundNoticeTx(ctx context.Context, tx *ent.Tx, userID, chatID uuid.UUID, n models.BackgroundNotice) error {
	existing, err := tx.BackgroundNotice.Query().
		Where(backgroundnotice.UserID(userID), backgroundnotice.ChatID(chatID), backgroundnotice.Step(string(n.Step))).
		Only(ctx)
	if ent.IsNotFound(err) {
		return tx.BackgroundNotice.Create().
			SetUserID(userID).SetChatID(chatID).SetStep(string(n.Step)).
			SetCode(n.Code).SetProvider(n.Provider).SetModel(n.Model).
			Exec(ctx)
	}
	if err != nil {
		return err
	}
	if existing.Code == n.Code {
		return existing.Update().SetProvider(n.Provider).SetModel(n.Model).AddCount(1).Exec(ctx)
	}
	// A new cause: show it again, even if the old one was dismissed.
	return existing.Update().SetCode(n.Code).SetProvider(n.Provider).SetModel(n.Model).
		SetCount(1).ClearDismissedAt().Exec(ctx)
}

// ListBackgroundNotices returns a chat's notices the account hasn't dismissed, newest first.
func (ds *Datastore) ListBackgroundNotices(ctx context.Context, userID, chatID uuid.UUID) ([]models.BackgroundNotice, error) {
	rows, err := ds.dbClient.BackgroundNotice.Query().
		Where(backgroundnotice.UserID(userID), backgroundnotice.ChatID(chatID), backgroundnotice.DismissedAtIsNil()).
		Order(ent.Desc(backgroundnotice.FieldUpdatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list background notices: %w", err)
	}
	out := make([]models.BackgroundNotice, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.BackgroundNotice{
			ID:        r.ID,
			ChatID:    r.ChatID,
			Step:      models.BackgroundStep(r.Step),
			Code:      r.Code,
			Provider:  r.Provider,
			Model:     r.Model,
			Count:     r.Count,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

// DismissBackgroundNotice hides a notice until its cause changes.
func (ds *Datastore) DismissBackgroundNotice(ctx context.Context, userID, chatID, id uuid.UUID) error {
	n, err := ds.dbClient.BackgroundNotice.Update().
		Where(backgroundnotice.ID(id), backgroundnotice.UserID(userID), backgroundnotice.ChatID(chatID)).
		SetDismissedAt(time.Now().UTC()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("dismiss background notice: %w", err)
	}
	if n == 0 {
		return ErrBackgroundNoticeNotFound
	}
	return nil
}
