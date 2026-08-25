package datastore

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessagecontextitem"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/personalityexpression"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

const maxChronologicalMessagePageSize = 32

// Convert from Ent ChatMessage to model
func toChatMessageModel(e *ent.ChatMessage) *models.ChatMessage {
	if e == nil {
		return nil
	}

	chatMessage := models.ChatMessage{
		ID:                    e.ID,
		Message:               e.Message,
		Origin:                models.MessageOrigin(e.Origin),
		ReadStatus:            models.MessageReadStatus(e.ReadStatus),
		GenerationModel:       e.GenerationModel,
		GenerationPersonality: e.GenerationPersonality,
		SentAt:                e.SentAt,
		Tokens:                e.Tokens,
	}

	if e.Edges.GenerationMood != nil {
		moodID := e.Edges.GenerationMood.ID
		chatMessage.GenerationMoodID = &moodID
		if n := strings.TrimSpace(e.Edges.GenerationMood.Name); n != "" {
			chatMessage.GenerationMoodName = n
		}
		if len(e.Edges.GenerationMood.ThumbnailData) > 0 {
			chatMessage.GenerationMoodThumbnail = base64.StdEncoding.EncodeToString(e.Edges.GenerationMood.ThumbnailData)
		}
	}

	if e.ResponseID != "" {
		chatMessage.ResponseID = &e.ResponseID
	}

	if e.Edges.Chat != nil {
		chatMessage.ChatID = e.Edges.Chat.ID
	}

	for _, attachment := range e.Edges.FileAttachments {
		chatMessage.Attachments = append(chatMessage.Attachments, toFileAttachmentModel(attachment))
	}

	for _, ritual := range e.Edges.Rituals {
		ritual.Content = ""
		ritual.Hotkeys = ""
		chatMessage.Rituals = append(chatMessage.Rituals, toRitualModel(ritual))
	}

	for _, toolCall := range e.Edges.ToolCalls {
		toolCall.Edges.ChatMessage = e
		chatMessage.ToolCalls = append(chatMessage.ToolCalls, toToolCallModel(toolCall))
	}

	for _, item := range e.Edges.ContextItems {
		chatMessage.AdditionalContext = append(chatMessage.AdditionalContext, models.AdditionalContextItem{
			Type:     item.Type,
			Content:  item.Content,
			MemoryID: item.MemoryID,
			Scope:    item.Scope,
		})
	}

	if exp := e.Edges.GenerationExpression; exp != nil {
		k := exp.ExpressionKey
		chatMessage.GenerationExpressionKey = &k
		if exp.Label != nil {
			l := strings.TrimSpace(*exp.Label)
			if l != "" {
				chatMessage.GenerationExpressionLabel = &l
			}
		}
		if img := exp.Edges.Image; img != nil {
			id := img.ID
			chatMessage.GenerationExpressionImageID = &id
			u := personalityExpressionImageURL(&img.ID)
			chatMessage.GenerationExpressionImageURL = u
		}
	}
	if e.GenerationExpressionReasoning != nil {
		r := trimGenerationExpressionReasoning(*e.GenerationExpressionReasoning)
		if r != "" {
			chatMessage.GenerationExpressionReasoning = &r
		}
	}

	if e.LastErrorMessage != nil {
		msg := strings.TrimSpace(*e.LastErrorMessage)
		if msg != "" {
			chatMessage.LastErrorMessage = &msg
		}
	}

	if e.CheckpointCompletedAt != nil && !e.CheckpointCompletedAt.IsZero() {
		t := e.CheckpointCompletedAt.UTC()
		chatMessage.CheckpointCompletedAt = &t
	}

	// context_breakdown is a typed jsonb column; ent unmarshals it for us. Only surface
	// snapshots that actually carry segments (defends against an empty/legacy row).
	if e.ContextBreakdown != nil && len(e.ContextBreakdown.Segments) > 0 {
		chatMessage.ContextBreakdown = e.ContextBreakdown
	}

	return &chatMessage
}

// trimGenerationExpressionReasoning normalizes classifier text for API exposure (bounded length).
func trimGenerationExpressionReasoning(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	const maxRunes = 512
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return strings.TrimSpace(string(r[:maxRunes]))
}

func trimChatMessageLastError(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	const maxRunes = 4096
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}

// SetChatMessageLastError sets or clears the user-visible generation error on a chat message (user-owned).
func (d *Datastore) SetChatMessageLastError(ctx context.Context, userID, messageID uuid.UUID, msg *string) error {
	upd := d.dbClient.ChatMessage.Update().
		Where(
			chatmessage.ID(messageID),
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(
					user.ID(userID),
				),
			),
		)
	if msg == nil {
		upd.ClearLastErrorMessage()
	} else {
		t := trimChatMessageLastError(*msg)
		if t == "" {
			upd.ClearLastErrorMessage()
		} else {
			upd.SetLastErrorMessage(t)
		}
	}
	affected, err := upd.Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}

// createContextItemsBulk inserts context items for a chat message within an existing transaction.
// Callers must treat a non-nil error as fatal for the transaction (message row alone is not enough
// to rebuild prefetched context on later turns).
func (d *Datastore) createContextItemsBulk(ctx context.Context, tx *ent.Tx, msgID uuid.UUID, items []models.AdditionalContextItem, operation string) error {
	if len(items) == 0 {
		return nil
	}
	builders := make([]*ent.ChatMessageContextItemCreate, 0, len(items))
	for _, item := range items {
		builders = append(builders, tx.ChatMessageContextItem.Create().
			SetType(item.Type).
			SetContent(item.Content).
			SetNillableMemoryID(item.MemoryID).
			SetScope(item.Scope).
			SetChatMessageID(msgID))
	}
	if _, err := tx.ChatMessageContextItem.CreateBulk(builders...).Save(ctx); err != nil {
		if d.metrics != nil {
			d.metrics.RecordCounter(ctx, telemetry.ChatMessageContextItemsPersistFailures, 1,
				metric.WithAttributes(attribute.String("operation", operation)))
		}
		d.logger.Error("failed to persist chat message context items",
			zap.String("chat_message_id", msgID.String()),
			zap.String("operation", operation),
			zap.Int("item_count", len(items)),
			zap.Error(err),
		)
		return fmt.Errorf("persist chat message context items (%s): %w", operation, err)
	}
	return nil
}

// CreateChatMessage persists a new chat message to the datastore
func (d *Datastore) CreateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error) {
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
	chatExists, err := tx.Chat.Query().
		Where(
			entchat.ID(chatMessage.ChatID),
			entchat.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !chatExists {
		d.logger.Error(i18n.T2("chat.not_found_or_unauthorized", "ChatID", chatMessage.ChatID.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrChatNotFound
	}

	// Create chat message
	create := tx.ChatMessage.Create().
		SetMessage(chatMessage.Message).
		SetOrigin(chatmessage.Origin(chatMessage.Origin)).
		SetChatID(chatMessage.ChatID).
		SetTokens(chatMessage.Tokens)

	if chatMessage.Origin == models.MessageOriginAssistant {
		create.SetReadStatus(chatmessage.ReadStatusUnread)
	} else {
		create.SetReadStatus(chatmessage.ReadStatusRead)
	}

	if chatMessage.ResponseID != nil {
		create.SetResponseID(*chatMessage.ResponseID)
	}
	if chatMessage.GenerationModel != "" {
		create.SetGenerationModel(chatMessage.GenerationModel)
	}
	if chatMessage.GenerationPersonality != "" {
		create.SetGenerationPersonality(chatMessage.GenerationPersonality)
	}
	if chatMessage.GenerationMoodID != nil && *chatMessage.GenerationMoodID != uuid.Nil {
		create.SetGenerationMoodID(*chatMessage.GenerationMoodID)
	}

	if !chatMessage.SentAt.IsZero() {
		create.SetSentAt(chatMessage.SentAt)
	}

	if len(chatMessage.Attachments) > 0 {
		attachmentIDs := make([]uuid.UUID, 0, len(chatMessage.Attachments))
		for _, attachment := range chatMessage.Attachments {
			if attachment == nil || attachment.ID == uuid.Nil {
				continue
			}
			attachmentIDs = append(attachmentIDs, attachment.ID)
		}
		if len(attachmentIDs) > 0 {
			create.AddFileAttachmentIDs(attachmentIDs...)
		}
	}

	if len(chatMessage.Rituals) > 0 {
		ritualIDs := make([]uuid.UUID, len(chatMessage.Rituals))
		for i, ritual := range chatMessage.Rituals {
			ritualIDs[i] = ritual.ID
		}
		create.AddRitualIDs(ritualIDs...)
	}

	entChatMessage, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Update chat's last_message_time
	_, err = tx.Chat.UpdateOneID(chatMessage.ChatID).
		SetLastMessageTime(entChatMessage.SentAt).
		Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T("chat.message.last_message_time_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Create tool calls in bulk if provided
	if len(chatMessage.ToolCalls) > 0 {
		err = d.createToolCallsBulk(ctx, tx, entChatMessage.ID, chatMessage.ToolCalls)
		if err != nil {
			// Log error but don't fail the transaction - we don't want to lose the chat message
			d.logger.Error(i18n.T2("chat.message.tool_calls_create_failed", "ChatMessageID", entChatMessage.ID.String(), "ToolCallCount", len(chatMessage.ToolCalls)), zap.Error(err))
		}
	}

	// Create context items in bulk if provided
	if err := d.createContextItemsBulk(ctx, tx, entChatMessage.ID, chatMessage.AdditionalContext, "create"); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load chat relationships needed for model conversion
	entChatMessage, err = tx.ChatMessage.Query().
		Where(chatmessage.ID(entChatMessage.ID)).
		WithChat().
		WithFileAttachments().
		WithRituals().
		WithToolCalls().
		WithContextItems().
		WithGenerationMood().
		WithGenerationExpression(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		}).
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("chat.message.relationship_load_failed"), zap.Error(err))
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

	return toChatMessageModel(entChatMessage), nil
}

func (d *Datastore) UpdateChatMessage(ctx context.Context, userID uuid.UUID, chatMessage models.ChatMessage) (*models.ChatMessage, error) {
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

	// Check if chat message exists and belongs to the user
	exists, err := tx.ChatMessage.Query().
		Where(
			chatmessage.ID(chatMessage.ID),
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(
					user.ID(userID),
				),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("chat.message.not_found_or_unauthorized", "ChatMessageID", chatMessage.ID.String(), "UserID", userID.String()))
		return nil, ErrChatMessageNotFound
	}

	// Update chat message scalar fields
	update := tx.ChatMessage.UpdateOneID(chatMessage.ID).
		SetTokens(chatMessage.Tokens)

	if chatMessage.ResponseID != nil {
		update.SetResponseID(*chatMessage.ResponseID)
	}

	if _, err := update.Save(ctx); err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Replace context items: delete existing rows then insert the new set.
	if _, err := tx.ChatMessageContextItem.Delete().
		Where(chatmessagecontextitem.HasChatMessageWith(chatmessage.ID(chatMessage.ID))).
		Exec(ctx); err != nil {
		d.logger.Error("failed to clear context items for chat message update",
			zap.String("chat_message_id", chatMessage.ID.String()),
			zap.Error(err),
		)
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if err := d.createContextItemsBulk(ctx, tx, chatMessage.ID, chatMessage.AdditionalContext, "update"); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Reload the full message so callers receive the updated state (same eager-load set as CreateChatMessage / GetChatMessage).
	entChatMessage, err := tx.ChatMessage.Query().
		Where(chatmessage.ID(chatMessage.ID)).
		WithChat().
		WithFileAttachments().
		WithRituals().
		WithToolCalls().
		WithContextItems().
		WithGenerationMood().
		WithGenerationExpression(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		}).
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
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

	return toChatMessageModel(entChatMessage), nil
}

// MarkChatMessagesRead marks all unread assistant messages in a chat as read.
func (d *Datastore) MarkChatMessagesRead(ctx context.Context, userID, chatID uuid.UUID) (int, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return 0, err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	chatExists, err := tx.Chat.Query().
		Where(
			entchat.ID(chatID),
			entchat.HasOwnerWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error("failed to query chat", zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return 0, err
	}
	if !chatExists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return 0, ErrChatNotFound
	}

	updated, err := tx.ChatMessage.Update().
		Where(
			chatmessage.HasChatWith(entchat.ID(chatID)),
			chatmessage.OriginEQ(chatmessage.Origin(models.MessageOriginAssistant)),
			chatmessage.ReadStatusEQ(chatmessage.ReadStatusUnread),
		).
		SetReadStatus(chatmessage.ReadStatusRead).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to mark chat messages read",
			zap.Error(err),
			zap.String("chat_id", chatID.String()),
			zap.String("user_id", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return 0, err
	}

	return updated, nil
}

// GetChatMessage retrieves a chat message from the datastore by ID
func (d *Datastore) GetChatMessage(ctx context.Context, userID, id uuid.UUID) (*models.ChatMessage, error) {
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

	// Query chat message with authorization check
	entChatMessage, err := tx.ChatMessage.Query().
		Where(
			chatmessage.ID(id),
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(
					user.ID(userID),
				),
			),
		).
		WithChat().
		WithFileAttachments().
		WithRituals().
		WithToolCalls().
		WithContextItems().
		WithGenerationMood().
		WithGenerationExpression(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		}).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("chat.message.not_found_or_unauthorized", "ChatMessageID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrChatMessageNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
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

	return toChatMessageModel(entChatMessage), nil
}

// ListChatMessages returns a newest-first, offset-paginated list of chat messages for a specific
// chat. It backs the chat UI, which displays the newest messages first while loading history.
func (d *Datastore) ListChatMessages(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	return d.listChatMessages(ctx, userID, chatID, pageNum, pageSize, time.Time{}, uuid.Nil, false, filters)
}

// ListChatMessagesAfter returns the next chronological page after (afterSentAt, afterID).
// Passing zero values fetches the beginning of the conversation. The (sent_at, id) cursor gives
// imports and live threads a stable ordering even when multiple messages share a timestamp.
func (d *Datastore) ListChatMessagesAfter(ctx context.Context, userID, chatID uuid.UUID, afterSentAt time.Time, afterID uuid.UUID, pageSize int, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
	if afterSentAt.IsZero() != (afterID == uuid.Nil) {
		return nil, fmt.Errorf("chronological message cursor requires both sent time and message ID")
	}
	if pageSize > maxChronologicalMessagePageSize {
		return nil, fmt.Errorf("chronological message page size must not exceed %d", maxChronologicalMessagePageSize)
	}
	return d.listChatMessages(ctx, userID, chatID, 1, pageSize, afterSentAt, afterID, true, filters)
}

// listChatMessages shares authorization, filters, and exact totals for the two message views.
// chronological pages are strictly ascending by (sent_at, id) after their keyset position; the UI
// view is descending and offset-paginated.
func (d *Datastore) listChatMessages(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, afterSentAt time.Time, afterID uuid.UUID, chronological bool, filters models.ChatMessageFilters) (*models.PaginatedResponse, error) {
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
	chatExists, err := tx.Chat.Query().
		Where(
			entchat.ID(chatID),
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

	if !chatExists {
		d.logger.Error(i18n.T2("chat.not_found_or_unauthorized", "ChatID", chatID.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrChatNotFound
	}

	// Build query with authorization
	query := tx.ChatMessage.Query().
		Where(
			chatmessage.HasChatWith(
				entchat.ID(chatID),
			),
		).
		WithChat().
		WithFileAttachments().
		WithRituals().
		WithToolCalls().
		WithContextItems().
		WithGenerationMood().
		WithGenerationExpression(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		})

	// Apply filters if provided
	if filters.Origin != nil {
		query = query.Where(chatmessage.OriginEQ(chatmessage.Origin(*filters.Origin)))
	}

	if filters.Query != nil {
		query = query.Where(chatmessage.MessageContainsFold(*filters.Query))
	}

	if filters.MinDate != nil {
		query = query.Where(chatmessage.SentAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(chatmessage.SentAtLTE(*filters.MaxDate))
	}

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "chat messages"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Apply pagination. Chronological retrieval uses keyset pagination so an insertion before a
	// later offset cannot duplicate or skip messages while an agent walks a thread.
	if pageSize < 1 {
		pageSize = 10
	}

	if chronological {
		if !afterSentAt.IsZero() {
			query = query.Where(chatmessage.Or(
				chatmessage.SentAtGT(afterSentAt),
				chatmessage.And(chatmessage.SentAtEQ(afterSentAt), chatmessage.IDGT(afterID)),
			))
		}
		query = query.
			Limit(pageSize).
			Order(ent.Asc(chatmessage.FieldSentAt), ent.Asc(chatmessage.FieldID))
	} else {
		if pageNum < 1 {
			pageNum = 1
		}
		offset := (pageNum - 1) * pageSize
		query = query.
			Offset(offset).
			Limit(pageSize).
			Order(ent.Desc(chatmessage.FieldSentAt), ent.Desc(chatmessage.FieldID))
	}

	// Execute query
	entChatMessages, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	chatMessageModels := make([]any, len(entChatMessages))
	for i, entChatMessage := range entChatMessages {
		chatMessageModels[i] = toChatMessageModel(entChatMessage)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    chatMessageModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// GetChatMessageCount returns the count of messages in a chat, optionally filtered by origin
func (d *Datastore) GetChatMessageCount(ctx context.Context, userID, chatID uuid.UUID, originFilter models.MessageOriginFilter) (int, error) {
	// Default to ALL if empty
	if originFilter == "" {
		originFilter = models.MessageOriginFilterAll
	}

	// Validate origin filter
	if originFilter != models.MessageOriginFilterAll &&
		originFilter != models.MessageOriginFilterUser &&
		originFilter != models.MessageOriginFilterAssistant {
		d.logger.Error(i18n.T1("chat.message.origin_filter.invalid", "OriginFilter", string(originFilter)))
		return 0, ErrInvalidMessageOriginFilter
	}

	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return 0, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if chat exists and belongs to the user
	chatExists, err := tx.Chat.Query().
		Where(
			entchat.ID(chatID),
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
		return 0, err
	}

	if !chatExists {
		d.logger.Error(i18n.T2("chat.not_found_or_unauthorized", "ChatID", chatID.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return 0, ErrChatNotFound
	}

	// Build query for counting messages
	query := tx.ChatMessage.Query().
		Where(
			chatmessage.HasChatWith(
				entchat.ID(chatID),
			),
		)

	// Apply origin filter if not ALL
	if originFilter == models.MessageOriginFilterUser {
		query = query.Where(chatmessage.OriginEQ(chatmessage.Origin(models.MessageOriginUser)))
	} else if originFilter == models.MessageOriginFilterAssistant {
		query = query.Where(chatmessage.OriginEQ(chatmessage.Origin(models.MessageOriginAssistant)))
	}

	// Get count
	count, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "chat messages"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return 0, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return 0, err
	}

	return count, nil
}

// UpdateChatMessageGenerationExpression sets or clears the personality expression chosen for an assistant message
// and optionally persists classifier reasoning (trimmed; empty string clears).
func (d *Datastore) UpdateChatMessageGenerationExpression(ctx context.Context, userID, messageID uuid.UUID, expressionID *uuid.UUID, reasoning string) (*models.ChatMessage, error) {
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

	cm, err := tx.ChatMessage.Query().
		Where(
			chatmessage.ID(messageID),
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(
					user.ID(userID),
				),
			),
			chatmessage.OriginEQ(chatmessage.Origin(models.MessageOriginAssistant)),
		).
		WithChat(func(cq *ent.ChatQuery) {
			cq.WithPersonality()
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrChatMessageNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if expressionID != nil && *expressionID != uuid.Nil {
		var chatPersonalityID uuid.UUID
		if cm.Edges.Chat != nil && cm.Edges.Chat.Edges.Personality != nil {
			chatPersonalityID = cm.Edges.Chat.Edges.Personality.ID
		}
		if chatPersonalityID == uuid.Nil {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrGenerationExpressionPersonalityMismatch
		}
		ok, err := tx.PersonalityExpression.Query().
			Where(
				personalityexpression.ID(*expressionID),
				personalityexpression.HasPersonalityWith(
					personality.And(
						personality.ID(chatPersonalityID),
						personality.HasUserWith(user.ID(userID)),
					),
				),
			).
			Exist(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "personality expression"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
		if !ok {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrGenerationExpressionPersonalityMismatch
		}
	}

	upd := tx.ChatMessage.UpdateOneID(messageID)
	if expressionID == nil || *expressionID == uuid.Nil {
		upd.ClearGenerationExpression()
		upd.ClearGenerationExpressionReasoning()
	} else {
		upd.SetGenerationExpressionID(*expressionID)
		if r := trimGenerationExpressionReasoning(reasoning); r != "" {
			upd.SetGenerationExpressionReasoning(r)
		} else {
			upd.ClearGenerationExpressionReasoning()
		}
	}

	if _, err := upd.Save(ctx); err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	entChatMessage, err := tx.ChatMessage.Query().
		Where(chatmessage.ID(messageID)).
		WithChat().
		WithFileAttachments().
		WithRituals().
		WithToolCalls().
		WithContextItems().
		WithGenerationMood().
		WithGenerationExpression(func(q *ent.PersonalityExpressionQuery) {
			q.WithImage()
		}).
		Only(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toChatMessageModel(entChatMessage), nil
}

// SetChatMessageCheckpointCompletedAt records a successful scratchpad/memory/summary checkpoint on an assistant message.
func (d *Datastore) SetChatMessageCheckpointCompletedAt(ctx context.Context, userID, messageID uuid.UUID, completedAt time.Time) error {
	n, err := d.dbClient.ChatMessage.Update().
		Where(
			chatmessage.ID(messageID),
			chatmessage.OriginEQ(chatmessage.OriginAssistant),
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(user.ID(userID)),
			),
		).
		SetCheckpointCompletedAt(completedAt.UTC()).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to set checkpoint_completed_at on chat message", zap.Error(err))
		return err
	}
	if n == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}

// SetChatMessageContextBreakdown persists the model-context X-ray snapshot on an assistant
// message. Best-effort by contract: callers log and move on rather than failing the turn.
func (d *Datastore) SetChatMessageContextBreakdown(ctx context.Context, userID, messageID uuid.UUID, breakdown *modeltypes.ContextBreakdown) error {
	if breakdown == nil || len(breakdown.Segments) == 0 {
		return nil
	}
	n, err := d.dbClient.ChatMessage.Update().
		Where(
			chatmessage.ID(messageID),
			chatmessage.OriginEQ(chatmessage.OriginAssistant),
			chatmessage.HasChatWith(
				entchat.HasOwnerWith(user.ID(userID)),
			),
		).
		SetContextBreakdown(breakdown).
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to set context_breakdown on chat message", zap.Error(err))
		return err
	}
	if n == 0 {
		return ErrChatMessageNotFound
	}
	return nil
}
