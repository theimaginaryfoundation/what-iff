package datastore

import (
	"context"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	entmood "github.com/theimaginaryfoundation/what-iff/ent/mood"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const maxFavoriteChatsPerUser = 10

// Convert from Ent Chat to model
func toChatModel(e *ent.Chat) *models.Chat {
	if e == nil {
		return nil
	}

	userID := uuid.Nil
	if e.Edges.Owner != nil {
		userID = e.Edges.Owner.ID
	}

	fav := e.IsFavorite
	arch := e.Archived
	chatModel := models.Chat{
		ID:              e.ID,
		UserID:          userID,
		Name:            e.Name,
		LastMessageTime: e.LastMessageTime,
		IsAutoMood:      e.IsAutoMood,
		Archived:        &arch,
		IsFavorite:      &fav,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
	if e.Source != "" {
		s := e.Source
		chatModel.Source = &s
	}
	if e.ImportHash != "" {
		h := e.ImportHash
		chatModel.ImportHash = &h
	}
	chatModel.RehydrationState = e.RehydrationState

	if e.ResponseID != "" {
		chatModel.ResponseID = &e.ResponseID
	}

	chatModel.CheckpointSummary = e.CheckpointSummary
	for _, summaryMemory := range e.Edges.Memories {
		if summaryMemory.Scope == entmemory.ScopeSummary {
			chatModel.CheckpointSummary = summaryMemory.Content
			break
		}
	}
	chatModel.CheckpointUserMessageCount = e.CheckpointUserMessageCount
	chatModel.LastCheckpointAt = e.LastCheckpointAt

	if e.Edges.Model != nil {
		chatModel.ModelID = e.Edges.Model.ID
		chatModel.ModelName = e.Edges.Model.Name
		chatModel.ToolsEnabled = e.Edges.Model.ToolSupport
	}

	chatModel.PersonalityExpressionsEnabled = true
	if e.Edges.Personality != nil {
		chatModel.PersonalityID = e.Edges.Personality.ID
		chatModel.PersonalityName = e.Edges.Personality.Name
		chatModel.SystemPrompt = e.Edges.Personality.SystemPrompt
		chatModel.Scratchpad = e.Edges.Personality.Scratchpad
		chatModel.PersonalityExpressionsEnabled = e.Edges.Personality.ExpressionsEnabled
	}

	if len(e.DisabledTools) > 0 {
		chatModel.DisabledTools = e.DisabledTools
	}
	if len(e.Tags) > 0 {
		chatModel.Tags = e.Tags
	}

	if e.Edges.ActiveMood != nil {
		id := e.Edges.ActiveMood.ID
		chatModel.ActiveMoodID = &id
	}

	return &chatModel
}

// CreateChat persists a new chat to the datastore
func (d *Datastore) CreateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
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

	// Get user preferences
	userPreferences, err := tx.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		WithModel().
		WithPersonality().
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "user preferences"), zap.Error(err))
		return nil, err
	}

	if chat.ModelID == uuid.Nil {
		if userPreferences.Edges.Model != nil {
			chat.ModelID = userPreferences.Edges.Model.ID
		}
	} else {
		// Validate the explicitly-supplied model exists and is active.
		modelExists, modelErr := tx.Model.Query().
			Where(entmodel.ID(chat.ModelID), entmodel.Deleted(false)).
			Exist(ctx)
		if modelErr != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(modelErr))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, modelErr
		}
		if !modelExists {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrModelNotFound
		}
		if err := d.assertUserCanUseModel(ctx, tx, userID, chat.ModelID); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
	}

	if chat.PersonalityID == uuid.Nil {
		if userPreferences.Edges.Personality != nil {
			chat.PersonalityID = userPreferences.Edges.Personality.ID
		}
	} else {
		// Validate the user owns the explicitly-supplied personality.
		ownsPersonality, ownErr := tx.Personality.Query().
			Where(personality.ID(chat.PersonalityID), personality.HasUserWith(user.ID(userID))).
			Exist(ctx)
		if ownErr != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(ownErr))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ownErr
		}
		if !ownsPersonality {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrPersonalityNotFound
		}
	}

	normalizedTags, err := modeltypes.NormalizeAndValidateChatTags(chat.Tags)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	// Best-effort UI guard for favorite volume.
	// This count check is intentionally non-locking and not a strict invariant.
	creatingFavorite := chat.IsFavorite != nil && *chat.IsFavorite
	if creatingFavorite {
		favoriteCount, countErr := tx.Chat.Query().
			Where(entchat.HasOwnerWith(user.ID(userID)), entchat.IsFavorite(true), entchat.Archived(false)).
			Count(ctx)
		if countErr != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, countErr
		}
		if favoriteCount >= maxFavoriteChatsPerUser {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrFavoriteLimitExceeded
		}
	}

	// Track whether this is the user's first chat in the same transaction to keep
	// the decision atomic with insertion.
	existingChatCount, err := tx.Chat.Query().
		Where(entchat.HasOwnerWith(user.ID(userID))).
		Count(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	isFirstChat := existingChatCount == 0

	// Create chat.
	// Use SetTags (replace semantics) rather than Ent AppendTags to keep validation
	// consistent with schema + shared normalizer across all write paths.
	create := tx.Chat.Create().
		SetName(chat.Name).
		SetOwnerID(userID).
		SetModelID(chat.ModelID).
		SetTags(normalizedTags).
		SetNillableIsFavorite(chat.IsFavorite).
		SetIsAutoMood(true)

	if chat.LastMessageTime != nil {
		create.SetLastMessageTime(*chat.LastMessageTime)
	}

	if chat.PersonalityID != uuid.Nil {
		create.SetPersonalityID(chat.PersonalityID)
	}

	entChat, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "chat"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load owner relationship needed for model conversion
	entChat, err = tx.Chat.Query().
		Where(entchat.ID(entChat.ID)).
		WithOwner().
		WithModel().
		WithPersonality().
		WithActiveMood().
		WithMemories(func(q *ent.MemoryQuery) {
			q.Where(entmemory.ScopeEQ(entmemory.ScopeSummary)).
				Order(ent.Asc(entmemory.FieldCreatedAt), ent.Asc(entmemory.FieldID)).
				Limit(1)
		}).
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("chat.owner_load_failed"), zap.Error(err))
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

	chatModel := toChatModel(entChat)
	if chatModel != nil {
		chatModel.IsFirstChat = isFirstChat
	}
	return chatModel, nil
}

// UpdateChatCheckpointState updates the stored checkpoint summary and checkpoint counter for a chat.
// This is intentionally separate from UpdateChat to avoid accidental overwrites of checkpoint state.
func (d *Datastore) UpdateChatCheckpointState(ctx context.Context, userID, chatID uuid.UUID, checkpointSummary string, checkpointUserMessageCount int) error {
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

	// Authorization: chat must belong to user.
	exists, err := tx.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		tx.Rollback()
		return err
	}
	if !exists {
		tx.Rollback()
		return ErrChatNotFound
	}

	_, err = tx.Chat.UpdateOneID(chatID).
		SetCheckpointSummary(checkpointSummary).
		SetCheckpointUserMessageCount(checkpointUserMessageCount).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("chat.checkpoint.update_failed"), zap.Error(err))
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// ClearChatResponseID clears the stored OpenAI response ID so the next user message starts a new Responses thread.
func (d *Datastore) ClearChatResponseID(ctx context.Context, userID, chatID uuid.UUID) error {
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

	exists, err := tx.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		tx.Rollback()
		return err
	}
	if !exists {
		tx.Rollback()
		return ErrChatNotFound
	}

	_, err = tx.Chat.UpdateOneID(chatID).
		SetResponseID("").
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("chat.response_id.clear_failed"), zap.Error(err))
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// UpdateChatCheckpointStateAndClearResponseID atomically updates the stored checkpoint state and clears the
// stored OpenAI response ID so the next user message starts a new Responses thread.
//
// It writes both CheckpointUserMessageCount (used by the checkpoint decision policy to determine *when*
// to checkpoint) and LastCheckpointAt (used as a DB range cursor to scope history fetches to the current
// checkpoint period). The two fields always move together here — CheckpointUserMessageCount drives the
// "when to fire" logic while LastCheckpointAt is the actual time-based filter applied to ListChatMessages
// queries. A count alone can't serve as a DB predicate without an extra round-trip to get the current
// total, so both are kept.
//
// This combines UpdateChatCheckpointState and ClearChatResponseID into a single transaction to avoid
// transient inconsistent state and reduce database round trips.
func (d *Datastore) UpdateChatCheckpointStateAndClearResponseID(ctx context.Context, userID, chatID uuid.UUID, checkpointSummary string, checkpointUserMessageCount int) error {
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

	updated, err := tx.Chat.Update().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		SetCheckpointSummary(checkpointSummary).
		SetCheckpointUserMessageCount(checkpointUserMessageCount).
		SetResponseID("").
		SetLastCheckpointAt(time.Now()).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("chat.checkpoint_response_id.update_failed"), zap.Error(err))
		tx.Rollback()
		return err
	}
	if updated == 0 {
		tx.Rollback()
		return ErrChatNotFound
	}

	return tx.Commit()
}

// GetChat retrieves a chat from the datastore by ID
func (d *Datastore) GetChat(ctx context.Context, userID, id uuid.UUID) (*models.Chat, error) {
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

	// Query chat with authorization check
	entChat, err := tx.Chat.Query().
		Where(
			entchat.ID(id),
			entchat.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithModel().
		WithPersonality().
		WithActiveMood().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("chat.not_found_or_unauthorized", "ChatID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrChatNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Lazy personality migration: if this chat has no personality but the user has a
	// default personality in their preferences, assign it now and persist it so that
	// all subsequent reads (agent processing, PATCH load, etc.) see the correct value.
	if entChat.Edges.Personality == nil {
		userPrefs, prefsErr := tx.UserPreference.Query().
			Where(userpreference.HasUserWith(user.ID(userID))).
			WithPersonality().
			Only(ctx)
		if prefsErr == nil && userPrefs.Edges.Personality != nil {
			_, updateErr := tx.Chat.UpdateOneID(id).
				SetPersonalityID(userPrefs.Edges.Personality.ID).
				Save(ctx)
			if updateErr != nil {
				d.logger.Warn(i18n.T2("chat.personality.auto_assign_failed", "ChatID", id.String(), "UserID", userID.String()), zap.Error(updateErr))
			} else {
				// Re-load so the returned model carries the personality edges.
				reloaded, reloadErr := tx.Chat.Query().
					Where(entchat.ID(id)).
					WithOwner().
					WithModel().
					WithPersonality().
					WithActiveMood().
					WithMemories(func(q *ent.MemoryQuery) {
						q.Where(entmemory.ScopeEQ(entmemory.ScopeSummary)).
							Order(ent.Asc(entmemory.FieldCreatedAt), ent.Asc(entmemory.FieldID)).
							Limit(1)
					}).
					Only(ctx)
				if reloadErr != nil {
					d.logger.Warn(i18n.T1("chat.personality.reload_failed", "ChatID", id.String()), zap.Error(reloadErr))
				} else {
					entChat = reloaded
				}
			}
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toChatModel(entChat), nil
}

// GetChatContext returns the current scratchpad/summary for a chat.
func (d *Datastore) GetChatContext(ctx context.Context, userID, chatID uuid.UUID) (*models.ChatContext, error) {
	chat, err := d.GetChat(ctx, userID, chatID)
	if err != nil {
		return nil, err
	}

	return &models.ChatContext{
		ChatID:           chat.ID,
		ActiveScratchpad: chat.Scratchpad,
		Summary:          chat.CheckpointSummary,
	}, nil
}

// ListChats returns a paginated list of chats for a user that match optional filter criteria
func (d *Datastore) ListChats(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.ChatFilters) (*models.PaginatedResponse, error) {
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

	// Build query with user authorization (avoid eager-load edges before Count()).
	query := tx.Chat.Query().
		Where(
			entchat.HasOwnerWith(
				user.ID(userID),
			),
		)

	// Apply filters if provided
	if filters.Name != nil && *filters.Name != "" {
		query = query.Where(entchat.NameContainsFold(*filters.Name))
	}

	if filters.Query != nil && *filters.Query != "" {
		queryValue := strings.ToLower(strings.TrimSpace(*filters.Query))
		if queryValue != "" {
			likeQuery := "%" + queryValue + "%"
			// tags are stored as JSON (not a native PG array); use jsonb_array_elements_text.
			// Use sql.P + b.Arg so Postgres gets $n placeholders (ExprP leaves literal "?" in the string).
			query = query.Where(entchat.Or(
				entchat.NameContainsFold(queryValue),
				entchat.CheckpointSummaryContainsFold(queryValue),
				func(selector *sql.Selector) {
					selector.Where(sql.P(func(b *sql.Builder) {
						b.WriteString("EXISTS (SELECT 1 FROM jsonb_array_elements_text(COALESCE(")
						b.WriteString(selector.C(entchat.FieldTags))
						b.WriteString("::jsonb, '[]'::jsonb)) AS _chat_tags(tag_value) WHERE lower(_chat_tags.tag_value) LIKE ")
						b.Arg(likeQuery)
						b.WriteString(")")
					}))
				},
			))
		}
	}

	if filters.Tag != nil && *filters.Tag != "" {
		exactTag := strings.ToLower(strings.TrimSpace(*filters.Tag))
		if exactTag != "" {
			query = query.Where(func(selector *sql.Selector) {
				selector.Where(sql.P(func(b *sql.Builder) {
					b.WriteString("EXISTS (SELECT 1 FROM jsonb_array_elements_text(COALESCE(")
					b.WriteString(selector.C(entchat.FieldTags))
					b.WriteString("::jsonb, '[]'::jsonb)) AS _chat_tags(tag_value) WHERE lower(_chat_tags.tag_value) = ")
					b.Arg(exactTag)
					b.WriteString(")")
				}))
			})
		}
	}

	if filters.PersonalityID != nil {
		query = query.Where(entchat.HasPersonalityWith(personality.ID(*filters.PersonalityID)))
	}

	if filters.IsFavorite != nil {
		query = query.Where(entchat.IsFavorite(*filters.IsFavorite))
	}

	if filters.MinDate != nil {
		query = query.Where(entchat.CreatedAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(entchat.CreatedAtLTE(*filters.MaxDate))
	}

	// Explicit IDs deliberately bypass archive filtering; callers already selected
	// concrete user-owned threads.
	if len(filters.IDs) > 0 {
		query = query.Where(entchat.IDIn(filters.IDs...))
	} else if filters.IncludeArchived {
		// Explicit callers (agent discovery) may safely span both active and
		// archived threads. HTTP lists retain their archived=false default.
	} else if filters.Archived != nil && *filters.Archived {
		query = query.Where(entchat.Archived(true))
	} else {
		query = query.Where(entchat.Archived(false))
	}

	if filters.Source != nil && *filters.Source != "" {
		query = query.Where(entchat.Source(*filters.Source))
	}

	if filters.HasMessages != nil && *filters.HasMessages {
		// Empty shells never get last_message_time; NULLs also float to the top of DESC sorts.
		query = query.Where(entchat.LastMessageTimeNotNil())
	}

	// Get total count
	totalCount, err := query.Clone().Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "chats"), zap.Error(err))
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
		WithOwner().
		WithModel().
		WithPersonality().
		WithActiveMood().
		Offset(offset).
		Limit(pageSize).
		Order(ent.Desc(entchat.FieldLastMessageTime), ent.Desc(entchat.FieldCreatedAt))

	// Execute query
	entChats, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "chats"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	unreadCountByChatID := make(map[uuid.UUID]int, len(entChats))
	if len(entChats) > 0 {
		chatIDs := make([]uuid.UUID, len(entChats))
		for i, entChat := range entChats {
			chatIDs[i] = entChat.ID
		}

		var unreadRows []struct {
			ChatID uuid.UUID `json:"chat_messages"`
			Count  int       `json:"count"`
		}

		err = tx.ChatMessage.Query().
			Where(
				chatmessage.HasChatWith(entchat.IDIn(chatIDs...)),
				chatmessage.OriginEQ(chatmessage.Origin(models.MessageOriginAssistant)),
				chatmessage.ReadStatusEQ(chatmessage.ReadStatusUnread),
			).
			GroupBy(chatmessage.ChatColumn).
			Aggregate(ent.Count()).
			Scan(ctx, &unreadRows)
		if err != nil {
			d.logger.Error("failed to query unread chat message counts", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error("failed to rollback transaction", zap.Error(rerr))
			}
			return nil, err
		}

		for _, row := range unreadRows {
			unreadCountByChatID[row.ChatID] = row.Count
		}
	}

	// Convert to model types
	chatModels := make([]any, len(entChats))
	for i, entChat := range entChats {
		chatModel := toChatModel(entChat)
		if chatModel != nil {
			chatModel.UnreadCount = unreadCountByChatID[entChat.ID]
		}
		chatModels[i] = chatModel
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    chatModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// UpdateChat updates an existing chat.
//
// Tags semantics:
//   - chat.Tags == nil  -> preserve existing tags (no-op for tags)
//   - chat.Tags != nil  -> normalize/validate and replace tags
//
// Archived semantics:
//   - chat.Archived == nil -> do not change the archived column (PATCH omit semantics; safe for agent partial updates)
//   - chat.Archived != nil -> set archived to *chat.Archived
func (d *Datastore) UpdateChat(ctx context.Context, userID uuid.UUID, chat models.Chat) (*models.Chat, error) {
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

	// Check if chat exists and belongs to the user
	exists, err := tx.Chat.Query().
		Where(
			entchat.ID(chat.ID),
			entchat.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("chat.not_found_or_unauthorized", "ChatID", chat.ID.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrChatNotFound
	}

	settingFavorite := chat.IsFavorite != nil && *chat.IsFavorite
	if settingFavorite {
		existingChat, existingErr := tx.Chat.Query().
			Where(
				entchat.ID(chat.ID),
				entchat.HasOwnerWith(user.ID(userID)),
			).
			Only(ctx)
		if existingErr != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, existingErr
		}
		// Best-effort UI guard for favorite volume.
		// This count check is intentionally non-locking and not a strict invariant.
		if !existingChat.IsFavorite {
			favoriteCount, countErr := tx.Chat.Query().
				Where(entchat.HasOwnerWith(user.ID(userID)), entchat.IsFavorite(true), entchat.Archived(false)).
				Count(ctx)
			if countErr != nil {
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, countErr
			}
			if favoriteCount >= maxFavoriteChatsPerUser {
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, ErrFavoriteLimitExceeded
			}
		}
	}

	// Update chat.
	// Use SetTags (replace semantics) rather than Ent AppendTags to keep validation
	// consistent with schema + shared normalizer across all write paths.
	update := tx.Chat.UpdateOneID(chat.ID).
		SetName(chat.Name)
	if chat.Tags != nil {
		normalizedTags, err := modeltypes.NormalizeAndValidateChatTags(chat.Tags)
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
		update.SetTags(normalizedTags)
	}
	update.SetNillableIsFavorite(chat.IsFavorite)

	if chat.LastMessageTime != nil {
		update.SetLastMessageTime(*chat.LastMessageTime)
	}

	if chat.ResponseID != nil {
		update.SetResponseID(*chat.ResponseID)
	}

	if chat.ModelID != uuid.Nil {
		modelExists, modelErr := tx.Model.Query().
			Where(entmodel.ID(chat.ModelID), entmodel.Deleted(false)).
			Exist(ctx)
		if modelErr != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "model"), zap.Error(modelErr))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, modelErr
		}
		if !modelExists {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrModelNotFound
		}
		if err := d.assertUserCanUseModel(ctx, tx, userID, chat.ModelID); err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
		update.SetModelID(chat.ModelID)
	}

	if chat.PersonalityID != uuid.Nil {
		update.SetPersonalityID(chat.PersonalityID)
	} else {
		update.ClearPersonality()
	}

	if chat.DisabledTools != nil {
		update.SetDisabledTools(chat.DisabledTools)
	} else {
		update.ClearDisabledTools()
	}

	if chat.ActiveMoodID != nil && *chat.ActiveMoodID != uuid.Nil {
		// Keep mood invariants consistent with SetChatActiveMood:
		// active mood must be owned by user and attached to the chat personality.
		if chat.PersonalityID == uuid.Nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrMoodNotFound
		}
		moodExists, moodErr := tx.Mood.Query().
			Where(
				entmood.ID(*chat.ActiveMoodID),
				entmood.HasOwnerWith(user.ID(userID)),
				entmood.HasPersonalitiesWith(personality.ID(chat.PersonalityID)),
			).
			Exist(ctx)
		if moodErr != nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, moodErr
		}
		if !moodExists {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrMoodNotFound
		}
		update.SetActiveMoodID(*chat.ActiveMoodID)
	} else {
		update.ClearActiveMood()
	}
	update.SetIsAutoMood(chat.IsAutoMood)
	update.SetNillableArchived(chat.Archived)

	entChat, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "chat"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the owner relationship
	entChat, err = tx.Chat.Query().
		Where(entchat.ID(entChat.ID)).
		WithOwner().
		WithModel().
		WithPersonality().
		WithActiveMood().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("chat.owner_load_failed"), zap.Error(err))
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

	return toChatModel(entChat), nil
}

// DeleteChat deletes a chat and all its messages
func (d *Datastore) DeleteChat(ctx context.Context, userID, id uuid.UUID) error {
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

	// Check if chat exists and belongs to the user
	exists, err := tx.Chat.Query().
		Where(
			entchat.ID(id),
			entchat.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		d.logger.Error(i18n.T2("chat.not_found_or_unauthorized", "ChatID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrChatNotFound
	}

	// Delete chat (this will cascade delete messages due to FK constraint)
	err = tx.Chat.DeleteOneID(id).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "chat"), zap.Error(err))
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

// SetChatActiveMood sets (or clears when moodID == uuid.Nil) the active mood for a chat.
// If modelID is non-nil and non-zero, the chat's model is also updated.
func (d *Datastore) SetChatActiveMood(ctx context.Context, userID, chatID uuid.UUID, moodID *uuid.UUID, modelID *uuid.UUID, isAutoMood *bool) error {
	chatRow, err := d.dbClient.Chat.Query().
		Where(
			entchat.ID(chatID),
			entchat.HasOwnerWith(user.ID(userID)),
		).
		WithPersonality().
		Only(ctx)
	if err != nil {
		return err
	}
	if chatRow.Edges.Personality == nil {
		return ErrChatNotFound
	}
	chatPersonalityID := chatRow.Edges.Personality.ID

	if moodID != nil && *moodID != uuid.Nil {
		exists, moodErr := d.dbClient.Mood.Query().
			Where(
				entmood.ID(*moodID),
				entmood.HasOwnerWith(user.ID(userID)),
				entmood.HasPersonalitiesWith(personality.ID(chatPersonalityID)),
			).
			Exist(ctx)
		if moodErr != nil {
			return moodErr
		}
		if !exists {
			return ErrMoodNotFound
		}
	}

	update := d.dbClient.Chat.UpdateOneID(chatID)
	if moodID == nil || *moodID == uuid.Nil {
		update.ClearActiveMood()
	} else {
		update.SetActiveMoodID(*moodID)
	}
	if modelID != nil && *modelID != uuid.Nil {
		update.SetModelID(*modelID)
	}
	if isAutoMood != nil {
		update.SetIsAutoMood(*isAutoMood)
	}
	return update.Exec(ctx)
}
