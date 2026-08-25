package datastore

import (
	"context"
	"encoding/base64"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	entmood "github.com/theimaginaryfoundation/what-iff/ent/mood"
	entpersonality "github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	entuser "github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func toMoodModel(e *ent.Mood) *models.Mood {
	if e == nil {
		return nil
	}

	m := models.Mood{
		ID:               e.ID,
		Name:             e.Name,
		Description:      e.Description,
		PromptSnippet:    e.PromptSnippet,
		RecommendedModel: e.RecommendedModel,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}

	if len(e.ThumbnailData) > 0 {
		m.ThumbnailData = base64.StdEncoding.EncodeToString(e.ThumbnailData)
	}

	for _, img := range e.Edges.Images {
		m.ImageIDs = append(m.ImageIDs, img.ID)
	}
	for _, r := range e.Edges.Rituals {
		m.RitualIDs = append(m.RitualIDs, r.ID)
	}
	for _, p := range e.Edges.Personalities {
		m.PersonalityIDs = append(m.PersonalityIDs, p.ID)
	}

	return &m
}

// CreateMood persists a new mood for a user.
func (d *Datastore) CreateMood(ctx context.Context, userID uuid.UUID, req models.CreateMoodRequest) (*models.Mood, error) {
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

	if len(req.ImageIDs) > 0 {
		count, countErr := tx.FileAttachment.Query().
			Where(
				entfileattachment.IDIn(req.ImageIDs...),
				entfileattachment.HasOwnerWith(entuser.ID(userID)),
				entfileattachment.FileTypeHasPrefix("image/"),
			).
			Count(ctx)
		if countErr != nil {
			tx.Rollback()
			return nil, countErr
		}
		if count != len(req.ImageIDs) {
			tx.Rollback()
			return nil, ErrFileAttachmentNotFound
		}
	}

	if len(req.RitualIDs) > 0 {
		count, countErr := tx.Ritual.Query().
			Where(
				entritual.IDIn(req.RitualIDs...),
				entritual.HasOwnerWith(entuser.ID(userID)),
			).
			Count(ctx)
		if countErr != nil {
			tx.Rollback()
			return nil, countErr
		}
		if count != len(req.RitualIDs) {
			tx.Rollback()
			return nil, ErrRitualNotFound
		}
	}

	create := tx.Mood.Create().
		SetName(req.Name).
		SetDescription(req.Description).
		SetPromptSnippet(req.PromptSnippet).
		SetOwnerID(userID)

	if len(req.ImageIDs) > 0 {
		create.AddImageIDs(req.ImageIDs...)
	}
	if len(req.RitualIDs) > 0 {
		create.AddRitualIDs(req.RitualIDs...)
	}

	created, err := create.Save(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	result, err := tx.Mood.Query().
		Where(entmood.ID(created.ID)).
		WithImages().
		WithRituals().
		Only(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toMoodModel(result), nil
}

// GetMood returns a mood by ID, verifying ownership.
func (d *Datastore) GetMood(ctx context.Context, userID, id uuid.UUID) (*models.Mood, error) {
	e, err := d.dbClient.Mood.Query().
		Where(entmood.ID(id), entmood.HasOwnerWith(entuser.ID(userID))).
		WithImages().
		WithRituals().
		WithPersonalities(func(q *ent.PersonalityQuery) { q.Select(entpersonality.FieldID) }).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrMoodNotFound
		}
		return nil, err
	}
	return toMoodModel(e), nil
}

// ListMoods returns a paginated list of moods owned by a user.
func (d *Datastore) ListMoods(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MoodFilters) (*models.PaginatedResponse, error) {
	query := d.dbClient.Mood.Query().
		Where(entmood.HasOwnerWith(entuser.ID(userID))).
		WithImages(func(q *ent.FileAttachmentQuery) { q.Select(entfileattachment.FieldID) }).
		WithRituals(func(q *ent.RitualQuery) { q.Select(entritual.FieldID) }).
		WithPersonalities(func(q *ent.PersonalityQuery) { q.Select(entpersonality.FieldID) })

	if filters.Name != nil && *filters.Name != "" {
		query = query.Where(entmood.NameContainsFold(*filters.Name))
	}
	if filters.MinDate != nil {
		query = query.Where(entmood.CreatedAtGTE(*filters.MinDate))
	}
	if filters.MaxDate != nil {
		query = query.Where(entmood.CreatedAtLTE(*filters.MaxDate))
	}

	total, err := query.Count(ctx)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	moods, err := query.
		Order(ent.Desc(entmood.FieldCreatedAt)).
		Offset((pageNum - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]any, len(moods))
	for i, k := range moods {
		results[i] = toMoodModel(k)
	}

	return &models.PaginatedResponse{Results: results, TotalCount: total, Page: pageNum}, nil
}

// UpdateMood updates a mood's fields and (optionally) its image/ritual associations.
// ThumbnailData is updated separately via SetMoodThumbnail.
func (d *Datastore) UpdateMood(ctx context.Context, userID, id uuid.UUID, req models.UpdateMoodRequest) (*models.Mood, error) {
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

	exists, err := tx.Mood.Query().
		Where(entmood.ID(id), entmood.HasOwnerWith(entuser.ID(userID))).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if !exists {
		tx.Rollback()
		return nil, ErrMoodNotFound
	}

	if req.ImageIDs != nil && len(*req.ImageIDs) > 0 {
		count, countErr := tx.FileAttachment.Query().
			Where(
				entfileattachment.IDIn(*req.ImageIDs...),
				entfileattachment.HasOwnerWith(entuser.ID(userID)),
				entfileattachment.FileTypeHasPrefix("image/"),
			).
			Count(ctx)
		if countErr != nil {
			tx.Rollback()
			return nil, countErr
		}
		if count != len(*req.ImageIDs) {
			tx.Rollback()
			return nil, ErrFileAttachmentNotFound
		}
	}

	if req.RitualIDs != nil && len(*req.RitualIDs) > 0 {
		count, countErr := tx.Ritual.Query().
			Where(
				entritual.IDIn(*req.RitualIDs...),
				entritual.HasOwnerWith(entuser.ID(userID)),
			).
			Count(ctx)
		if countErr != nil {
			tx.Rollback()
			return nil, countErr
		}
		if count != len(*req.RitualIDs) {
			tx.Rollback()
			return nil, ErrRitualNotFound
		}
	}

	update := tx.Mood.UpdateOneID(id).
		SetName(req.Name).
		SetDescription(req.Description).
		SetPromptSnippet(req.PromptSnippet).
		SetRecommendedModel(req.RecommendedModel)

	if req.ImageIDs != nil {
		update.ClearImages().AddImageIDs(*req.ImageIDs...)
	}
	if req.RitualIDs != nil {
		update.ClearRituals().AddRitualIDs(*req.RitualIDs...)
	}

	if _, err := update.Save(ctx); err != nil {
		tx.Rollback()
		return nil, err
	}

	result, err := tx.Mood.Query().
		Where(entmood.ID(id)).
		WithImages().
		WithRituals().
		Only(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toMoodModel(result), nil
}

// SetMoodThumbnail stores a JPEG thumbnail on the mood record (generated size is handler-defined).
func (d *Datastore) SetMoodThumbnail(ctx context.Context, id uuid.UUID, jpegData []byte) error {
	return d.dbClient.Mood.UpdateOneID(id).SetThumbnailData(jpegData).Exec(ctx)
}

// DeleteMood removes a mood. Any personality default_mood FK pointing to this mood
// is cleared first to avoid FK constraint failures.
func (d *Datastore) DeleteMood(ctx context.Context, userID, id uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.Mood.Query().
		Where(entmood.ID(id), entmood.HasOwnerWith(entuser.ID(userID))).
		Exist(ctx)
	if err != nil {
		tx.Rollback()
		return err
	}
	if !exists {
		tx.Rollback()
		return ErrMoodNotFound
	}

	if err := tx.Mood.DeleteOneID(id).Exec(ctx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// SetMoodPersonalities replaces all personality associations for a mood.
// The personality list is synced from the mood side via the M2M join table.
func (d *Datastore) SetMoodPersonalities(ctx context.Context, userID, moodID uuid.UUID, personalityIDs []uuid.UUID) error {
	// Verify mood ownership.
	exists, err := d.dbClient.Mood.Query().
		Where(entmood.ID(moodID), entmood.HasOwnerWith(entuser.ID(userID))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrMoodNotFound
	}
	if len(personalityIDs) > 0 {
		count, countErr := d.dbClient.Personality.Query().
			Where(
				entpersonality.IDIn(personalityIDs...),
				entpersonality.HasUserWith(entuser.ID(userID)),
			).
			Count(ctx)
		if countErr != nil {
			return countErr
		}
		if count != len(personalityIDs) {
			return ErrPersonalityNotFound
		}
	}

	update := d.dbClient.Mood.UpdateOneID(moodID).ClearPersonalities()
	if len(personalityIDs) > 0 {
		update.AddPersonalityIDs(personalityIDs...)
	}
	return update.Exec(ctx)
}

// GetMoodsForPersonality returns all moods attached to a personality, with full details
// (including rituals for context injection).
func (d *Datastore) GetMoodsForPersonality(ctx context.Context, userID, personalityID uuid.UUID) ([]*models.Mood, error) {
	moods, err := d.dbClient.Mood.Query().
		Where(
			entmood.HasOwnerWith(entuser.ID(userID)),
			entmood.HasPersonalitiesWith(entpersonality.ID(personalityID)),
		).
		WithRituals(func(q *ent.RitualQuery) { q.Select(entritual.FieldID) }).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*models.Mood, len(moods))
	for i, k := range moods {
		result[i] = toMoodModel(k)
	}
	return result, nil
}
