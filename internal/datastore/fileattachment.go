package datastore

import (
	"context"
	"sort"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	entchatmessage "github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entfileattachment "github.com/theimaginaryfoundation/what-iff/ent/fileattachment"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/personalityexpression"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Convert from Ent FileAttachment to model
func toFileAttachmentModel(e *ent.FileAttachment) *models.FileAttachment {
	if e == nil {
		return nil
	}

	userID := uuid.Nil
	if e.Edges.Owner != nil {
		userID = e.Edges.Owner.ID
	}

	fileAttachmentModel := models.FileAttachment{
		ID:          e.ID,
		UserID:      userID,
		Name:        e.Name,
		FileType:    e.FileType,
		Description: e.Description,
		S3Key:       e.S3Key,
		CreatedAt:   e.CreatedAt,
	}

	if e.FileID != "" {
		fileAttachmentModel.FileID = &e.FileID
	}

	if e.Edges.ChatMessage != nil {
		chatMessageID := e.Edges.ChatMessage.ID
		fileAttachmentModel.ChatMessageID = &chatMessageID
		if e.Edges.ChatMessage.Edges.Chat != nil {
			chatID := e.Edges.ChatMessage.Edges.Chat.ID
			fileAttachmentModel.ChatID = &chatID
		}
	}
	if e.Edges.Personality != nil {
		personalityID := e.Edges.Personality.ID
		fileAttachmentModel.PersonalityID = &personalityID
		fileAttachmentModel.Personalities = []models.FileAttachmentPersonalityRef{
			{
				ID:   e.Edges.Personality.ID,
				Name: e.Edges.Personality.Name,
			},
		}
	}

	return &fileAttachmentModel
}

// CreateFileAttachment persists a new file attachment to the datastore
func (d *Datastore) CreateFileAttachment(ctx context.Context, userID uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error) {
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

	// Create file attachment
	create := tx.FileAttachment.Create().
		SetName(fileAttachment.Name).
		SetFileType(fileAttachment.FileType).
		SetOwnerID(userID)
	if fileAttachment.Description != nil {
		create.SetDescription(*fileAttachment.Description)
	}

	if fileAttachment.S3Key != "" {
		create.SetS3Key(fileAttachment.S3Key)
	}

	if fileAttachment.FileID != nil {
		create.SetFileID(*fileAttachment.FileID)
	}

	if fileAttachment.ChatMessageID != nil {
		create.SetChatMessageID(*fileAttachment.ChatMessageID)
	}

	if fileAttachment.PersonalityID != nil {
		create.SetPersonalityID(*fileAttachment.PersonalityID)
	}

	entFileAttachment, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "file attachment"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load relationships needed for model conversion
	entFileAttachment, err = tx.FileAttachment.Query().
		Where(entfileattachment.ID(entFileAttachment.ID)).
		WithOwner().
		WithChatMessage().
		WithPersonality().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("file_attachment.relationships_load_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	model := toFileAttachmentModel(entFileAttachment)
	if err := d.populateAttachmentPersonalities(ctx, tx, userID, []*ent.FileAttachment{entFileAttachment}, map[uuid.UUID]*models.FileAttachment{
		model.ID: model,
	}); err != nil {
		d.logger.Error("failed to populate file attachment personalities", zap.Error(err))
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

	return model, nil
}

// GetFileAttachment retrieves a file attachment from the datastore by ID
func (d *Datastore) GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error) {
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

	// Query file attachment with authorization check.
	// WithChatMessage + WithChat lets callers derive FileKeyForChat for gallery fallback.
	entFileAttachment, err := tx.FileAttachment.Query().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithChatMessage(func(q *ent.ChatMessageQuery) {
			q.WithChat()
		}).
		WithPersonality().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("file_attachment.not_found_or_unauthorized", "FileAttachmentID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrFileAttachmentNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "file attachment"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	model := toFileAttachmentModel(entFileAttachment)
	if err := d.populateAttachmentPersonalities(ctx, tx, userID, []*ent.FileAttachment{entFileAttachment}, map[uuid.UUID]*models.FileAttachment{
		model.ID: model,
	}); err != nil {
		d.logger.Error("failed to populate file attachment personalities", zap.Error(err))
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

	return model, nil
}

// ListFileAttachments returns a paginated list of file attachments for a user that match optional filter criteria
func (d *Datastore) ListFileAttachments(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
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
	query := tx.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithChatMessage().
		WithPersonality()

	// Apply filters if provided
	if filters.Name != nil && *filters.Name != "" {
		search := *filters.Name
		expressionImageIDs, exprErr := tx.PersonalityExpression.Query().
			Where(personalityexpression.HasPersonalityWith(personality.NameContainsFold(search))).
			QueryImage().
			IDs(ctx)
		if exprErr != nil {
			d.logger.Error("failed to list expression image ids for search filter", zap.Error(exprErr))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, exprErr
		}
		predicates := []predicate.FileAttachment{
			entfileattachment.NameContainsFold(search),
			entfileattachment.DescriptionContainsFold(search),
			entfileattachment.HasChatMessageWith(
				entchatmessage.HasChatWith(chat.NameContainsFold(search)),
			),
			entfileattachment.HasPersonalityWith(personality.NameContainsFold(search)),
		}
		if len(expressionImageIDs) > 0 {
			predicates = append(predicates, entfileattachment.IDIn(expressionImageIDs...))
		}
		query = query.Where(entfileattachment.Or(predicates...))
	}

	if filters.FileType != nil && *filters.FileType != "" {
		query = query.Where(entfileattachment.FileTypeContainsFold(*filters.FileType))
	}

	if filters.ChatMessageID != nil {
		query = query.Where(entfileattachment.HasChatMessageWith(
			entchatmessage.ID(*filters.ChatMessageID),
		))
	}

	if filters.PersonalityID != nil {
		if filters.DocsOnly != nil && *filters.DocsOnly {
			// Restrict to user-uploaded RAG documents: must have a direct
			// personality_id FK and must NOT be an image type.
			// Expression images (expression-*.png) and cover photos are stored
			// with personality_id set, so the image-type exclusion is the only
			// way to reliably separate them from text/doc uploads. User-uploaded
			// docs cannot be images (the upload form enforces text/doc MIME
			// types), so this filter is safe.
			query = query.Where(
				entfileattachment.HasPersonalityWith(personality.ID(*filters.PersonalityID)),
				entfileattachment.Not(entfileattachment.FileTypeHasPrefix(models.ImageMIMEPrefix)),
			)
		} else {
			// Collect expression image IDs for this personality. The
			// HasPersonalityWith predicate inside the subquery scopes results
			// exclusively to the requested personality, so expressionImageIDs
			// cannot leak attachments from other personalities.
			expressionImageIDs, exprErr := tx.PersonalityExpression.Query().
				Where(personalityexpression.HasPersonalityWith(personality.ID(*filters.PersonalityID))).
				QueryImage().
				IDs(ctx)
			if exprErr != nil {
				d.logger.Error("failed to list expression image ids for personality filter", zap.Error(exprErr))
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, exprErr
			}
			// OR: files with a direct personality FK  ∪  expression images for
			// this personality (both sets are already personality-scoped).
			predicates := []predicate.FileAttachment{
				entfileattachment.HasPersonalityWith(personality.ID(*filters.PersonalityID)),
			}
			if len(expressionImageIDs) > 0 {
				predicates = append(predicates, entfileattachment.IDIn(expressionImageIDs...))
			}
			query = query.Where(entfileattachment.Or(predicates...))
		}
	}
	if filters.GlobalOnly != nil && *filters.GlobalOnly {
		expressionImageIDs, exprErr := tx.PersonalityExpression.Query().
			Where(personalityexpression.HasPersonalityWith(personality.HasUserWith(user.ID(userID)))).
			QueryImage().
			IDs(ctx)
		if exprErr != nil {
			d.logger.Error("failed to list expression image ids for global filter", zap.Error(exprErr))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, exprErr
		}
		predicates := []predicate.FileAttachment{
			entfileattachment.Not(entfileattachment.HasPersonality()),
		}
		if len(expressionImageIDs) > 0 {
			predicates = append(predicates, entfileattachment.Not(entfileattachment.IDIn(expressionImageIDs...)))
		}
		query = query.Where(entfileattachment.And(predicates...))
	}

	if filters.MinDate != nil {
		query = query.Where(entfileattachment.CreatedAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(entfileattachment.CreatedAtLTE(*filters.MaxDate))
	}

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "file attachments"), zap.Error(err))
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
		Order(ent.Desc(entfileattachment.FieldCreatedAt))

	// Execute query with selected fields (excluding file_content for performance)
	entFileAttachments, err := query.Select(
		entfileattachment.FieldID,
		entfileattachment.FieldName,
		entfileattachment.FieldFileType,
		entfileattachment.FieldDescription,
		entfileattachment.FieldFileID,
		entfileattachment.FieldS3Key,
		entfileattachment.FieldCreatedAt,
	).All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "file attachments"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	fileAttachmentModels := make([]any, len(entFileAttachments))
	modelsByID := make(map[uuid.UUID]*models.FileAttachment, len(entFileAttachments))
	for i, entFileAttachment := range entFileAttachments {
		model := toFileAttachmentModel(entFileAttachment)
		fileAttachmentModels[i] = model
		modelsByID[model.ID] = model
	}
	if err := d.populateAttachmentPersonalities(ctx, tx, userID, entFileAttachments, modelsByID); err != nil {
		d.logger.Error("failed to populate file attachment personalities", zap.Error(err))
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

	return &models.PaginatedResponse{
		Results:    fileAttachmentModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

func (d *Datastore) populateAttachmentPersonalities(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
	entAttachments []*ent.FileAttachment,
	modelsByID map[uuid.UUID]*models.FileAttachment,
) error {
	if len(entAttachments) == 0 || len(modelsByID) == 0 {
		return nil
	}

	perAttachment := make(map[uuid.UUID]map[uuid.UUID]models.FileAttachmentPersonalityRef, len(entAttachments))
	attachmentIDs := make([]uuid.UUID, 0, len(entAttachments))
	for _, attachment := range entAttachments {
		attachmentIDs = append(attachmentIDs, attachment.ID)
		perAttachment[attachment.ID] = map[uuid.UUID]models.FileAttachmentPersonalityRef{}
		if attachment.Edges.Personality != nil {
			perAttachment[attachment.ID][attachment.Edges.Personality.ID] = models.FileAttachmentPersonalityRef{
				ID:   attachment.Edges.Personality.ID,
				Name: attachment.Edges.Personality.Name,
			}
		}
	}

	expressions, err := tx.PersonalityExpression.Query().
		Where(
			personalityexpression.HasImageWith(entfileattachment.IDIn(attachmentIDs...)),
			personalityexpression.HasPersonalityWith(personality.HasUserWith(user.ID(userID))),
		).
		WithImage(func(q *ent.FileAttachmentQuery) {
			q.Select(entfileattachment.FieldID)
		}).
		WithPersonality(func(q *ent.PersonalityQuery) {
			q.Select(personality.FieldID, personality.FieldName)
		}).
		All(ctx)
	if err != nil {
		return err
	}

	for _, expression := range expressions {
		if expression.Edges.Image == nil || expression.Edges.Personality == nil {
			continue
		}
		attachmentMap, ok := perAttachment[expression.Edges.Image.ID]
		if !ok {
			continue
		}
		attachmentMap[expression.Edges.Personality.ID] = models.FileAttachmentPersonalityRef{
			ID:   expression.Edges.Personality.ID,
			Name: expression.Edges.Personality.Name,
		}
	}

	for attachmentID, personalitiesByID := range perAttachment {
		model, ok := modelsByID[attachmentID]
		if !ok {
			continue
		}
		personalities := make([]models.FileAttachmentPersonalityRef, 0, len(personalitiesByID))
		for _, ref := range personalitiesByID {
			personalities = append(personalities, ref)
		}
		sort.Slice(personalities, func(i, j int) bool {
			if personalities[i].Name == personalities[j].Name {
				return personalities[i].ID.String() < personalities[j].ID.String()
			}
			return personalities[i].Name < personalities[j].Name
		})
		model.Personalities = personalities
		if model.PersonalityID == nil && len(personalities) > 0 {
			fallbackID := personalities[0].ID
			model.PersonalityID = &fallbackID
		}
	}

	return nil
}

// UpdateFileAttachment updates a file attachment by ID
func (d *Datastore) UpdateFileAttachment(ctx context.Context, userID, id uuid.UUID, fileAttachment models.FileAttachment) (*models.FileAttachment, error) {
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

	// Check if file attachment exists and belongs to the user
	exists, err := tx.FileAttachment.Query().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "file attachment"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("file_attachment.not_found_or_unauthorized", "FileAttachmentID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrFileAttachmentNotFound
	}

	// Update file attachment
	update := tx.FileAttachment.UpdateOneID(id)

	if fileAttachment.FileID == nil && fileAttachment.ChatMessageID == nil {
		d.logger.Error(i18n.T2("file_attachment.update_invalid", "FileAttachmentID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrInvalidRequestBody
	}

	if fileAttachment.FileID != nil {
		update.SetFileID(*fileAttachment.FileID)
	}

	if fileAttachment.ChatMessageID != nil {
		update.SetChatMessageID(*fileAttachment.ChatMessageID)
	}

	_, err = update.Save(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "file attachment"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	updatedFileAttachment, err := tx.FileAttachment.Query().
		Where(
			entfileattachment.ID(id),
		).
		WithOwner().
		WithChatMessage().
		WithPersonality().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("file_attachment.get_updated_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	model := toFileAttachmentModel(updatedFileAttachment)
	if err := d.populateAttachmentPersonalities(ctx, tx, userID, []*ent.FileAttachment{updatedFileAttachment}, map[uuid.UUID]*models.FileAttachment{
		model.ID: model,
	}); err != nil {
		d.logger.Error("failed to populate file attachment personalities", zap.Error(err))
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

	return model, nil
}

// DeleteFileAttachment deletes a file attachment by ID
func (d *Datastore) DeleteFileAttachment(ctx context.Context, userID, id uuid.UUID) error {
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

	// Check if file attachment exists and belongs to the user
	exists, err := tx.FileAttachment.Query().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "file attachment"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		d.logger.Error(i18n.T2("file_attachment.not_found_or_unauthorized", "FileAttachmentID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrFileAttachmentNotFound
	}

	// Delete file attachment
	err = tx.FileAttachment.DeleteOneID(id).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "file attachment"), zap.Error(err))
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

// SetFileAttachmentS3Key sets the s3_key on an existing file attachment record.
// This is a fire-and-forget helper called after a successful S3 upload.
func (d *Datastore) SetFileAttachmentS3Key(ctx context.Context, userID, id uuid.UUID, s3Key string) error {
	updated, err := d.dbClient.FileAttachment.Update().
		Where(
			entfileattachment.ID(id),
			entfileattachment.HasOwnerWith(user.ID(userID)),
		).
		SetS3Key(s3Key).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		return ErrFileAttachmentNotFound
	}
	return nil
}

// UpdateFileAttachmentName renames the display name of a file attachment. The S3 key
// is left unchanged so existing objects remain retrievable.
func (d *Datastore) UpdateFileAttachmentName(ctx context.Context, userID, id uuid.UUID, name string) (*models.FileAttachment, error) {
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

	exists, err := tx.FileAttachment.Query().
		Where(entfileattachment.ID(id), entfileattachment.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if !exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrFileAttachmentNotFound
	}

	updated, err := tx.FileAttachment.UpdateOneID(id).SetName(name).Save(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	result, err := tx.FileAttachment.Query().
		Where(entfileattachment.ID(updated.ID)).
		WithOwner().
		WithChatMessage(func(q *ent.ChatMessageQuery) { q.WithChat() }).
		Only(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toFileAttachmentModel(result), nil
}

// CreateFileAttachmentReference creates a lightweight file_attachment row that
// references the same S3 object as srcID. Used when attaching a gallery image
// to a new chat without copying the S3 object.
func (d *Datastore) CreateFileAttachmentReference(ctx context.Context, userID, srcID uuid.UUID) (*models.FileAttachment, error) {
	src, err := d.GetFileAttachment(ctx, userID, srcID)
	if err != nil {
		return nil, err
	}

	ref := models.FileAttachment{
		UserID:   userID,
		Name:     src.Name,
		FileType: src.FileType,
		FileID:   src.FileID,
		S3Key:    src.S3Key,
	}
	// Derive a fallback key for legacy records that predate the s3_key column.
	// Prefer original chat path when available, otherwise fall back to image path.
	// Note: this only affects the new reference row; source attachment remains unchanged.
	if ref.S3Key == "" {
		if src.ChatID != nil && *src.ChatID != uuid.Nil {
			ref.S3Key = storage.FileKeyForChat(userID, *src.ChatID, src.ID, src.Name)
		} else {
			ref.S3Key = storage.FileKeyForImage(userID, src.ID, src.Name)
		}
		d.logger.Warn("file attachment reference: synthesized legacy s3_key",
			zap.String("source_attachment_id", src.ID.String()),
			zap.String("synthesized_s3_key", ref.S3Key))
	}

	return d.CreateFileAttachment(ctx, userID, ref)
}

// ChatHasSearchableFiles reports whether a chat has files successfully indexed in pgvector.
func (d *Datastore) ChatHasSearchableFiles(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
	exists, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasOwnerWith(user.ID(userID)),
			entfileattachment.HasChatMessageWith(
				entchatmessage.HasChatWith(chat.ID(chatID)),
			),
			entfileattachment.ChunkStatusEQ("chunked"),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T2("file_attachment.searchable_chat_check_failed", "ChatID", chatID.String(), "UserID", userID.String()), zap.Error(err))
		return false, err
	}
	return exists, nil
}

// PersonalityHasSearchableFiles reports whether a personality has files successfully indexed in pgvector.
func (d *Datastore) PersonalityHasSearchableFiles(ctx context.Context, personalityID uuid.UUID) (bool, error) {
	exists, err := d.dbClient.FileAttachment.Query().
		Where(
			entfileattachment.HasPersonalityWith(personality.ID(personalityID)),
			entfileattachment.ChunkStatusEQ("chunked"),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("file_attachment.searchable_personality_check_failed", "PersonalityID", personalityID.String()), zap.Error(err))
		return false, err
	}
	return exists, nil
}
