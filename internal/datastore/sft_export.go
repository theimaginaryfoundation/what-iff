package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"go.uber.org/zap"
)

const defaultSFTExportBatchSize = 100

// SFTExportOptions configures the user SFT JSONL export.
type SFTExportOptions struct {
	BatchSize int
}

// SFTExportStats captures export totals and skip reasons.
type SFTExportStats struct {
	ChatsScanned                int
	MessagesScanned             int
	PersonalitiesLoaded         int
	ExportedPairs               int
	SkippedAssistantWithoutUser int
	SkippedUserWithoutAssistant int
	SkippedMissingPersonality   int
	SkippedEmptyMessages        int
}

type sftTrainingMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sftTrainingExample struct {
	Messages []sftTrainingMessage `json:"messages"`
}

func (o SFTExportOptions) batchSize() int {
	if o.BatchSize > 0 {
		return o.BatchSize
	}
	return defaultSFTExportBatchSize
}

func normalizeSFTPersonalityName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func buildSFTSystemMessageContent(personalityModel *ent.Personality) string {
	if personalityModel == nil {
		return ""
	}
	systemPrompt := strings.TrimSpace(personalityModel.SystemPrompt)
	scratchpad := strings.TrimSpace(personalityModel.Scratchpad)
	switch {
	case systemPrompt != "" && scratchpad != "":
		return systemPrompt + "\n\n" + scratchpad
	case systemPrompt != "":
		return systemPrompt
	default:
		return scratchpad
	}
}

func resolveSFTPersonalityForAssistant(
	assistantMessage *ent.ChatMessage,
	personalityByName map[string]*ent.Personality,
	chatFallback *ent.Personality,
) *ent.Personality {
	if assistantMessage != nil {
		nameKey := normalizeSFTPersonalityName(assistantMessage.GenerationPersonality)
		if nameKey != "" {
			if matched, ok := personalityByName[nameKey]; ok && matched != nil {
				return matched
			}
		}
	}
	return chatFallback
}

func writeSFTTrainingExample(encoder *json.Encoder, systemMessage, userMessage, assistantMessage string) error {
	record := sftTrainingExample{
		Messages: []sftTrainingMessage{
			{Role: "system", Content: systemMessage},
			{Role: "user", Content: userMessage},
			{Role: "assistant", Content: assistantMessage},
		},
	}
	return encoder.Encode(&record)
}

func processSFTMessageBatch(
	encoder *json.Encoder,
	messages []*ent.ChatMessage,
	personalityByName map[string]*ent.Personality,
	chatFallback *ent.Personality,
	pendingUserMessage *ent.ChatMessage,
	stats *SFTExportStats,
) (*ent.ChatMessage, error) {
	if stats != nil {
		stats.MessagesScanned += len(messages)
	}

	for _, message := range messages {
		if message == nil {
			continue
		}
		switch message.Origin {
		case chatmessage.OriginUser:
			if pendingUserMessage != nil && stats != nil {
				stats.SkippedUserWithoutAssistant++
			}
			pendingUserMessage = message
		case chatmessage.OriginAssistant:
			if pendingUserMessage == nil {
				if stats != nil {
					stats.SkippedAssistantWithoutUser++
				}
				continue
			}

			userText := strings.TrimSpace(pendingUserMessage.Message)
			assistantText := strings.TrimSpace(message.Message)
			if userText == "" || assistantText == "" {
				if stats != nil {
					stats.SkippedEmptyMessages++
				}
				pendingUserMessage = nil
				continue
			}

			personalityModel := resolveSFTPersonalityForAssistant(message, personalityByName, chatFallback)
			systemText := buildSFTSystemMessageContent(personalityModel)
			if systemText == "" {
				if stats != nil {
					stats.SkippedMissingPersonality++
				}
				pendingUserMessage = nil
				continue
			}

			if err := writeSFTTrainingExample(encoder, systemText, userText, assistantText); err != nil {
				return nil, fmt.Errorf("write SFT training example: %w", err)
			}
			if stats != nil {
				stats.ExportedPairs++
			}
			pendingUserMessage = nil
		}
	}

	return pendingUserMessage, nil
}

func (d *Datastore) loadSFTPersonalityMap(
	ctx context.Context,
	tx *ent.Tx,
	userID uuid.UUID,
) (map[string]*ent.Personality, int, error) {
	entPersonalities, err := tx.Personality.Query().
		Where(personality.HasUserWith(user.ID(userID))).
		Order(ent.Asc(personality.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}

	personalityByName := make(map[string]*ent.Personality, len(entPersonalities))
	for _, item := range entPersonalities {
		if item == nil {
			continue
		}
		nameKey := normalizeSFTPersonalityName(item.Name)
		if nameKey == "" {
			continue
		}
		if _, exists := personalityByName[nameKey]; !exists {
			personalityByName[nameKey] = item
		}
	}

	return personalityByName, len(entPersonalities), nil
}

// ExportUserSFTJSONL streams OpenAI chat-format SFT examples for one user.
func (d *Datastore) ExportUserSFTJSONL(ctx context.Context, userID uuid.UUID, w io.Writer, opts SFTExportOptions) (*SFTExportStats, error) {
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

	userExists, err := tx.User.Query().Where(user.ID(userID)).Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "user"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if !userExists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrUnauthorized
	}

	stats := &SFTExportStats{}
	personalityByName, personalitiesLoaded, err := d.loadSFTPersonalityMap(ctx, tx, userID)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	stats.PersonalitiesLoaded = personalitiesLoaded

	encoder := json.NewEncoder(w)
	batchSize := opts.batchSize()
	chatOffset := 0

	for {
		entChats, queryErr := tx.Chat.Query().
			Where(entchat.HasOwnerWith(user.ID(userID))).
			WithPersonality().
			Order(ent.Asc(entchat.FieldCreatedAt)).
			Offset(chatOffset).
			Limit(batchSize).
			All(ctx)
		if queryErr != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(queryErr))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, queryErr
		}
		if len(entChats) == 0 {
			break
		}

		for _, entChatItem := range entChats {
			stats.ChatsScanned++
			messageOffset := 0
			var pendingUserMessage *ent.ChatMessage

			for {
				entMessages, messageErr := tx.ChatMessage.Query().
					Where(chatmessage.HasChatWith(entchat.ID(entChatItem.ID))).
					Order(ent.Asc(chatmessage.FieldSentAt), ent.Asc(chatmessage.FieldID)).
					Offset(messageOffset).
					Limit(batchSize).
					All(ctx)
				if messageErr != nil {
					d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(messageErr))
					if rerr := tx.Rollback(); rerr != nil {
						d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
					}
					return nil, messageErr
				}
				if len(entMessages) == 0 {
					break
				}

				pendingUserMessage, err = processSFTMessageBatch(
					encoder,
					entMessages,
					personalityByName,
					entChatItem.Edges.Personality,
					pendingUserMessage,
					stats,
				)
				if err != nil {
					d.logger.Error("failed to process SFT message batch",
						zap.String("chat_id", entChatItem.ID.String()),
						zap.Error(err),
					)
					if rerr := tx.Rollback(); rerr != nil {
						d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
					}
					return nil, err
				}

				messageOffset += len(entMessages)
				if len(entMessages) < batchSize {
					break
				}
			}

			if pendingUserMessage != nil {
				stats.SkippedUserWithoutAssistant++
			}
		}

		chatOffset += len(entChats)
		if len(entChats) < batchSize {
			break
		}
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return stats, nil
}
