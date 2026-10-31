package datastore

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"entgo.io/ent/dialect/sql"

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
		Folder:      e.Folder,
		ContextOnly: e.ContextOnly,
		CreatedAt:   e.CreatedAt,

		Origin:                 models.FileOrigin(e.Origin),
		SkillID:                e.SkillID,
		CreatedByPersonalityID: e.CreatedByPersonalityID,
		CreatedByChatID:        e.CreatedByChatID,
		UpdatedAt:              e.UpdatedAt,
	}

	if e.FileID != "" {
		fileAttachmentModel.FileID = &e.FileID
	}
	if e.ChunkStatus != nil {
		fileAttachmentModel.ChunkStatus = string(*e.ChunkStatus)
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
	fileAttachmentModel.Source = classifyFileAttachmentSource(e)

	return &fileAttachmentModel
}

// generatedUnlinkedImageName matches the fixed names used by generation
// pipelines that persist images without a chat message: expression grid cells
// (expression_grid_generation.go) and personality portraits
// (personality_portrait_generation.go).
var generatedUnlinkedImageName = regexp.MustCompile(`(?i)^(expression-[a-z0-9_-]+|personality-portrait)\.png$`)

// classifyFileAttachmentSource derives the gallery Generated/Imported class.
// A linked chat message is decisive: assistant-origin messages carry tool and
// ritual output, user-origin messages carry the user's own uploads. Unlinked
// rows are generated only when they carry a generation pipeline's fixed name;
// everything else (gallery imports, personality uploads) is imported. Returns
// "" when the chat message edge was not eager-loaded, since the origin is then
// unknown.
func classifyFileAttachmentSource(e *ent.FileAttachment) models.FileAttachmentSource {
	msg, err := e.Edges.ChatMessageOrErr()
	if ent.IsNotLoaded(err) {
		return ""
	}
	if msg != nil {
		if msg.Origin == entchatmessage.OriginAssistant {
			return models.FileAttachmentSourceGenerated
		}
		return models.FileAttachmentSourceImported
	}
	if e.Origin == entfileattachment.OriginAgent {
		return models.FileAttachmentSourceGenerated
	}
	if strings.HasPrefix(strings.ToLower(e.FileType), models.ImageMIMEPrefix) &&
		generatedUnlinkedImageName.MatchString(strings.TrimSpace(e.Name)) {
		return models.FileAttachmentSourceGenerated
	}
	return models.FileAttachmentSourceImported
}

// excludeReferenceCopies keeps only the earliest row per (owner, s3_key).
// CreateFileAttachmentReference clones s3_key onto a new row each time a
// gallery image is reused in a chat; without this the gallery lists those
// clones (whose chat/personality links differ from the original, so they
// classify differently) and per-page dedupe cannot see across page boundaries.
// Rows with an empty/NULL s3_key never match and are always kept.
//
// Context-only rows (CreateContextReference) are reference copies by definition and are always
// left out, even once the original is deleted and they are the earliest row for their key.
func excludeReferenceCopies() predicate.FileAttachment {
	notACopy := func(s *sql.Selector) {
		s.Where(sql.P(func(b *sql.Builder) {
			b.WriteString("NOT EXISTS (SELECT 1 FROM ")
			b.WriteString(entfileattachment.Table)
			b.WriteString(" AS fa_orig WHERE fa_orig.")
			b.WriteString(entfileattachment.OwnerColumn)
			b.WriteString(" = ")
			b.WriteString(s.C(entfileattachment.OwnerColumn))
			b.WriteString(" AND fa_orig.")
			b.WriteString(entfileattachment.FieldS3Key)
			b.WriteString(" = ")
			b.WriteString(s.C(entfileattachment.FieldS3Key))
			b.WriteString(" AND ")
			b.WriteString(s.C(entfileattachment.FieldS3Key))
			b.WriteString(" <> '' AND (fa_orig.")
			b.WriteString(entfileattachment.FieldCreatedAt)
			b.WriteString(" < ")
			b.WriteString(s.C(entfileattachment.FieldCreatedAt))
			b.WriteString(" OR (fa_orig.")
			b.WriteString(entfileattachment.FieldCreatedAt)
			b.WriteString(" = ")
			b.WriteString(s.C(entfileattachment.FieldCreatedAt))
			b.WriteString(" AND fa_orig.")
			b.WriteString(entfileattachment.FieldID)
			b.WriteString(" < ")
			b.WriteString(s.C(entfileattachment.FieldID))
			b.WriteString(")))")
		}))
	}
	return entfileattachment.And(entfileattachment.ContextOnly(false), notACopy)
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

	if fileAttachment.Folder != "" {
		folder, ferr := models.NormalizeFolder(fileAttachment.Folder)
		if ferr != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ferr
		}
		create.SetFolder(folder)
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

	if fileAttachment.ContextOnly {
		create.SetContextOnly(true)
	}

	if fileAttachment.Origin != "" {
		create.SetOrigin(entfileattachment.Origin(fileAttachment.Origin))
	}
	if fileAttachment.IsSkillFile() {
		create.SetNillableSkillID(fileAttachment.SkillID)
	}
	if fileAttachment.AgentAuthored() {
		create.SetNillableCreatedByPersonalityID(fileAttachment.CreatedByPersonalityID).
			SetNillableCreatedByChatID(fileAttachment.CreatedByChatID)
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
	if kind := galleryKindPredicate(filters.Kind); kind != nil {
		query = query.Where(kind)
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

	// A context-only row is a file already listed in its own right, shown again on a reply that
	// fetched it; it loads with that reply, never as a file of its own.
	query = query.Where(entfileattachment.ContextOnly(false))

	if filters.Folder != nil {
		query = query.Where(entfileattachment.FolderEQ(*filters.Folder))
	}
	if filters.FolderPrefix != nil && *filters.FolderPrefix != "" {
		// The folder itself or anything beneath it; the trailing slash keeps "charts" from
		// matching "charts-old".
		query = query.Where(entfileattachment.Or(
			entfileattachment.FolderEQ(*filters.FolderPrefix),
			entfileattachment.FolderHasPrefix(*filters.FolderPrefix+"/"),
		))
	}

	if filters.Origin != "" {
		query = query.Where(entfileattachment.OriginEQ(entfileattachment.Origin(filters.Origin)))
	} else if !listingUnderSkillsFolder(filters) {
		// Skill bundle files live under the reserved skills folder and are managed by the skill
		// editor; a gallery listing leaves them out unless it looks inside that folder.
		query = query.Where(entfileattachment.OriginNEQ(entfileattachment.OriginSkill))
	}

	if filters.ExcludeReferenceCopies {
		query = query.Where(excludeReferenceCopies())
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
		entfileattachment.FieldFolder,
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
	if ref.S3Key == "" {
		ref.S3Key = d.legacyReferenceKey(userID, src)
	}

	return d.CreateFileAttachment(ctx, userID, ref)
}

// legacyReferenceKey derives the object key of a legacy record that predates the s3_key column,
// for a reference row to share. It prefers the original chat path when available, otherwise the
// image path. Only the new reference row gets it; the source attachment remains unchanged.
func (d *Datastore) legacyReferenceKey(userID uuid.UUID, src *models.FileAttachment) string {
	var key string
	if src.ChatID != nil && *src.ChatID != uuid.Nil {
		key = storage.FileKeyForChat(userID, *src.ChatID, src.ID, src.Name)
	} else {
		key = storage.FileKeyForImage(userID, src.ID, src.Name)
	}
	d.logger.Warn("file attachment reference: synthesized legacy s3_key",
		zap.String("source_attachment_id", src.ID.String()),
		zap.String("synthesized_s3_key", key))
	return key
}

// CreateContextReference saves the user's existing file srcID onto the assistant reply
// chatMessageID as a context-only reference row (see models.FileAttachment.ContextOnly): it
// shares srcID's stored object, so the reply shows the file the agent fetched without uploading
// a copy, and it never appears in the gallery or the Discord relay.
func (d *Datastore) CreateContextReference(ctx context.Context, userID, srcID, chatMessageID uuid.UUID) (*models.FileAttachment, error) {
	src, err := d.GetFileAttachment(ctx, userID, srcID)
	if err != nil {
		return nil, err
	}
	key := src.S3Key
	if key == "" {
		key = d.legacyReferenceKey(userID, src)
	}
	return d.CreateFileAttachment(ctx, userID, models.FileAttachment{
		UserID:        userID,
		Name:          src.Name,
		FileType:      src.FileType,
		S3Key:         key,
		ChatMessageID: &chatMessageID,
		ContextOnly:   true,
	})
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

// listingUnderSkillsFolder reports whether a file listing is scoped to the reserved skills folder,
// in which case the skill bundle files there are what the caller wants.
func listingUnderSkillsFolder(filters models.FileAttachmentFilters) bool {
	if filters.Folder != nil && models.IsReservedFolder(*filters.Folder) {
		return true
	}
	return filters.FolderPrefix != nil && models.IsReservedFolder(*filters.FolderPrefix)
}
