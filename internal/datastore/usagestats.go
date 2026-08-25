package datastore

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (d *Datastore) GetUsageStats(ctx context.Context, startDate, endDate time.Time, userID uuid.UUID) (*models.UsageStats, error) {
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

	// Count distinct chats for the user within the date range
	chatCount, err := tx.Chat.Query().
		Where(
			chat.HasOwnerWith(
				user.ID(userID),
			),
			chat.Archived(false),
			chat.HasMessagesWith(
				chatmessage.SentAtGTE(startDate),
				chatmessage.SentAtLTE(endDate),
			),
		).
		Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "chats"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Count total messages for the user within the date range
	messageCount, err := tx.ChatMessage.Query().
		Where(
			chatmessage.HasChatWith(
				chat.HasOwnerWith(
					user.ID(userID),
				),
				chat.Archived(false),
			),
			chatmessage.SentAtGTE(startDate),
			chatmessage.SentAtLTE(endDate),
		).
		Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "messages"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Sum input tokens (User messages) using SQL aggregation
	var inputTokenResult []struct {
		Sum int64
	}
	err = tx.ChatMessage.Query().
		Where(
			chatmessage.HasChatWith(
				chat.HasOwnerWith(
					user.ID(userID),
				),
				chat.Archived(false),
			),
			chatmessage.SentAtGTE(startDate),
			chatmessage.SentAtLTE(endDate),
			chatmessage.OriginEQ(chatmessage.OriginUser),
			chatmessage.TokensNotNil(),
		).
		Aggregate(ent.Sum(chatmessage.FieldTokens)).
		Scan(ctx, &inputTokenResult)
	if err != nil {
		d.logger.Error(i18n.T("usagestats.input_tokens_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	var inputTokenSum int64
	if len(inputTokenResult) > 0 {
		inputTokenSum = inputTokenResult[0].Sum
	}

	// Sum output tokens (Assistant messages) using SQL aggregation
	var outputTokenResult []struct {
		Sum int64
	}
	err = tx.ChatMessage.Query().
		Where(
			chatmessage.HasChatWith(
				chat.HasOwnerWith(
					user.ID(userID),
				),
				chat.Archived(false),
			),
			chatmessage.SentAtGTE(startDate),
			chatmessage.SentAtLTE(endDate),
			chatmessage.OriginEQ(chatmessage.OriginAssistant),
			chatmessage.TokensNotNil(),
		).
		Aggregate(ent.Sum(chatmessage.FieldTokens)).
		Scan(ctx, &outputTokenResult)
	if err != nil {
		d.logger.Error(i18n.T("usagestats.output_tokens_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	var outputTokenSum int64
	if len(outputTokenResult) > 0 {
		outputTokenSum = outputTokenResult[0].Sum
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.UsageStats{
		Chats:        int64(chatCount),
		Messages:     int64(messageCount),
		InputTokens:  inputTokenSum,
		OutputTokens: outputTokenSum,
	}, nil
}
