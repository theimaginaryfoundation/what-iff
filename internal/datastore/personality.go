package datastore

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/mood"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const maxScratchpadHistoryEntries = 10

var preferredPersonalityCoverExpressionKeys = []string{"default", "neutral", "happy", "content"}

func buildScratchpadHistory(current string, history []string) []string {
	result := append([]string(nil), history...)
	if current != "" {
		result = append([]string{current}, result...)
	}

	if len(result) > maxScratchpadHistoryEntries {
		result = result[:maxScratchpadHistoryEntries]
	}

	if len(result) == 0 {
		return []string{}
	}

	return result
}

func fallbackPersonalityCoverImageID(expressions []*ent.PersonalityExpression) *uuid.UUID {
	if len(expressions) == 0 {
		return nil
	}

	byKey := make(map[string]*ent.PersonalityExpression, len(expressions))
	for _, expression := range expressions {
		if expression != nil {
			byKey[expression.ExpressionKey] = expression
		}
	}

	for _, key := range preferredPersonalityCoverExpressionKeys {
		if imageID := expressionImageID(byKey[key]); imageID != nil {
			return imageID
		}
	}

	for _, expression := range expressions {
		if imageID := expressionImageID(expression); imageID != nil {
			return imageID
		}
	}

	return nil
}

func expressionImageID(expression *ent.PersonalityExpression) *uuid.UUID {
	if expression == nil || expression.Edges.Image == nil {
		return nil
	}
	imageID := expression.Edges.Image.ID
	return &imageID
}

// Convert from Ent Personality to model
func toPersonalityModel(e *ent.Personality) *models.Personality {
	if e == nil {
		return nil
	}

	history := make([]string, len(e.ScratchpadHistory))
	copy(history, e.ScratchpadHistory)

	m := &models.Personality{
		ID:                     e.ID,
		Name:                   e.Name,
		SystemPrompt:           e.SystemPrompt,
		Scratchpad:             e.Scratchpad,
		ScratchpadHistory:      history,
		ArchivalModel:          e.ArchivalModel,
		ScratchpadUpdatePrompt: e.ScratchpadUpdatePrompt,
		MemorySearchPrompt:     e.MemorySearchPrompt,
		MemoryWritePrompt:      e.MemoryWritePrompt,
		AutoPinMemories:        e.AutoPinMemories,
		AccentColor:            e.AccentColor,
		ExpressionsEnabled:     e.ExpressionsEnabled,
		ImageStyle:             e.ImageStyle,
		CreatedAt:              e.CreatedAt,
		UpdatedAt:              e.UpdatedAt,
	}
	if e.ThumbnailCircle != nil {
		m.ThumbnailCircle = &models.PersonalityThumbnailCircle{
			CX: e.ThumbnailCircle.CX,
			CY: e.ThumbnailCircle.CY,
			R:  e.ThumbnailCircle.R,
		}
	}

	for _, k := range e.Edges.Moods {
		m.MoodIDs = append(m.MoodIDs, k.ID)
	}

	if e.Edges.CoverImage != nil {
		coverID := e.Edges.CoverImage.ID
		m.CoverImageID = &coverID
		m.CoverImageURL = personalityExpressionImageURL(&coverID)
	} else if coverID := fallbackPersonalityCoverImageID(e.Edges.Expressions); coverID != nil {
		m.CoverImageID = coverID
		m.CoverImageURL = personalityExpressionImageURL(coverID)
	}

	return m
}

// CreatePersonality persists a new personality to the datastore
func (d *Datastore) CreatePersonality(ctx context.Context, userID uuid.UUID, personalityModel models.Personality) (*models.Personality, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if user exists
	userExists, err := tx.User.Query().
		Where(user.ID(userID)).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "user"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !userExists {
		d.logger.Error(i18n.T1("user.not_found", "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrUnauthorized
	}

	if personalityModel.CoverImageID != nil {
		if err := ensureUserOwnsImage(ctx, tx, userID, *personalityModel.CoverImageID); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
	}

	// Create personality
	create := tx.Personality.Create().
		SetName(personalityModel.Name).
		SetSystemPrompt(personalityModel.SystemPrompt).
		SetScratchpad(personalityModel.Scratchpad).
		SetUserID(userID).
		SetExpressionsEnabled(personalityModel.ExpressionsEnabled).
		SetImageStyle(personalityModel.ImageStyle)
	if personalityModel.CoverImageID != nil {
		create = create.SetCoverImageID(*personalityModel.CoverImageID)
	}
	if personalityModel.AccentColor != nil {
		create = create.SetAccentColor(*personalityModel.AccentColor)
	}
	if personalityModel.ThumbnailCircle != nil {
		create = create.SetThumbnailCircle(&schema.PersonalityThumbnailCircle{
			CX: personalityModel.ThumbnailCircle.CX,
			CY: personalityModel.ThumbnailCircle.CY,
			R:  personalityModel.ThumbnailCircle.R,
		})
	}

	entPersonality, err := create.Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if personalityModel.CoverImageID != nil {
		entPersonality, err = tx.Personality.Query().
			Where(personality.ID(entPersonality.ID)).
			WithCoverImage().
			Only(ctx)
		if err != nil {
			d.logger.Error("failed to reload personality with cover image", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toPersonalityModel(entPersonality), nil
}

// GetPersonality retrieves a personality from the datastore by ID
func (d *Datastore) GetPersonality(ctx context.Context, userID, id uuid.UUID) (*models.Personality, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Query personality with authorization check; load attached moods and cover image.
	entPersonality, err := tx.Personality.Query().
		Where(
			personality.ID(id),
			personality.HasUserWith(
				user.ID(userID),
			),
		).
		WithMoods(func(q *ent.MoodQuery) { q.Select(mood.FieldID) }).
		WithCoverImage().
		WithExpressions(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		}).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("personality.not_found_or_unauthorized", "PersonalityID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrPersonalityNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toPersonalityModel(entPersonality), nil
}

// ListPersonalities returns a paginated list of personalities for a user that match optional filter criteria
func (d *Datastore) ListPersonalities(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.PersonalityFilters) (*models.PaginatedResponse, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Build query with user authorization
	query := tx.Personality.Query().
		Where(
			personality.HasUserWith(
				user.ID(userID),
			),
		)

	// Apply filters if provided
	if filters.Name != nil && *filters.Name != "" {
		query = query.Where(personality.NameContainsFold(*filters.Name))
	}

	// Free-text search across the personality's display name and system prompt
	// so callers (e.g. the cross-resource search endpoint) can match on either.
	if filters.Query != nil && *filters.Query != "" {
		query = query.Where(personality.Or(
			personality.NameContainsFold(*filters.Query),
			personality.SystemPromptContainsFold(*filters.Query),
		))
	}

	if filters.MinDate != nil {
		query = query.Where(personality.CreatedAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(personality.CreatedAtLTE(*filters.MaxDate))
	}

	if len(filters.IDs) > 0 {
		query = query.Where(personality.IDIn(filters.IDs...))
	}

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "personalities"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Apply pagination
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	offset := (pageNum - 1) * pageSize
	query = query.
		Offset(offset).
		Limit(pageSize).
		Order(ent.Desc(personality.FieldCreatedAt)).
		// Load attached mood IDs for the chat mood selector.
		WithMoods(func(q *ent.MoodQuery) { q.Select(mood.FieldID) }).
		WithCoverImage().
		WithExpressions(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		})

	// Execute query
	entPersonalities, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "personalities"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	personalityIDs := make([]uuid.UUID, len(entPersonalities))
	for i, entPersonality := range entPersonalities {
		personalityIDs[i] = entPersonality.ID
	}
	statsByPersonalityID, err := d.personalityUsageStats(ctx, tx, userID, personalityIDs)
	if err != nil {
		d.logger.Error("failed to query personality usage stats", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	personalityModels := make([]any, len(entPersonalities))
	for i, entPersonality := range entPersonalities {
		personalityModel := toPersonalityModel(entPersonality)
		if personalityModel != nil {
			personalityModel.Stats = statsByPersonalityID[entPersonality.ID]
		}
		personalityModels[i] = personalityModel
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    personalityModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

func (d *Datastore) personalityUsageStats(ctx context.Context, tx *ent.Tx, userID uuid.UUID, personalityIDs []uuid.UUID) (map[uuid.UUID]models.PersonalityUsageStats, error) {
	if len(personalityIDs) == 0 {
		return personalityUsageStatsFromChats(personalityIDs, nil), nil
	}

	entChats, err := tx.Chat.Query().
		Where(
			entchat.HasOwnerWith(user.ID(userID)),
			entchat.Archived(false),
			entchat.HasPersonalityWith(personality.IDIn(personalityIDs...)),
		).
		WithPersonality(func(q *ent.PersonalityQuery) {
			q.Select(personality.FieldID)
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return personalityUsageStatsFromChats(personalityIDs, entChats), nil
}

func personalityUsageStatsFromChats(personalityIDs []uuid.UUID, entChats []*ent.Chat) map[uuid.UUID]models.PersonalityUsageStats {
	statsByPersonalityID := make(map[uuid.UUID]models.PersonalityUsageStats, len(personalityIDs))
	for _, personalityID := range personalityIDs {
		statsByPersonalityID[personalityID] = models.PersonalityUsageStats{}
	}

	for _, entChat := range entChats {
		if entChat.Edges.Personality == nil {
			continue
		}

		personalityID := entChat.Edges.Personality.ID
		stats := statsByPersonalityID[personalityID]
		stats.ChatCount++

		lastUsedAt := personalityLastUsedAt(entChat)
		if stats.LastUsedAt == nil || lastUsedAt.After(*stats.LastUsedAt) {
			lastUsedAtCopy := lastUsedAt
			stats.LastUsedAt = &lastUsedAtCopy
		}

		statsByPersonalityID[personalityID] = stats
	}

	return statsByPersonalityID
}

func personalityLastUsedAt(entChat *ent.Chat) time.Time {
	if entChat.LastMessageTime != nil {
		return *entChat.LastMessageTime
	}
	return entChat.UpdatedAt
}

// UpdatePersonality updates an existing personality
func (d *Datastore) UpdatePersonality(ctx context.Context, userID uuid.UUID, personalityModel models.Personality) (*models.Personality, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	personalityQuery := tx.Personality.Query().
		Where(
			personality.ID(personalityModel.ID),
			personality.HasUserWith(
				user.ID(userID),
			),
		)

	currentPersonality, err := personalityQuery.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("personality.not_found_or_unauthorized", "PersonalityID", personalityModel.ID.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrPersonalityNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if personalityModel.CoverImageID != nil {
		if err := ensureUserOwnsImage(ctx, tx, userID, *personalityModel.CoverImageID); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
	}

	// Update personality
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
	// PUT semantics: an explicit cover_image_id sets it; nil/omitted clears it.
	if personalityModel.CoverImageID != nil {
		update = update.SetCoverImageID(*personalityModel.CoverImageID)
	} else {
		update = update.ClearCoverImage()
	}

	entPersonality, err := update.Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Reload to get the cover image edge populated for the response.
	entPersonality, err = tx.Personality.Query().
		Where(personality.ID(entPersonality.ID)).
		WithCoverImage().
		WithMoods(func(q *ent.MoodQuery) { q.Select(mood.FieldID) }).
		Only(ctx)
	if err != nil {
		d.logger.Error("failed to reload personality with cover image", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toPersonalityModel(entPersonality), nil
}

// UpdatePersonalityScratchpad updates the scratchpad of a personality
func (d *Datastore) UpdatePersonalityScratchpad(ctx context.Context, userID uuid.UUID, personalityModel models.Personality) (*models.Personality, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	personalityQuery := tx.Personality.Query().
		Where(
			personality.ID(personalityModel.ID),
			personality.HasUserWith(
				user.ID(userID),
			),
		)

	currentPersonality, err := personalityQuery.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("personality.not_found_or_unauthorized", "PersonalityID", personalityModel.ID.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrPersonalityNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	history := buildScratchpadHistory(currentPersonality.Scratchpad, currentPersonality.ScratchpadHistory)

	entPersonality, err := tx.Personality.UpdateOneID(personalityModel.ID).
		SetScratchpad(personalityModel.Scratchpad).
		SetScratchpadHistory(history).
		Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toPersonalityModel(entPersonality), nil
}

// DeletePersonality deletes a personality by ID
func (d *Datastore) DeletePersonality(ctx context.Context, userID, id uuid.UUID) error {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if personality exists and belongs to the user
	exists, err := tx.Personality.Query().
		Where(
			personality.ID(id),
			personality.HasUserWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		d.logger.Error(i18n.T2("personality.not_found_or_unauthorized", "PersonalityID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrPersonalityNotFound
	}

	// Delete personality
	err = tx.Personality.DeleteOneID(id).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	return nil
}

// SetPersonalityMoods replaces the full set of moods attached to a personality.
// All provided mood IDs must be owned by userID, otherwise ErrMoodNotFound is returned.
func (d *Datastore) SetPersonalityMoods(ctx context.Context, userID, personalityID uuid.UUID, moodIDs []uuid.UUID) error {
	// Verify personality ownership.
	exists, err := d.dbClient.Personality.Query().
		Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrPersonalityNotFound
	}
	if len(moodIDs) > 0 {
		count, countErr := d.dbClient.Mood.Query().
			Where(
				mood.IDIn(moodIDs...),
				mood.HasOwnerWith(user.ID(userID)),
			).
			Count(ctx)
		if countErr != nil {
			return countErr
		}
		if count != len(moodIDs) {
			return ErrMoodNotFound
		}
	}

	update := d.dbClient.Personality.UpdateOneID(personalityID).ClearMoods()
	if len(moodIDs) > 0 {
		update.AddMoodIDs(moodIDs...)
	}
	return update.Exec(ctx)
}
