package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entmemory "github.com/theimaginaryfoundation/what-iff/ent/memory"
	entmood "github.com/theimaginaryfoundation/what-iff/ent/mood"
	entexpression "github.com/theimaginaryfoundation/what-iff/ent/personalityexpression"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/userpreference"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// ErrForkMessageNotFound is returned when the branch-point message is missing or not in the parent thread.
var ErrForkMessageNotFound = errors.New("fork message not found in chat")

// forkMessageBatchSize bounds each bulk insert when copying a long thread's prefix.
const forkMessageBatchSize = 500

// maxForkNameLength keeps generated branch names comfortably inside the chat-name column.
const maxForkNameLength = 200

// ForkChat creates a new thread that shares the parent's history up to a message and then diverges.
//
// Copied: settings (model, personality, mood, tools, tags, MCP servers) and message text/metadata
// (origin, sent_at, generation model/personality/mood/expression) so the branch renders like the
// original. Not copied: file attachments, tool calls, context snapshots, bookmarks, provider
// response IDs, and thread-scoped memories — the branch starts clean on those.
//
// Context continuity: if the parent's last checkpoint happened at or before the branch point, its
// summary and window pointer are reused verbatim (the summarized turns are shared history). If the
// checkpoint is later than the branch point, reusing it would leak the "future" into the branch, so
// the branch is marked for rehydration instead (see models.ForkChatResult.NeedsRehydration).
func (d *Datastore) ForkChat(ctx context.Context, userID uuid.UUID, params models.ForkChatParams) (*models.ForkChatResult, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}
	committed := false
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
		if !committed {
			_ = tx.Rollback()
		}
	}()

	parent, err := tx.Chat.Query().
		Where(entchat.ID(params.ParentChatID), entchat.HasOwnerWith(user.ID(userID))).
		WithOwner().
		WithModel().
		WithPersonality().
		WithActiveMood().
		WithMcpServers().
		WithMemories(func(q *ent.MemoryQuery) { q.Where(entmemory.ScopeEQ(entmemory.ScopeSummary)) }).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrChatNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		return nil, err
	}

	pivot, err := tx.ChatMessage.Query().
		Where(chatmessage.ID(params.MessageID), chatmessage.HasChatWith(entchat.ID(parent.ID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrForkMessageNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		return nil, err
	}

	prefixPredicate := chatmessage.SentAtLT(pivot.SentAt)
	if params.IncludeMessage {
		prefixPredicate = chatmessage.Or(chatmessage.SentAtLT(pivot.SentAt), chatmessage.ID(pivot.ID))
	}
	prefix, err := tx.ChatMessage.Query().
		Where(chatmessage.HasChatWith(entchat.ID(parent.ID)), prefixPredicate).
		WithGenerationMood(func(q *ent.MoodQuery) { q.Select(entmood.FieldID) }).
		WithGenerationExpression(func(q *ent.PersonalityExpressionQuery) { q.Select(entexpression.FieldID) }).
		Order(chatmessage.BySentAt(), chatmessage.ByID(sql.OrderAsc())).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat message"), zap.Error(err))
		return nil, err
	}

	parentModel := toChatModel(parent)
	now := time.Now()
	lastMessageTime := now
	if n := len(prefix); n > 0 {
		lastMessageTime = prefix[n-1].SentAt
	}

	create := tx.Chat.Create().
		SetOwnerID(userID).
		SetName(forkChatName(params.Name, parent.Name)).
		SetLastMessageTime(lastMessageTime).
		SetIsAutoMood(parent.IsAutoMood).
		SetForkedFromChatID(parent.ID).
		SetForkedFromMessageID(pivot.ID)
	if modelID, err := d.forkModelID(ctx, tx, userID, parent); err != nil {
		return nil, err
	} else if modelID != uuid.Nil {
		create.SetModelID(modelID)
	}
	if parent.Edges.Personality != nil {
		create.SetPersonalityID(parent.Edges.Personality.ID)
	}
	if parent.Edges.ActiveMood != nil {
		create.SetActiveMoodID(parent.Edges.ActiveMood.ID)
	}
	if len(parent.DisabledTools) > 0 {
		create.SetDisabledTools(parent.DisabledTools)
	}
	if len(parent.Tags) > 0 {
		create.SetTags(parent.Tags)
	}
	for _, server := range parent.Edges.McpServers {
		create.AddMcpServerIDs(server.ID)
	}

	needsRehydration := forkNeedsRehydration(parentModel, pivot.SentAt, len(prefix))
	switch {
	case needsRehydration:
		create.SetRehydrationState(models.RehydrationStatePending)
	case parentModel.LastCheckpointAt != nil:
		// The checkpoint only covers shared history: reuse it so the branch keeps the same context.
		create.SetCheckpointSummary(parentModel.CheckpointSummary).
			SetCheckpointUserMessageCount(parentModel.CheckpointUserMessageCount).
			SetLastCheckpointAt(*parentModel.LastCheckpointAt)
		if parentModel.RehydrationState != "" {
			create.SetRehydrationState(models.RehydrationStateReady)
		}
	}

	branch, err := create.Save(ctx)
	if err != nil {
		d.logger.Error("chat fork: failed to create branch", zap.String("parent_chat_id", parent.ID.String()), zap.Error(err))
		return nil, err
	}

	for start := 0; start < len(prefix); start += forkMessageBatchSize {
		end := min(start+forkMessageBatchSize, len(prefix))
		batch := prefix[start:end]
		if _, err := tx.ChatMessage.MapCreateBulk(batch, func(c *ent.ChatMessageCreate, i int) {
			copyForkMessage(c, batch[i], branch.ID)
		}).Save(ctx); err != nil {
			d.logger.Error("chat fork: failed to copy messages", zap.String("parent_chat_id", parent.ID.String()), zap.Error(err))
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	committed = true

	out, err := d.GetChat(ctx, userID, branch.ID)
	if err != nil {
		return nil, fmt.Errorf("load branch: %w", err)
	}
	return &models.ForkChatResult{Chat: out, CopiedMessages: len(prefix), NeedsRehydration: needsRehydration}, nil
}

// forkModelID keeps the parent's model when it is still active and usable by the user (the same
// gate CreateChat applies to an explicit model); otherwise it falls back to the user's default.
func (d *Datastore) forkModelID(ctx context.Context, tx *ent.Tx, userID uuid.UUID, parent *ent.Chat) (uuid.UUID, error) {
	if m := parent.Edges.Model; m != nil && !m.Deleted {
		if err := d.assertUserCanUseModel(ctx, tx, userID, m.ID); err == nil {
			return m.ID, nil
		}
	}
	prefs, err := tx.UserPreference.Query().
		Where(userpreference.HasUserWith(user.ID(userID))).
		WithModel().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return uuid.Nil, nil
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "user preferences"), zap.Error(err))
		return uuid.Nil, err
	}
	if prefs.Edges.Model != nil {
		return prefs.Edges.Model.ID, nil
	}
	return uuid.Nil, nil
}

// forkNeedsRehydration decides whether the branch must re-summarize its copied prefix.
func forkNeedsRehydration(parent *models.Chat, branchPoint time.Time, copied int) bool {
	if copied == 0 {
		return false
	}
	switch parent.RehydrationState {
	case models.RehydrationStatePending, models.RehydrationStateProcessing, models.RehydrationStateFailed:
		// The parent's own summary isn't usable yet; summarize the branch's prefix independently.
		return true
	}
	if parent.LastCheckpointAt == nil {
		// Unsummarized imports can be arbitrarily long; native threads without a checkpoint fit the
		// history window by construction.
		return parent.Source != nil && *parent.Source != ""
	}
	// The checkpoint window starts at LastCheckpointAt; if that is after the branch point the
	// summary includes turns the branch never had.
	return parent.LastCheckpointAt.After(branchPoint)
}

func copyForkMessage(c *ent.ChatMessageCreate, m *ent.ChatMessage, chatID uuid.UUID) {
	c.SetChatID(chatID).
		SetMessage(m.Message).
		SetOrigin(m.Origin).
		SetReadStatus(chatmessage.ReadStatusRead).
		SetSentAt(m.SentAt).
		SetGenerationModel(m.GenerationModel).
		SetGenerationPersonality(m.GenerationPersonality).
		SetNillableGenerationExpressionReasoning(m.GenerationExpressionReasoning)
	if m.Tokens != 0 {
		c.SetTokens(m.Tokens)
	}
	if m.Edges.GenerationMood != nil {
		c.SetGenerationMoodID(m.Edges.GenerationMood.ID)
	}
	if m.Edges.GenerationExpression != nil {
		c.SetGenerationExpressionID(m.Edges.GenerationExpression.ID)
	}
}

// forkChatName returns the caller's name, or "What if: <parent>" trimmed to a safe length.
func forkChatName(requested, parentName string) string {
	name := strings.TrimSpace(requested)
	if name == "" {
		base := strings.TrimSpace(parentName)
		if base == "" {
			base = "Untitled thread"
		}
		name = "What if: " + base
	}
	if r := []rune(name); len(r) > maxForkNameLength {
		name = string(r[:maxForkNameLength])
	}
	return name
}

// ListChatBranches returns the user's threads branched directly from chatID, newest first.
func (d *Datastore) ListChatBranches(ctx context.Context, userID, chatID uuid.UUID) ([]models.ChatBranchSummary, error) {
	rows, err := d.dbClient.Chat.Query().
		Where(entchat.HasOwnerWith(user.ID(userID)), entchat.ForkedFromChatIDEQ(chatID)).
		Order(entchat.ByCreatedAt(sql.OrderDesc())).
		Select(entchat.FieldID, entchat.FieldName, entchat.FieldForkedFromMessageID, entchat.FieldLastMessageTime, entchat.FieldCreatedAt).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		return nil, err
	}
	out := make([]models.ChatBranchSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.ChatBranchSummary{
			ID:                  r.ID,
			Name:                r.Name,
			ForkedFromMessageID: r.ForkedFromMessageID,
			LastMessageTime:     r.LastMessageTime,
			CreatedAt:           r.CreatedAt,
		})
	}
	return out, nil
}

// GetChatName returns a chat's name when the user owns it; ErrChatNotFound otherwise.
// Used by lineage UI to label a branch's parent without loading the full chat.
func (d *Datastore) GetChatName(ctx context.Context, userID, chatID uuid.UUID) (string, error) {
	row, err := d.dbClient.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Select(entchat.FieldName).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", ErrChatNotFound
		}
		return "", err
	}
	return row.Name, nil
}
