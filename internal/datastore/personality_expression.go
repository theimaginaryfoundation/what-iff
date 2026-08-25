package datastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/personalityexpression"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ListPersonalityExpressions returns all expression slots for a user-owned personality.
func (d *Datastore) ListPersonalityExpressions(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityExpression, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	if err := ensureUserOwnsPersonality(ctx, tx, userID, personalityID); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	entExpressions, err := tx.PersonalityExpression.Query().
		Where(personalityexpression.HasPersonalityWith(personality.ID(personalityID))).
		WithImage().
		Order(ent.Asc(personalityexpression.FieldExpressionKey)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list personality expressions", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toPersonalityExpressionModels(entExpressions), nil
}

// UpsertPersonalityExpression creates or updates a user-defined expression slot.
func (d *Datastore) UpsertPersonalityExpression(ctx context.Context, userID, personalityID uuid.UUID, key string, req models.UpdatePersonalityExpressionRequest) (*models.PersonalityExpression, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	if err := ensureUserOwnsPersonality(ctx, tx, userID, personalityID); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if req.ImageSet && req.ImageID != nil {
		if err := ensureUserOwnsImage(ctx, tx, userID, *req.ImageID); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
	}

	entExpression, err := tx.PersonalityExpression.Query().
		Where(
			personalityexpression.ExpressionKey(key),
			personalityexpression.HasPersonalityWith(personality.ID(personalityID)),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		d.logger.Error("failed to query personality expression", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if ent.IsNotFound(err) {
		create := tx.PersonalityExpression.Create().
			SetExpressionKey(key).
			SetPersonalityID(personalityID)
		applyPersonalityExpressionCreate(create, req)
		entExpression, err = create.Save(ctx)
	} else {
		update := tx.PersonalityExpression.UpdateOneID(entExpression.ID)
		applyPersonalityExpressionUpdate(update, req)
		entExpression, err = update.Save(ctx)
	}
	if err != nil {
		d.logger.Error("failed to save personality expression", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	entExpression, err = tx.PersonalityExpression.Query().
		Where(personalityexpression.ID(entExpression.ID)).
		WithImage().
		Only(ctx)
	if err != nil {
		d.logger.Error("failed to reload personality expression", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toPersonalityExpressionModel(entExpression), nil
}

// DeletePersonalityExpression removes an expression slot from a user-owned personality.
func (d *Datastore) DeletePersonalityExpression(ctx context.Context, userID, personalityID uuid.UUID, key string) error {
	if strings.EqualFold(strings.TrimSpace(key), "thinking") {
		return ErrPersonalityExpressionNotDeletable
	}
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	if err := ensureUserOwnsPersonality(ctx, tx, userID, personalityID); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	_, err = tx.PersonalityExpression.Delete().
		Where(
			personalityexpression.ExpressionKey(key),
			personalityexpression.HasPersonalityWith(personality.ID(personalityID)),
		).
		Exec(ctx)
	if err != nil {
		d.logger.Error("failed to delete personality expression", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}
	return nil
}

func ensureUserOwnsPersonality(ctx context.Context, tx *ent.Tx, userID, personalityID uuid.UUID) error {
	exists, err := tx.Personality.Query().
		Where(
			personality.ID(personalityID),
			personality.HasUserWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrPersonalityNotFound
	}
	return nil
}

func ensureUserOwnsImage(ctx context.Context, tx *ent.Tx, userID, imageID uuid.UUID) error {
	exists, err := tx.FileAttachment.Query().
		Where(
			entfileattachment.ID(imageID),
			entfileattachment.FileTypeHasPrefix(models.ImageMIMEPrefix),
			entfileattachment.HasOwnerWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrFileAttachmentNotFound
	}
	return nil
}

func applyPersonalityExpressionCreate(create *ent.PersonalityExpressionCreate, req models.UpdatePersonalityExpressionRequest) {
	if req.LabelSet {
		create.SetNillableLabel(req.Label)
	}
	if req.ImageSet {
		create.SetNillableImageID(req.ImageID)
	}
}

func applyPersonalityExpressionUpdate(update *ent.PersonalityExpressionUpdateOne, req models.UpdatePersonalityExpressionRequest) {
	if req.LabelSet {
		if req.Label == nil {
			update.ClearLabel()
		} else {
			update.SetLabel(*req.Label)
		}
	}
	if req.ImageSet {
		if req.ImageID == nil {
			update.ClearImage()
		} else {
			update.SetImageID(*req.ImageID)
		}
	}
}

func toPersonalityExpressionModels(entExpressions []*ent.PersonalityExpression) []models.PersonalityExpression {
	expressions := make([]models.PersonalityExpression, 0, len(entExpressions))
	for _, entExpression := range entExpressions {
		if model := toPersonalityExpressionModel(entExpression); model != nil {
			expressions = append(expressions, *model)
		}
	}
	return expressions
}

func toPersonalityExpressionModel(entExpression *ent.PersonalityExpression) *models.PersonalityExpression {
	if entExpression == nil {
		return nil
	}

	model := &models.PersonalityExpression{
		ID:            entExpression.ID,
		ExpressionKey: entExpression.ExpressionKey,
		Label:         entExpression.Label,
		CreatedAt:     entExpression.CreatedAt,
		UpdatedAt:     entExpression.UpdatedAt,
	}
	if entExpression.Edges.Image != nil {
		imageID := entExpression.Edges.Image.ID
		model.ImageID = &imageID
		model.ImageURL = personalityExpressionImageURL(&imageID)
	}
	return model
}

func personalityExpressionImageURL(imageID *uuid.UUID) *string {
	if imageID == nil {
		return nil
	}
	url := fmt.Sprintf("/api/image-gallery/%s?size=full", imageID.String())
	return &url
}
