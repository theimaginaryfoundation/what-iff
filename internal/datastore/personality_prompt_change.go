package datastore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/mood"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/personalitypromptchange"
	"github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

var ErrPersonalityPromptChangeNotFound = fmt.Errorf("personality prompt change not found")

func toPersonalityPromptChangeModel(row *ent.PersonalityPromptChange) *models.PersonalityPromptChange {
	if row == nil {
		return nil
	}
	return &models.PersonalityPromptChange{
		ID:               row.ID,
		UserID:           row.UserID,
		PersonalityID:    row.PersonalityID,
		OldPrompt:        row.OldPrompt,
		NewPrompt:        row.NewPrompt,
		Action:           models.PersonalityPromptChangeAction(row.Action),
		RevertedChangeID: row.RevertedChangeID,
		CreatedAt:        row.CreatedAt,
	}
}

func createPersonalityPromptChangeTx(
	ctx context.Context,
	tx *ent.Tx,
	userID, personalityID uuid.UUID,
	oldPrompt, newPrompt string,
	action personalitypromptchange.Action,
	revertedChangeID *uuid.UUID,
) (*ent.PersonalityPromptChange, error) {
	create := tx.PersonalityPromptChange.Create().
		SetUserID(userID).
		SetPersonalityID(personalityID).
		SetOldPrompt(oldPrompt).
		SetNewPrompt(newPrompt).
		SetAction(action)
	if revertedChangeID != nil {
		create = create.SetRevertedChangeID(*revertedChangeID)
	}
	return create.Save(ctx)
}

// UpdatePersonalityWithPromptHistory performs the normal personality PUT and,
// when system_prompt changes, appends the before/after transition in the same
// transaction. The existing UpdatePersonality method remains available for
// internal callers that deliberately do not represent a user prompt edit.
func (d *Datastore) UpdatePersonalityWithPromptHistory(ctx context.Context, userID uuid.UUID, personalityModel models.Personality) (*models.Personality, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	personalityQuery := tx.Personality.Query().Where(
		personality.ID(personalityModel.ID),
		personality.HasUserWith(user.ID(userID)),
	)
	currentPersonality, err := personalityQuery.Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, ErrPersonalityNotFound
		}
		return nil, err
	}

	if personalityModel.CoverImageID != nil {
		if err := ensureUserOwnsImage(ctx, tx, userID, *personalityModel.CoverImageID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}

	history := buildScratchpadHistory(currentPersonality.Scratchpad, currentPersonality.ScratchpadHistory)
	update := tx.Personality.UpdateOneID(personalityModel.ID).
		SetName(personalityModel.Name).
		SetSystemPrompt(personalityModel.SystemPrompt).
		SetScratchpad(personalityModel.Scratchpad).
		SetArchivalModel(personalityModel.ArchivalModel).
		SetScratchpadUpdatePrompt(personalityModel.ScratchpadUpdatePrompt).
		SetMemorySearchPrompt(personalityModel.MemorySearchPrompt).
		SetMemoryWritePrompt(personalityModel.MemoryWritePrompt).
		SetAutoPinMemories(personalityModel.AutoPinMemories).
		SetScratchpadHistory(history).
		SetExpressionsEnabled(personalityModel.ExpressionsEnabled).
		SetImageStyle(personalityModel.ImageStyle)
	if personalityModel.AccentColor != nil {
		update = update.SetAccentColor(*personalityModel.AccentColor)
	} else {
		update = update.ClearAccentColor()
	}
	if personalityModel.ThumbnailCircle != nil {
		update = update.SetThumbnailCircle(&schema.PersonalityThumbnailCircle{
			CX: personalityModel.ThumbnailCircle.CX,
			CY: personalityModel.ThumbnailCircle.CY,
			R:  personalityModel.ThumbnailCircle.R,
		})
	} else {
		update = update.ClearThumbnailCircle()
	}
	if personalityModel.CoverImageID != nil {
		update = update.SetCoverImageID(*personalityModel.CoverImageID)
	} else {
		update = update.ClearCoverImage()
	}

	entPersonality, err := update.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if currentPersonality.SystemPrompt != personalityModel.SystemPrompt {
		if _, err := createPersonalityPromptChangeTx(
			ctx,
			tx,
			userID,
			personalityModel.ID,
			currentPersonality.SystemPrompt,
			personalityModel.SystemPrompt,
			personalitypromptchange.ActionEdit,
			nil,
		); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}

	entPersonality, err = tx.Personality.Query().
		Where(personality.ID(entPersonality.ID)).
		WithCoverImage().
		WithMoods(func(q *ent.MoodQuery) { q.Select(mood.FieldID) }).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toPersonalityModel(entPersonality), nil
}

// ListPersonalityPromptChanges returns immutable prompt transitions newest first.
func (d *Datastore) ListPersonalityPromptChanges(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityPromptChange, error) {
	owned, err := d.dbClient.Personality.Query().
		Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrPersonalityNotFound
	}

	rows, err := d.dbClient.PersonalityPromptChange.Query().
		Where(
			personalitypromptchange.UserID(userID),
			personalitypromptchange.PersonalityID(personalityID),
		).
		Order(ent.Desc(personalitypromptchange.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.PersonalityPromptChange, 0, len(rows))
	for _, row := range rows {
		if item := toPersonalityPromptChangeModel(row); item != nil {
			out = append(out, *item)
		}
	}
	return out, nil
}

// RevertPersonalityPromptChange restores the old_prompt captured by a historical
// change and appends a new revert event. Historical rows are never rewritten.
func (d *Datastore) RevertPersonalityPromptChange(ctx context.Context, userID, personalityID, changeID uuid.UUID) (*models.PersonalityPromptChange, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	change, err := tx.PersonalityPromptChange.Query().
		Where(
			personalitypromptchange.ID(changeID),
			personalitypromptchange.UserID(userID),
			personalitypromptchange.PersonalityID(personalityID),
		).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, ErrPersonalityPromptChangeNotFound
		}
		return nil, err
	}

	current, err := tx.Personality.Query().
		Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, ErrPersonalityNotFound
		}
		return nil, err
	}

	if current.SystemPrompt == change.OldPrompt {
		_ = tx.Rollback()
		return toPersonalityPromptChangeModel(change), nil
	}

	if _, err := tx.Personality.UpdateOneID(personalityID).SetSystemPrompt(change.OldPrompt).Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	revert, err := createPersonalityPromptChangeTx(
		ctx,
		tx,
		userID,
		personalityID,
		current.SystemPrompt,
		change.OldPrompt,
		personalitypromptchange.ActionRevert,
		&change.ID,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toPersonalityPromptChangeModel(revert), nil
}
