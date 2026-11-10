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

// SettleBackgroundNotices records what a run of background steps on a chat found: a succeeded
// step's notice is deleted, and a failed step's is created or updated. A notice the account
// dismissed stays dismissed while its cause (code, provider and model) stays the same.
//
// Each step is written on its own, so one that can't be written doesn't undo the others; the
// errors from any that failed are returned together.
func (ds *Datastore) SettleBackgroundNotices(ctx context.Context, userID, chatID uuid.UUID, outcome models.BackgroundOutcome) error {
	var errs []error
	if len(outcome.Succeeded) > 0 {
		steps := make([]string, 0, len(outcome.Succeeded))
		for _, s := range outcome.Succeeded {
			steps = append(steps, string(s))
		}
		if _, err := ds.dbClient.BackgroundNotice.Delete().
			Where(backgroundnotice.UserID(userID), backgroundnotice.ChatID(chatID), backgroundnotice.StepIn(steps...)).
			Exec(ctx); err != nil {
			errs = append(errs, fmt.Errorf("clear background notices: %w", err))
		}
	}
	for _, n := range outcome.Failed {
		if err := ds.recordBackgroundNotice(ctx, userID, chatID, n); err != nil {
			errs = append(errs, fmt.Errorf("record background notice for %s: %w", n.Step, err))
		}
	}
	return errors.Join(errs...)
}

// recordBackgroundNotice upserts one step's notice in a transaction of its own, so reading the
// existing notice and writing it back can't interleave with another write.
func (ds *Datastore) recordBackgroundNotice(ctx context.Context, userID, chatID uuid.UUID, n models.BackgroundNotice) error {
	tx, err := ds.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	if err := upsertBackgroundNoticeTx(ctx, tx, userID, chatID, n); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
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
	// The cause is the code and where it happened: the same error from another provider or model
	// is a different problem, which the account hasn't seen yet.
	if existing.Code == n.Code && existing.Provider == n.Provider && existing.Model == n.Model {
		return existing.Update().AddCount(1).Exec(ctx)
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
