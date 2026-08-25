package datastore

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ExportChat streams a chat and all its messages as a ZIP file to the provided writer.
// The ZIP contains two files:
//   - chat.json: Chat metadata as models.ChatExport
//   - messages.jsonl: All chat messages as models.ChatMessageExport (one JSON object per line)
//
// This method is designed to be memory and bandwidth efficient by streaming messages
// directly to the ZIP file without loading everything into memory.
func (d *Datastore) ExportChat(ctx context.Context, userID, chatID uuid.UUID, w io.Writer) error {
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

	// Query chat with authorization check
	entChat, err := tx.Chat.Query().
		Where(
			entchat.ID(chatID),
			entchat.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithModel().
		WithPersonality().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("chatexport.chat_not_found_or_unauthorized", "ChatID", chatID.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return ErrChatNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Create ZIP writer - defer close to ensure ZIP is properly finalized even on error
	// This gives users a valid (but potentially incomplete) ZIP file rather than corrupted data
	zipWriter := zip.NewWriter(w)
	defer func() {
		if closeErr := zipWriter.Close(); closeErr != nil {
			d.logger.Error(i18n.T("chatexport.zip_close_failed"), zap.Error(closeErr))
		}
	}()

	// Write chat.json
	chatExport := models.ChatExport{
		ID:              entChat.ID,
		Name:            entChat.Name,
		LastMessageTime: entChat.LastMessageTime,
		CreatedAt:       entChat.CreatedAt,
		UpdatedAt:       entChat.UpdatedAt,
	}

	if entChat.Edges.Model != nil {
		chatExport.ModelName = entChat.Edges.Model.Name
	}

	if entChat.Edges.Personality != nil {
		chatExport.PersonalityName = entChat.Edges.Personality.Name
	}

	chatFile, err := zipWriter.Create("chat.json")
	if err != nil {
		d.logger.Error(i18n.T("chatexport.chat_json_create_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if err := json.NewEncoder(chatFile).Encode(chatExport); err != nil {
		d.logger.Error(i18n.T("chatexport.chat_json_encode_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Write messages.jsonl
	messagesFile, err := zipWriter.Create("messages.jsonl")
	if err != nil {
		d.logger.Error(i18n.T("chatexport.messages_jsonl_create_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Stream messages in batches to avoid loading everything into memory
	const batchSize = 100
	encoder := json.NewEncoder(messagesFile)
	offset := 0

	// Pre-allocate reusable structures to minimize allocations in the loop
	var messageExport models.ChatMessageExport
	var attachmentNames []string
	var ritualNames []string
	var toolCallNames []string

	for {
		// Query messages in batches, ordered by sent_at ascending (chronological order)
		entMessages, err := tx.ChatMessage.Query().
			Where(
				chatmessage.HasChatWith(
					entchat.ID(chatID),
				),
			).
			WithFileAttachments().
			WithRituals().
			WithToolCalls().
			Order(ent.Asc(chatmessage.FieldSentAt)).
			Offset(offset).
			Limit(batchSize).
			All(ctx)

		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "chat messages"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return err
		}

		// No more messages to process
		if len(entMessages) == 0 {
			break
		}

		// Convert and write each message to JSONL
		for _, entMessage := range entMessages {
			// Reuse the message export struct, updating its fields
			messageExport.Message = entMessage.Message
			messageExport.Origin = models.MessageOrigin(entMessage.Origin)
			messageExport.SentAt = entMessage.SentAt
			messageExport.Attachments = nil // Reset to nil to avoid including previous message's data
			messageExport.Rituals = nil     // Reset to nil to avoid including previous message's data
			messageExport.ToolCalls = nil   // Reset to nil to avoid including previous message's data

			// Add attachment filenames - reuse slice to minimize allocations
			numAttachments := len(entMessage.Edges.FileAttachments)
			if numAttachments > 0 {
				// Grow slice only if needed
				if cap(attachmentNames) < numAttachments {
					attachmentNames = make([]string, numAttachments)
				} else {
					attachmentNames = attachmentNames[:numAttachments]
				}
				for i, attachment := range entMessage.Edges.FileAttachments {
					attachmentNames[i] = attachment.Name
				}
				messageExport.Attachments = attachmentNames
			}

			// Add ritual names - reuse slice to minimize allocations
			numRituals := len(entMessage.Edges.Rituals)
			if numRituals > 0 {
				// Grow slice only if needed
				if cap(ritualNames) < numRituals {
					ritualNames = make([]string, numRituals)
				} else {
					ritualNames = ritualNames[:numRituals]
				}
				for i, ritual := range entMessage.Edges.Rituals {
					ritualNames[i] = ritual.Name
				}
				messageExport.Rituals = ritualNames
			}

			// Add tool call names - reuse slice to minimize allocations
			numToolCalls := len(entMessage.Edges.ToolCalls)
			if numToolCalls > 0 {
				// Grow slice only if needed
				if cap(toolCallNames) < numToolCalls {
					toolCallNames = make([]string, numToolCalls)
				} else {
					toolCallNames = toolCallNames[:numToolCalls]
				}
				for i, toolCall := range entMessage.Edges.ToolCalls {
					toolCallNames[i] = toolCall.ToolName
				}
				messageExport.ToolCalls = toolCallNames
			}

			// Write message as a single JSON line
			// Safe to reuse messageExport because Encode serializes immediately
			// and doesn't hold references after the call completes
			if err := encoder.Encode(&messageExport); err != nil {
				d.logger.Error(i18n.T("chatexport.message_encode_failed"), zap.Error(err))
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return err
			}
		}

		// Move to next batch
		offset += len(entMessages)

		// If we got fewer than batchSize, we're done
		if len(entMessages) < batchSize {
			break
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	// ZIP writer will be closed by defer, ensuring proper finalization even on error
	return nil
}
