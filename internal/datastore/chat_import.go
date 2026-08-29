package datastore

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entuser "github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// importHashIndex is the DB index name for the (owner_id, import_hash) unique constraint.
// Matching by index name in the driver error string is intentional: it lets us distinguish a
// dedup-race violation on this specific index from any other constraint error on the chats table.
// If the index is renamed in a future migration this constant must be updated to match.
const importHashIndex = "chat_import_hash_user_chats"

// ImportChats persists parsed conversations (from any supported export source) for the user, one
// transaction per conversation. Imports are intentionally sequential; result is shared across
// iterations and not goroutine-safe.
// TODO: for very large exports consider a bounded worker pool once contention on the unique index is understood.
// On context cancellation the function returns early with a non-nil error and a partial ImportResult
// containing whatever was committed before cancellation. Per-conversation failures are recorded in result.Errors.
// onProgress, when non-nil, is invoked after each conversation with the running imported/skipped counts
// so callers can surface progress; it must not block for long as it runs inline with the import loop.
func (d *Datastore) ImportChats(ctx context.Context, userID uuid.UUID, convs []models.ImportConversation, onProgress func(imported, skipped int)) (*models.ImportResult, error) {
	result := &models.ImportResult{Errors: []string{}}

	for i := range convs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		d.importOneConversation(ctx, userID, convs[i], result)
		if onProgress != nil {
			onProgress(result.Imported, result.Skipped)
		}
	}

	return result, nil
}

// importOneConversation attempts to persist a single conversation in its own transaction.
// All per-conversation failures are logged with full detail server-side and appended as redacted
// messages to result; the function always returns so the caller continues to the next conversation.
func (d *Datastore) importOneConversation(ctx context.Context, userID uuid.UUID, conv models.ImportConversation, result *models.ImportResult) {
	if conv.ImportHash == "" {
		result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: empty import hash; skipping", models.TruncateImportTitle(conv.Title)))
		return
	}

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("chat import: failed to start transaction",
			zap.String("title", conv.Title), zap.Error(err))
		result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: failed to start transaction", models.TruncateImportTitle(conv.Title)))
		return
	}

	// Contain panics to this conversation: roll back its transaction and record a redacted error,
	// then return normally so the outer loop continues. A single malformed conversation must not be
	// able to abort the entire import batch (the job's top-level recover would fail the whole import).
	// The persist call is a single expression so that, after recover(), we can check panicked and
	// return before any post-panic logic (e.g. Imported++) runs.
	panicked := false
	defer func() {
		if v := recover(); v != nil {
			d.logger.Error("chat import: panic during conversation; rolling back and skipping",
				zap.String("title", conv.Title),
				zap.Any("panic", v),
				zap.ByteString("stack", debug.Stack()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: unexpected error; skipped", models.TruncateImportTitle(conv.Title)))
			panicked = true
		}
	}()

	committed := d.persistImportedConversation(ctx, tx, userID, conv, result)
	if panicked {
		return
	}
	if committed {
		result.Imported++
	}
}

// normalizeImportedMessageTimes preserves the archive transcript order under the datastore's
// (sent_at, id) retrieval key. Provider exports can contain equal, missing-fallback, or regressing
// timestamps; without normalization equal times are broken by UUID ordering, which is unrelated to
// conversational sequence. Existing increasing timestamps are preserved exactly. A non-increasing
// timestamp is advanced by the minimum representable duration so source order remains authoritative.
func normalizeImportedMessageTimes(messages []models.ChatMessage) []models.ChatMessage {
	if len(messages) < 2 {
		return messages
	}

	normalized := append([]models.ChatMessage(nil), messages...)
	previous := normalized[0].SentAt.UTC()
	normalized[0].SentAt = previous
	for i := 1; i < len(normalized); i++ {
		candidate := normalized[i].SentAt.UTC()
		if !candidate.After(previous) {
			candidate = previous.Add(time.Nanosecond)
		}
		normalized[i].SentAt = candidate
		previous = candidate
	}
	return normalized
}

// persistImportedConversation runs dedup, create, bulk messages, and commit inside an open tx.
// Returns true only when the conversation was committed. Skip and error paths update result in place.
func (d *Datastore) persistImportedConversation(ctx context.Context, tx *ent.Tx, userID uuid.UUID, conv models.ImportConversation, result *models.ImportResult) (committed bool) {
	// rollback is safe to call multiple times in pre-commit error paths; ent.Tx.Rollback is idempotent.
	rollback := func() {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
	}

	// Dedup check: skip if this user already has a chat with the same import hash.
	exists, err := tx.Chat.Query().
		Where(
			entchat.HasOwnerWith(entuser.ID(userID)),
			entchat.ImportHashEQ(conv.ImportHash),
		).
		Exist(ctx)
	if err != nil {
		rollback()
		if ctx.Err() != nil {
			// Context was canceled; don't append a per-conversation error — the outer loop
			// will detect cancellation on the next iteration and return early.
			return false
		}
		d.logger.Error("chat import: dedup check failed",
			zap.String("title", conv.Title), zap.Error(err))
		result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: database error during dedup check", models.TruncateImportTitle(conv.Title)))
		return false
	}
	if exists {
		rollback()
		result.Skipped++
		return false
	}

	// Normalize imported timestamps before both metadata calculation and persistence so the archive's
	// transcript sequence remains the authoritative ordering even when source timestamps tie/regress.
	messages := normalizeImportedMessageTimes(conv.Messages)

	lastMsgTime := conv.CreatedAt
	for _, msg := range messages {
		if msg.SentAt.After(lastMsgTime) {
			lastMsgTime = msg.SentAt
		}
	}

	source := conv.Source
	if source == "" {
		source = models.ChatSourceOpenAI
	}

	entChat, err := tx.Chat.Create().
		SetName(conv.Title).
		SetOwnerID(userID).
		SetArchived(true).
		SetSource(source).
		SetImportHash(conv.ImportHash).
		SetIsAutoMood(true).
		SetCreatedAt(conv.CreatedAt).
		SetLastMessageTime(lastMsgTime).
		Save(ctx)
	if err != nil {
		rollback()
		// Only treat the specific import-hash uniqueness violation as a dedup race;
		// other constraint errors (e.g. on future unique indexes) surface as real failures.
		// String matching on the index name is intentional — see importHashIndex comment.
		if ent.IsConstraintError(err) && strings.Contains(err.Error(), importHashIndex) {
			result.Skipped++
			return false
		}
		d.logger.Error("chat import: failed to create chat",
			zap.String("title", conv.Title), zap.Error(err))
		result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: database error creating chat", models.TruncateImportTitle(conv.Title)))
		return false
	}

	if len(messages) > 0 {
		_, err = tx.ChatMessage.MapCreateBulk(messages, func(c *ent.ChatMessageCreate, i int) {
			msg := messages[i]
			c.SetMessage(msg.Message).
				SetOrigin(chatmessage.Origin(msg.Origin)).
				SetReadStatus(chatmessage.ReadStatusRead).
				SetSentAt(msg.SentAt).
				SetChatID(entChat.ID)
		}).Save(ctx)
		if err != nil {
			rollback()
			d.logger.Error("chat import: failed to bulk create messages",
				zap.String("title", conv.Title), zap.Error(err))
			result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: database error creating messages", models.TruncateImportTitle(conv.Title)))
			return false
		}
	}

	// tx.Commit() transitions the transaction to a terminal state; do not call rollback after this
	// regardless of outcome — a second operation on an already-closed transaction will itself error.
	if err := tx.Commit(); err != nil {
		d.logger.Error("chat import: failed to commit transaction",
			zap.String("title", conv.Title), zap.Error(err))
		result.Errors = append(result.Errors, fmt.Sprintf("conversation %q: database error committing transaction", models.TruncateImportTitle(conv.Title)))
		return false
	}

	result.ImportedIDs = append(result.ImportedIDs, entChat.ID)
	return true
}
