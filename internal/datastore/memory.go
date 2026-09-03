package datastore

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/embedding"
	"github.com/theimaginaryfoundation/what-iff/ent/memory"
	entpersonality "github.com/theimaginaryfoundation/what-iff/ent/personality"
	entschema "github.com/theimaginaryfoundation/what-iff/ent/schema"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"go.uber.org/zap"
)

const MemoryRelevanceThreshold = 1.2
const memoryImportBatchSize = 200
const memoryImportEmbeddingWorkers = 8
const memoryImportMaxJSONLine = 16 << 20 // 16 MiB, aligned with account backup JSONL cap

func memoryLevelForEntity(mem *ent.Memory) models.MemoryLevel {
	switch mem.Scope {
	case memory.ScopeSummary:
		return models.MemoryLevelSummary
	case memory.ScopeChat:
		return models.MemoryLevelThread
	case memory.ScopeUser:
		if mem.PinnedPersonalityID != nil {
			return models.MemoryLevelPersonality
		}
		return models.MemoryLevelGlobal
	default:
		return models.MemoryLevelThread
	}
}

// memoryCursorPredicate returns a Where predicate for cursor-based pagination
// ordered by (created_at ASC, id ASC). Pass zero values for the first page.
func memoryCursorPredicate(lastCreatedAt time.Time, lastID uuid.UUID) func(*ent.MemoryQuery) *ent.MemoryQuery {
	return func(q *ent.MemoryQuery) *ent.MemoryQuery {
		if lastCreatedAt.IsZero() {
			return q
		}
		return q.Where(memory.Or(
			memory.CreatedAtGT(lastCreatedAt),
			memory.And(memory.CreatedAtEQ(lastCreatedAt), memory.IDGT(lastID)),
		))
	}
}

// Convert from Ent Memory to model
func toMemoryModel(e *ent.Memory) *models.Memory {
	if e == nil {
		return nil
	}

	status := models.MemoryStatusActive
	if e.Status != "" {
		status = models.MemoryStatus(e.Status)
	}

	memoryModel := &models.Memory{
		ID:         e.ID,
		Content:    e.Content,
		Level:      memoryLevelForEntity(e),
		Type:       models.MemoryType(e.Type),
		Status:     status,
		Confidence: models.ClampConfidence(e.Confidence),
		Starred:    e.Starred,
		Scope:      string(e.Scope),
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
	if e.ChainMetadata != nil {
		memoryModel.ChainMetadata = chainMetadataToModel(e.ChainMetadata)
	}

	// Add pinned personality ID if set
	if e.PinnedPersonalityID != nil {
		memoryModel.PinnedPersonalityID = e.PinnedPersonalityID
	}

	// Add chat name if available
	if e.Edges.Chat != nil {
		memoryModel.ChatID = e.Edges.Chat.ID
		memoryModel.ChatName = e.Edges.Chat.Name
	}

	return memoryModel
}

func chainMetadataToModel(meta *entschema.MemoryChainMetadata) *models.MemoryChainMetadata {
	if meta == nil {
		return nil
	}
	modelMeta := &models.MemoryChainMetadata{
		DuplicateCount:      meta.DuplicateCount,
		MergedFromMemoryIDs: meta.MergedFromMemoryIDs,
	}
	for _, ts := range meta.VerifiedTimestampsFirst {
		modelMeta.VerifiedTimestampsFirst = append(modelMeta.VerifiedTimestampsFirst, ts.UTC().Format(time.RFC3339))
	}
	for _, ts := range meta.VerifiedTimestampsLast {
		modelMeta.VerifiedTimestampsLast = append(modelMeta.VerifiedTimestampsLast, ts.UTC().Format(time.RFC3339))
	}
	return modelMeta
}

func (d *Datastore) CreateMemory(ctx context.Context, userID uuid.UUID, mem models.Memory, embedding []float32, activePersonalityID uuid.UUID) (*ent.Memory, error) {
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

	// If provided, validate chat ownership before linking.
	if mem.ChatID != uuid.Nil {
		chatExists, err := tx.Chat.Query().
			Where(
				entchat.ID(mem.ChatID),
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
			d.logger.Error(i18n.T2("memory.chat_not_found_or_unauthorized", "ChatID", mem.ChatID.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrChatNotFound
		}
	}

	// Determine pinned personality ID for User-scoped memories. Decision order:
	//  1. Manual: mem.PinnedPersonalityID set — use it directly.
	//  2. Auto:   scope=User + active personality — pin if personality.AutoPinMemories.
	//  3. None:   scope=Chat, or no active personality, or auto-pin disabled — leave unpinned.
	var pinnedPersonalityID *uuid.UUID

	if mem.PinnedPersonalityID != nil {
		pinnedPersonalityID = mem.PinnedPersonalityID
	} else if mem.Scope == "User" && activePersonalityID != uuid.Nil {
		personality, err := tx.Personality.Get(ctx, activePersonalityID)
		if err != nil {
			d.logger.Warn(i18n.T1("memory.personality_auto_pin_failed", "PersonalityID", activePersonalityID.String()), zap.Error(err))
		} else if personality != nil && personality.AutoPinMemories {
			pinnedPersonalityID = &activePersonalityID
		}
	}

	// Create memory
	memCreate := tx.Memory.Create().
		SetContent(mem.Content).
		SetScope(memory.Scope(mem.Scope)).
		SetOwnerID(userID).
		SetType(memory.TypeContext).
		SetStatus(normalizeMemoryStatus(mem.Status)).
		SetConfidence(models.ClampConfidence(mem.Confidence)).
		SetStarred(mem.Starred).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now())

	if mem.ChatID != uuid.Nil {
		memCreate.SetChatID(mem.ChatID)
	}

	// Set pinned personality ID if determined
	if pinnedPersonalityID != nil {
		memCreate.SetPinnedPersonalityID(*pinnedPersonalityID)
	}
	if mem.ChainMetadata != nil {
		first := make([]time.Time, 0, len(mem.ChainMetadata.VerifiedTimestampsFirst))
		for _, raw := range mem.ChainMetadata.VerifiedTimestampsFirst {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				first = append(first, parsed.UTC())
			}
		}
		last := make([]time.Time, 0, len(mem.ChainMetadata.VerifiedTimestampsLast))
		for _, raw := range mem.ChainMetadata.VerifiedTimestampsLast {
			if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
				last = append(last, parsed.UTC())
			}
		}
		memCreate.SetChainMetadata(&entschema.MemoryChainMetadata{
			DuplicateCount:          mem.ChainMetadata.DuplicateCount,
			VerifiedTimestampsFirst: first,
			VerifiedTimestampsLast:  last,
			MergedFromMemoryIDs:     mem.ChainMetadata.MergedFromMemoryIDs,
		})
	}

	newMem, err := memCreate.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "memory"), zap.Error(err))
		return nil, err
	}

	if len(embedding) > 0 {
		_, err = tx.Embedding.Create().
			SetEmbedding(pgvector.NewVector(embedding)).
			SetMemoryID(newMem.ID).
			Save(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("create.failed", "Entity", "embedding"), zap.Error(err))
			return nil, err
		}
	}

	err = tx.Commit()
	if err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return newMem, nil
}

func scopeFromLevel(level models.MemoryLevel) (memory.Scope, error) {
	switch level {
	case models.MemoryLevelGlobal, models.MemoryLevelPersonality:
		return memory.ScopeUser, nil
	case models.MemoryLevelThread:
		return memory.ScopeChat, nil
	case models.MemoryLevelSummary:
		return memory.ScopeSummary, nil
	default:
		return "", fmt.Errorf("%w: invalid memory level: %s", ErrInvalidRequestBody, level)
	}
}

func validateMemoryType(t models.MemoryType) error {
	if t == "" || t == models.MemoryTypeContext {
		return nil
	}
	return fmt.Errorf("%w: invalid memory type: %s", ErrInvalidRequestBody, t)
}

func normalizeMemoryStatus(status models.MemoryStatus) memory.Status {
	if status == models.MemoryStatusInactive {
		return memory.StatusInactive
	}
	return memory.StatusActive
}

func (d *Datastore) chatOwnedByUser(ctx context.Context, tx *ent.Tx, userID uuid.UUID, chatID uuid.UUID) (bool, error) {
	if chatID == uuid.Nil {
		return false, nil
	}
	return tx.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
}

func (d *Datastore) personalityOwnedByUser(ctx context.Context, tx *ent.Tx, userID uuid.UUID, personalityID uuid.UUID) (bool, error) {
	if personalityID == uuid.Nil {
		return false, nil
	}
	return tx.Personality.Query().
		Where(
			entpersonality.ID(personalityID),
			entpersonality.HasUserWith(user.ID(userID)),
		).
		Exist(ctx)
}

func validateLevelInput(input models.CreateMemoryInput) error {
	switch input.Level {
	case models.MemoryLevelGlobal:
		if input.ChatID != nil && *input.ChatID != uuid.Nil {
			return fmt.Errorf("global memory cannot include chat_id")
		}
		if input.PinnedPersonalityID != nil && *input.PinnedPersonalityID != uuid.Nil {
			return fmt.Errorf("global memory cannot include pinned_personality_id")
		}
	case models.MemoryLevelPersonality:
		if input.ChatID != nil && *input.ChatID != uuid.Nil {
			return fmt.Errorf("personality memory cannot include chat_id")
		}
		if input.PinnedPersonalityID == nil || *input.PinnedPersonalityID == uuid.Nil {
			return fmt.Errorf("personality memory requires pinned_personality_id")
		}
	case models.MemoryLevelThread:
		if input.ChatID == nil || *input.ChatID == uuid.Nil {
			return fmt.Errorf("thread memory requires chat_id")
		}
		if input.PinnedPersonalityID != nil && *input.PinnedPersonalityID != uuid.Nil {
			return fmt.Errorf("thread memory cannot include pinned_personality_id")
		}
	case models.MemoryLevelSummary:
		if input.ChatID == nil || *input.ChatID == uuid.Nil {
			return fmt.Errorf("summary memory requires chat_id")
		}
		if input.PinnedPersonalityID != nil && *input.PinnedPersonalityID != uuid.Nil {
			return fmt.Errorf("summary memory cannot include pinned_personality_id")
		}
	default:
		return fmt.Errorf("%w: invalid memory level: %s", ErrInvalidRequestBody, input.Level)
	}
	return nil
}

func (d *Datastore) createMemoryFromLevelInput(ctx context.Context, tx *ent.Tx, userID uuid.UUID, input models.CreateMemoryInput) (*models.Memory, error) {
	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("%w: memory content is required", ErrInvalidRequestBody)
	}
	if err := validateLevelInput(input); err != nil {
		return nil, err
	}
	if err := validateMemoryType(input.Type); err != nil {
		return nil, err
	}

	memType := input.Type
	if memType == "" {
		memType = models.MemoryTypeContext
	}

	scope, err := scopeFromLevel(input.Level)
	if err != nil {
		return nil, err
	}

	if input.ChatID != nil && *input.ChatID != uuid.Nil {
		exists, err := d.chatOwnedByUser(ctx, tx, userID, *input.ChatID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrChatNotFound
		}
	}

	if input.PinnedPersonalityID != nil && *input.PinnedPersonalityID != uuid.Nil {
		exists, err := d.personalityOwnedByUser(ctx, tx, userID, *input.PinnedPersonalityID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrPersonalityNotFound
		}
	}

	create := tx.Memory.Create().
		SetContent(strings.TrimSpace(input.Content)).
		SetScope(scope).
		SetType(memory.Type(memType)).
		SetStatus(memory.StatusActive).
		SetConfidence(input.Confidence.Float()).
		SetStarred(input.Starred).
		SetOwnerID(userID).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now())

	if input.ChatID != nil && *input.ChatID != uuid.Nil {
		create.SetChatID(*input.ChatID)
	}
	if input.PinnedPersonalityID != nil && *input.PinnedPersonalityID != uuid.Nil {
		create.SetPinnedPersonalityID(*input.PinnedPersonalityID)
	}

	entMemory, err := create.Save(ctx)
	if err != nil {
		return nil, err
	}

	entMemory, err = tx.Memory.Query().
		Where(memory.ID(entMemory.ID)).
		WithChat().
		Only(ctx)
	if err != nil {
		return nil, err
	}

	return toMemoryModel(entMemory), nil
}

func (d *Datastore) CreateMemoryFromInput(ctx context.Context, userID uuid.UUID, input models.CreateMemoryInput) (*models.Memory, error) {
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

	mem, err := d.createMemoryFromLevelInput(ctx, tx, userID, input)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return mem, nil
}

func (d *Datastore) CreateMemoriesBatch(ctx context.Context, userID uuid.UUID, input models.BatchCreateMemoryInput) ([]*models.Memory, error) {
	if len(input.Items) == 0 {
		return []*models.Memory{}, nil
	}

	if input.AllOrNone {
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

		out := make([]*models.Memory, 0, len(input.Items))
		for _, item := range input.Items {
			mem, err := d.createMemoryFromLevelInput(ctx, tx, userID, item)
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			out = append(out, mem)
		}
		if err := tx.Commit(); err != nil {
			d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
			return nil, err
		}
		return out, nil
	}

	out := make([]*models.Memory, 0, len(input.Items))
	for _, item := range input.Items {
		mem, err := d.CreateMemoryFromInput(ctx, userID, item)
		if err != nil {
			d.logger.Warn("skipping invalid memory create item in partial batch", zap.Error(err))
			continue
		}
		out = append(out, mem)
	}
	return out, nil
}

func (d *Datastore) UpdateMemory(ctx context.Context, userID, memoryID uuid.UUID, patch models.MemoryPatch) (*models.Memory, error) {
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

	existing, err := tx.Memory.Query().
		Where(memory.ID(memoryID), memory.HasOwnerWith(user.ID(userID))).
		WithChat().
		Only(ctx)
	if err != nil {
		tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, ErrMemoryNotFound
		}
		return nil, err
	}

	nextContent := existing.Content
	if patch.Content != nil {
		nextContent = strings.TrimSpace(*patch.Content)
		if nextContent == "" {
			tx.Rollback()
			return nil, fmt.Errorf("%w: memory content is required", ErrInvalidRequestBody)
		}
	}

	nextType := models.MemoryType(existing.Type)
	if patch.Type != nil {
		if err := validateMemoryType(*patch.Type); err != nil {
			tx.Rollback()
			return nil, err
		}
		nextType = *patch.Type
	}

	nextStarred := existing.Starred
	if patch.Starred != nil {
		nextStarred = *patch.Starred
	}

	nextStatus := models.MemoryStatus(existing.Status)
	if patch.Status != nil {
		nextStatus = *patch.Status
	}

	nextConfidence := models.MemoryConfidenceFromFloat(existing.Confidence)
	if patch.Confidence != nil {
		nextConfidence = *patch.Confidence
	}

	currentLevel := memoryLevelForEntity(existing)
	nextLevel := currentLevel
	if patch.Level != nil {
		nextLevel = *patch.Level
	}

	var nextChatID *uuid.UUID
	switch {
	case patch.SetChatID:
		nextChatID = patch.ChatID
	case existing.Edges.Chat != nil:
		id := existing.Edges.Chat.ID
		nextChatID = &id
	}

	var nextPinnedID *uuid.UUID
	switch {
	case patch.SetPinnedPersonalityID:
		nextPinnedID = patch.PinnedPersonalityID
	case existing.PinnedPersonalityID != nil:
		id := *existing.PinnedPersonalityID
		nextPinnedID = &id
	}

	input := models.CreateMemoryInput{
		Content:             nextContent,
		Level:               nextLevel,
		ChatID:              nextChatID,
		PinnedPersonalityID: nextPinnedID,
		Type:                nextType,
		Starred:             nextStarred,
		Confidence:          nextConfidence,
	}
	if err := validateLevelInput(input); err != nil {
		tx.Rollback()
		return nil, err
	}

	scope, err := scopeFromLevel(nextLevel)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if nextChatID != nil && *nextChatID != uuid.Nil {
		exists, err := d.chatOwnedByUser(ctx, tx, userID, *nextChatID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		if !exists {
			tx.Rollback()
			return nil, ErrChatNotFound
		}
	}
	if nextPinnedID != nil && *nextPinnedID != uuid.Nil {
		exists, err := d.personalityOwnedByUser(ctx, tx, userID, *nextPinnedID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		if !exists {
			tx.Rollback()
			return nil, ErrPersonalityNotFound
		}
	}

	update := tx.Memory.UpdateOneID(memoryID).
		SetContent(nextContent).
		SetScope(scope).
		SetType(memory.Type(nextType)).
		SetStatus(normalizeMemoryStatus(nextStatus)).
		SetConfidence(nextConfidence.Float()).
		SetStarred(nextStarred).
		SetUpdatedAt(time.Now())

	if nextChatID != nil && *nextChatID != uuid.Nil {
		update.SetChatID(*nextChatID)
	} else {
		update.ClearChat()
	}
	if nextPinnedID != nil && *nextPinnedID != uuid.Nil {
		update.SetPinnedPersonalityID(*nextPinnedID)
	} else {
		update.ClearPinnedPersonalityID()
	}

	if _, err := update.Save(ctx); err != nil {
		tx.Rollback()
		return nil, err
	}

	reloaded, err := tx.Memory.Query().
		Where(memory.ID(memoryID)).
		WithChat().
		Only(ctx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toMemoryModel(reloaded), nil
}

// UpsertChatSummaryMemory creates or updates the singleton Summary memory for a chat.
// Summary memories are internal checkpoint state and are intentionally separate
// from user/tool-created Chat memories.
func (d *Datastore) UpsertChatSummaryMemory(ctx context.Context, userID, chatID uuid.UUID, summary string, embeddingVector []float32) error {
	if strings.TrimSpace(summary) == "" {
		return fmt.Errorf("summary memory content cannot be empty")
	}

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

	chatExists, err := tx.Chat.Query().
		Where(entchat.ID(chatID), entchat.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "chat"), zap.Error(err))
		tx.Rollback()
		return err
	}
	if !chatExists {
		tx.Rollback()
		return ErrChatNotFound
	}

	summaryMemory, err := tx.Memory.Query().
		Where(
			memory.ScopeEQ(memory.ScopeSummary),
			memory.HasOwnerWith(user.ID(userID)),
			memory.HasChatWith(entchat.ID(chatID)),
		).
		Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		d.logger.Error(i18n.T1("query.failed", "Entity", "summary memory"), zap.Error(err))
		tx.Rollback()
		return err
	}

	if ent.IsNotFound(err) {
		summaryMemory, err = tx.Memory.Create().
			SetContent(summary).
			SetScope(memory.ScopeSummary).
			SetStatus(memory.StatusActive).
			SetConfidence(0.9).
			SetOwnerID(userID).
			SetChatID(chatID).
			SetCreatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("create.failed", "Entity", "summary memory"), zap.Error(err))
			tx.Rollback()
			return err
		}
	} else {
		summaryMemory, err = tx.Memory.UpdateOneID(summaryMemory.ID).
			SetContent(summary).
			SetStatus(memory.StatusActive).
			SetConfidence(0.9).
			Save(ctx)
		if err != nil {
			d.logger.Error(i18n.T1("update.failed", "Entity", "summary memory"), zap.Error(err))
			tx.Rollback()
			return err
		}
	}

	existingEmbedding, err := tx.Embedding.Query().
		Where(embedding.HasMemoryWith(memory.ID(summaryMemory.ID))).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		d.logger.Error(i18n.T1("query.failed", "Entity", "summary embedding"), zap.Error(err))
		tx.Rollback()
		return err
	}

	embeddingValue := pgvector.NewVector(embeddingVector)
	if ent.IsNotFound(err) {
		_, err = tx.Embedding.Create().
			SetEmbedding(embeddingValue).
			SetMemoryID(summaryMemory.ID).
			Save(ctx)
	} else {
		_, err = tx.Embedding.UpdateOneID(existingEmbedding.ID).
			SetEmbedding(embeddingValue).
			Save(ctx)
	}
	if err != nil {
		d.logger.Error(i18n.T1("upsert.failed", "Entity", "summary embedding"), zap.Error(err))
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// GetChatSummaryMemory returns the singleton Summary memory for a chat, if present.
func (d *Datastore) GetChatSummaryMemory(ctx context.Context, userID, chatID uuid.UUID) (*models.Memory, error) {
	entMemory, err := d.dbClient.Memory.Query().
		Where(
			memory.ScopeEQ(memory.ScopeSummary),
			memory.StatusEQ(memory.StatusActive),
			memory.HasOwnerWith(user.ID(userID)),
			memory.HasChatWith(entchat.ID(chatID)),
		).
		WithChat().
		Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "summary memory"), zap.Error(err))
		return nil, err
	}

	return toMemoryModel(entMemory), nil
}

// ListChatsMissingSummaryMemory returns chats whose legacy checkpoint summary
// still needs an internal Summary memory.
func (d *Datastore) ListChatsMissingSummaryMemory(ctx context.Context, limit int) ([]models.ChatSummaryBackfillCandidate, error) {
	if limit <= 0 {
		limit = 100
	}

	chats, err := d.dbClient.Chat.Query().
		Where(
			entchat.CheckpointSummaryNotNil(),
			entchat.CheckpointSummaryNEQ(""),
			entchat.Not(entchat.HasMemoriesWith(memory.ScopeEQ(memory.ScopeSummary))),
		).
		WithOwner().
		Order(ent.Asc(entchat.FieldUpdatedAt), ent.Asc(entchat.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "summary backfill candidates"), zap.Error(err))
		return nil, err
	}

	candidates := make([]models.ChatSummaryBackfillCandidate, 0, len(chats))
	for _, chat := range chats {
		if chat.Edges.Owner == nil {
			d.logger.Warn(i18n.T1("chat.owner_missing", "ChatID", chat.ID.String()))
			continue
		}
		candidates = append(candidates, models.ChatSummaryBackfillCandidate{
			UserID:  chat.Edges.Owner.ID,
			ChatID:  chat.ID,
			Summary: chat.CheckpointSummary,
		})
	}

	return candidates, nil
}

// truncateString truncates a string to the specified length and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ListMemories returns a paginated list of memories for a user that match optional filter criteria
func (d *Datastore) ListMemories(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MemoryFilters) (*models.PaginatedResponse, error) {
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

	// Build query with user authorization (no eager-load edges here: WithChat() before
	// Count() can skew totals for User-scoped rows that have no chat edge).
	query := tx.Memory.Query().
		Where(
			memory.HasOwnerWith(
				user.ID(userID),
			),
		)

	if filters.Status != nil {
		query = query.Where(memory.StatusEQ(memory.Status(*filters.Status)))
	} else {
		query = query.Where(memory.StatusEQ(memory.StatusActive))
	}

	// Apply filters if provided
	if filters.ChatID != nil {
		query = query.Where(memory.HasChatWith(entchat.ID(*filters.ChatID)))
	}

	if filters.Level != nil && *filters.Level != "" {
		switch *filters.Level {
		case models.MemoryLevelGlobal:
			query = query.Where(
				memory.ScopeEQ(memory.ScopeUser),
				memory.PinnedPersonalityIDIsNil(),
			)
		case models.MemoryLevelPersonality:
			query = query.Where(
				memory.ScopeEQ(memory.ScopeUser),
				memory.PinnedPersonalityIDNotNil(),
			)
		case models.MemoryLevelThread:
			query = query.Where(memory.ScopeEQ(memory.ScopeChat))
		case models.MemoryLevelSummary:
			query = query.Where(memory.ScopeEQ(memory.ScopeSummary))
		default:
			return nil, fmt.Errorf("%w: invalid memory level filter: %s", ErrInvalidRequestBody, *filters.Level)
		}
	}

	if filters.Type != nil && *filters.Type != "" {
		query = query.Where(memory.TypeEQ(memory.Type(*filters.Type)))
	}

	if filters.Starred != nil {
		query = query.Where(memory.StarredEQ(*filters.Starred))
	}

	if filters.PinnedPersonalityID != nil {
		if *filters.PinnedPersonalityID == uuid.Nil {
			query = query.Where(memory.PinnedPersonalityIDIsNil())
		} else {
			query = query.Where(memory.PinnedPersonalityIDEQ(*filters.PinnedPersonalityID))
		}
	}
	if len(filters.PinnedPersonalityIDs) > 0 {
		query = query.Where(memory.PinnedPersonalityIDIn(filters.PinnedPersonalityIDs...))
	}
	if filters.GlobalOnly != nil && *filters.GlobalOnly {
		query = query.Where(memory.PinnedPersonalityIDIsNil())
	}

	if filters.Scope != nil && *filters.Scope != "" {
		query = query.Where(memory.ScopeEQ(memory.Scope(*filters.Scope)))
	}

	if filters.Query != nil && *filters.Query != "" {
		query = query.Where(memory.ContentContainsFold(*filters.Query))
	}

	if filters.MinDate != nil {
		query = query.Where(memory.CreatedAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(memory.CreatedAtLTE(*filters.MaxDate))
	}

	// Get total count (clone so Count does not inherit later modifiers).
	totalCount, err := query.Clone().Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "memories"), zap.Error(err))
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
		WithChat().
		Offset(offset).
		Limit(pageSize)

	switch {
	case filters.Sort == nil, *filters.Sort == models.MemorySortCreatedDesc:
		query = query.Order(ent.Desc(memory.FieldCreatedAt))
	case *filters.Sort == models.MemorySortCreatedAsc:
		query = query.Order(ent.Asc(memory.FieldCreatedAt))
	case *filters.Sort == models.MemorySortUpdatedDesc:
		query = query.Order(ent.Desc(memory.FieldUpdatedAt))
	default:
		query = query.Order(ent.Desc(memory.FieldCreatedAt))
	}

	// Execute query
	entMemories, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "memories"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	memoryModels := make([]any, len(entMemories))
	for i, entMemory := range entMemories {
		memoryModels[i] = toMemoryModel(entMemory)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    memoryModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// DeleteMemory deletes a memory with authorization check
func (d *Datastore) DeleteMemory(ctx context.Context, userID, id uuid.UUID) error {
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

	// Check if memory exists and belongs to the user
	exists, err := tx.Memory.Query().
		Where(
			memory.ID(id),
			memory.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "memory"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		d.logger.Error(i18n.T2("memory.not_found_or_unauthorized", "MemoryID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrMemoryNotFound
	}

	// Delete associatedembedding
	_, err = tx.Embedding.Delete().Where(embedding.HasMemoryWith(memory.ID(id))).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "embedding"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Delete memory
	err = tx.Memory.DeleteOneID(id).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "memory"), zap.Error(err))
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

// GetMemory retrieves a memory from the datastore by ID
func (d *Datastore) GetMemory(ctx context.Context, userID, id uuid.UUID) (*models.Memory, error) {
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

	// Query memory with authorization check
	entMemory, err := tx.Memory.Query().
		Where(
			memory.ID(id),
			memory.HasOwnerWith(
				user.ID(userID),
			),
			memory.StatusEQ(memory.StatusActive),
		).
		WithChat().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("memory.not_found_or_unauthorized", "MemoryID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrMemoryNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "memory"), zap.Error(err))
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

	return toMemoryModel(entMemory), nil
}

// GetMemoryByIDPrefix resolves an active memory owned by userID whose UUID text (dashes stripped)
// starts with prefix. It translates the prefix into a UUID primary-key range rather than formatting
// every ID with REPLACE(...), so PostgreSQL can use its UUID index. Returns the unique match,
// ErrMemoryNotFound when none, or an error when the prefix is ambiguous. Used by recall
// related/origin to accept short "memory:df3e519d" hop targets.
func (d *Datastore) GetMemoryByIDPrefix(ctx context.Context, userID uuid.UUID, prefix string) (*models.Memory, error) {
	prefix = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(prefix, "-", "")))
	lower, upper, err := memoryIDPrefixBounds(prefix)
	if err != nil {
		return nil, err
	}

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

	query := tx.Memory.Query().
		Where(
			memory.HasOwnerWith(user.ID(userID)),
			memory.StatusEQ(memory.StatusActive),
			memory.IDGTE(lower),
		)
	if upper != nil {
		query = query.Where(memory.IDLT(*upper))
	}
	ents, err := query.
		WithChat().
		Limit(2).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "memory"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if len(ents) == 0 {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrMemoryNotFound
	}
	if len(ents) > 1 {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, fmt.Errorf("memory ID prefix %q is ambiguous; pass the full UUID", prefix)
	}
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}
	return toMemoryModel(ents[0]), nil
}

// memoryIDPrefixBounds maps a lowercase, dashless UUID prefix to [lower, upper) UUID bounds.
// PostgreSQL orders UUIDs by their 16 bytes, which matches the fixed-width hexadecimal ordering
// used here. The minimum eight hex digits keeps accidental or overly broad prefix scans bounded.
func memoryIDPrefixBounds(prefix string) (lower uuid.UUID, upper *uuid.UUID, err error) {
	const (
		minMemoryIDPrefixLen = 8
		uuidHexLen           = 32
		hexDigits            = "0123456789abcdef"
	)
	if len(prefix) < minMemoryIDPrefixLen || len(prefix) > uuidHexLen {
		return uuid.Nil, nil, fmt.Errorf("memory ID prefix must contain %d-%d hex digits", minMemoryIDPrefixLen, uuidHexLen)
	}
	for _, r := range prefix {
		if !strings.ContainsRune(hexDigits, r) {
			return uuid.Nil, nil, fmt.Errorf("memory ID prefix contains non-hex character")
		}
	}

	lower, err = uuid.Parse(prefix + strings.Repeat("0", uuidHexLen-len(prefix)))
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("parse lower memory ID prefix bound: %w", err)
	}

	next := []byte(prefix)
	for i := len(next) - 1; i >= 0; i-- {
		digit := strings.IndexByte(hexDigits, next[i])
		if digit < len(hexDigits)-1 {
			next[i] = hexDigits[digit+1]
			next = append(next, strings.Repeat("0", uuidHexLen-len(next))...)
			upperBound, parseErr := uuid.Parse(string(next))
			if parseErr != nil {
				return uuid.Nil, nil, fmt.Errorf("parse upper memory ID prefix bound: %w", parseErr)
			}
			return lower, &upperBound, nil
		}
		next[i] = '0'
	}

	// A prefix made entirely of 'f' has no representable exclusive upper UUID bound.
	return lower, nil, nil
}

// UpdateMemoryPin updates the pinned personality for a User-scoped memory
// If pinnedPersonalityID is nil, the memory becomes unpinned (accessible by all personalities)
// If pinnedPersonalityID is set, the memory is pinned to that specific personality
func (d *Datastore) UpdateMemoryPin(ctx context.Context, userID, memoryID uuid.UUID, pinnedPersonalityID *uuid.UUID) (*models.Memory, error) {
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

	// Query memory with authorization check
	entMemory, err := tx.Memory.Query().
		Where(
			memory.ID(memoryID),
			memory.HasOwnerWith(
				user.ID(userID),
			),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("memory.not_found_or_unauthorized", "MemoryID", memoryID.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrMemoryNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "memory"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Validate that the memory is User-scoped (pinning only applies to User-scoped memories)
	if entMemory.Scope != memory.ScopeUser {
		d.logger.Error(i18n.T2("memory.pin_scope_invalid", "MemoryID", memoryID.String(), "Scope", string(entMemory.Scope)))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, fmt.Errorf("%s", i18n.T2("memory.pin_scope_invalid", "MemoryID", memoryID.String(), "Scope", string(entMemory.Scope)))
	}

	// If pinnedPersonalityID is provided, verify it belongs to the user
	if pinnedPersonalityID != nil && *pinnedPersonalityID != uuid.Nil {
		personalityExists, err := tx.Personality.Query().
			Where(
				entpersonality.ID(*pinnedPersonalityID),
				entpersonality.HasUserWith(
					user.ID(userID),
				),
			).
			Exist(ctx)

		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "personality"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}

		if !personalityExists {
			d.logger.Error(i18n.T2("memory.personality_not_found_or_unauthorized", "PersonalityID", pinnedPersonalityID.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, fmt.Errorf("%s", i18n.T2("memory.personality_not_found_or_unauthorized", "PersonalityID", pinnedPersonalityID.String(), "UserID", userID.String()))
		}
	}

	// Update the memory's pinned personality
	updateQuery := tx.Memory.UpdateOneID(memoryID)
	if pinnedPersonalityID != nil && *pinnedPersonalityID != uuid.Nil {
		updateQuery = updateQuery.SetPinnedPersonalityID(*pinnedPersonalityID)
	} else {
		updateQuery = updateQuery.ClearPinnedPersonalityID()
	}

	updatedMemory, err := updateQuery.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "memory pin"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the memory with chat relationship for model conversion
	updatedMemory, err = tx.Memory.Query().
		Where(memory.ID(memoryID)).
		WithChat().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("memory.with_chat_load_failed"), zap.Error(err))
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

	return toMemoryModel(updatedMemory), nil
}

type memoryImportCandidate struct {
	record              models.MemoryRecord
	scope               memory.Scope
	chatID              *uuid.UUID
	pinnedPersonalityID *uuid.UUID
}

type memoryImportPrepared struct {
	candidate memoryImportCandidate
	embedding []float32
}

// MemoryImportBatchEmbeddingFunc generates one embedding per input, in input order.
type MemoryImportBatchEmbeddingFunc func(context.Context, []string) ([][]float32, error)

// ImportMemories imports a ZIP produced by ExportMemories, deduping by memory ID.
func (d *Datastore) ImportMemories(ctx context.Context, userID uuid.UUID, zr *zip.Reader, createEmbedding func(context.Context, string) ([]float32, error)) (models.MemoryImportResult, error) {
	return d.importMemories(ctx, userID, zr, createEmbedding, nil, true)
}

// ImportMemoriesWithBatchEmbeddings imports a ZIP with batched embedding generation.
// The single-input callback remains available so callers without batch support retain
// the generic ImportMemories behavior.
func (d *Datastore) ImportMemoriesWithBatchEmbeddings(
	ctx context.Context,
	userID uuid.UUID,
	zr *zip.Reader,
	createEmbedding func(context.Context, string) ([]float32, error),
	createEmbeddings MemoryImportBatchEmbeddingFunc,
) (models.MemoryImportResult, error) {
	return d.importMemories(ctx, userID, zr, createEmbedding, createEmbeddings, true)
}

// importMemories implements ImportMemories. When writePackAudit is false, no audit_log
// rows are written for this ZIP (used by account backup import, which records memory
// totals in the account_backup audit entry).
func (d *Datastore) importMemories(
	ctx context.Context,
	userID uuid.UUID,
	zr *zip.Reader,
	createEmbedding func(context.Context, string) ([]float32, error),
	createEmbeddings MemoryImportBatchEmbeddingFunc,
	writePackAudit bool,
) (models.MemoryImportResult, error) {
	result := models.MemoryImportResult{}
	candidates := make([]memoryImportCandidate, 0)

	for _, zf := range zr.File {
		fileCandidates, invalidCount, invalidReasons, err := parseMemoryImportFile(zf, d.logger)
		if err != nil {
			if writePackAudit {
				d.auditMemoryPackImport(ctx, userID, result, err)
			}
			return result, err
		}
		result.InvalidRecordCount += invalidCount
		result.InvalidReasons.MalformedJSON += invalidReasons.MalformedJSON
		result.InvalidReasons.MissingID += invalidReasons.MissingID
		result.InvalidReasons.EmptyContent += invalidReasons.EmptyContent
		result.InvalidReasons.MissingCreatedAt += invalidReasons.MissingCreatedAt
		result.InvalidReasons.MissingChatID += invalidReasons.MissingChatID
		candidates = append(candidates, fileCandidates...)
	}
	d.logger.Info("memory import parsed archive",
		zap.String("user_id", userID.String()),
		zap.Int("candidate_count", len(candidates)),
		zap.Int("invalid_record_count", result.InvalidRecordCount),
		zap.Any("invalid_reasons", result.InvalidReasons))

	if len(candidates) == 0 {
		if writePackAudit {
			d.auditMemoryPackImport(ctx, userID, result, nil)
		}
		return result, nil
	}

	allIDs := make([]uuid.UUID, 0, len(candidates))
	seenIDs := make(map[uuid.UUID]struct{}, len(candidates))
	for _, c := range candidates {
		if _, exists := seenIDs[c.record.ID]; exists {
			result.DuplicateCount++
			continue
		}
		seenIDs[c.record.ID] = struct{}{}
		allIDs = append(allIDs, c.record.ID)
	}
	existingIDs, err := d.existingAnyMemoryIDs(ctx, allIDs)
	if err != nil {
		if writePackAudit {
			d.auditMemoryPackImport(ctx, userID, result, err)
		}
		return result, err
	}

	chatIDs := make([]uuid.UUID, 0)
	personalityIDs := make([]uuid.UUID, 0)
	for _, c := range candidates {
		if c.chatID != nil {
			chatIDs = append(chatIDs, *c.chatID)
		}
		if c.pinnedPersonalityID != nil {
			personalityIDs = append(personalityIDs, *c.pinnedPersonalityID)
		}
	}

	existingChats, err := d.existingChatIDs(ctx, userID, chatIDs)
	if err != nil {
		if writePackAudit {
			d.auditMemoryPackImport(ctx, userID, result, err)
		}
		return result, err
	}
	existingPersonalities, err := d.existingPersonalityIDs(ctx, userID, personalityIDs)
	if err != nil {
		if writePackAudit {
			d.auditMemoryPackImport(ctx, userID, result, err)
		}
		return result, err
	}

	toPrepare := make([]memoryImportCandidate, 0, len(candidates))
	queuedIDs := make(map[uuid.UUID]struct{}, len(candidates))
	for _, c := range candidates {
		if _, exists := queuedIDs[c.record.ID]; exists {
			continue
		}
		queuedIDs[c.record.ID] = struct{}{}
		if _, exists := existingIDs[c.record.ID]; exists {
			result.DuplicateCount++
			continue
		}
		if c.chatID != nil {
			if _, exists := existingChats[*c.chatID]; !exists {
				result.SkippedMissingChat++
				continue
			}
		}
		if c.pinnedPersonalityID != nil {
			if _, exists := existingPersonalities[*c.pinnedPersonalityID]; !exists {
				result.SkippedMissingPersonality++
				continue
			}
		}
		toPrepare = append(toPrepare, c)
	}
	d.logger.Info("memory import filtered candidates",
		zap.String("user_id", userID.String()),
		zap.Int("ready_for_embedding_count", len(toPrepare)),
		zap.Int("duplicate_count", result.DuplicateCount),
		zap.Int("skipped_missing_chat_count", result.SkippedMissingChat),
		zap.Int("skipped_missing_personality_count", result.SkippedMissingPersonality))

	total := len(toPrepare)
	processed := 0
	for start := 0; start < total; start += memoryImportBatchSize {
		end := start + memoryImportBatchSize
		if end > total {
			end = total
		}
		chunk := toPrepare[start:end]

		var prepared []memoryImportPrepared
		if createEmbeddings != nil {
			prepared, err = buildImportEmbeddingsBatch(ctx, chunk, createEmbeddings)
		} else {
			prepared, err = buildImportEmbeddingsChunk(ctx, chunk, createEmbedding)
		}
		if err != nil {
			if writePackAudit {
				d.auditMemoryPackImport(ctx, userID, result, err)
			}
			return result, err
		}
		processed = end
		d.logger.Info("memory import embedding progress",
			zap.String("user_id", userID.String()),
			zap.Int("embedded", processed),
			zap.Int("total", total))

		imported, duplicates, err := d.importPreparedMemories(ctx, userID, prepared)
		if err != nil {
			if writePackAudit {
				d.auditMemoryPackImport(ctx, userID, result, err)
			}
			return result, err
		}
		result.ImportedCount += imported
		result.DuplicateCount += duplicates

		d.logger.Info("memory import persist progress",
			zap.String("user_id", userID.String()),
			zap.Int("processed", processed),
			zap.Int("total", total),
			zap.Int("chunk_size", len(prepared)),
			zap.Int("imported_count", result.ImportedCount),
			zap.Int("duplicate_count", result.DuplicateCount))
	}

	if writePackAudit {
		d.auditMemoryPackImport(ctx, userID, result, nil)
	}
	return result, nil
}

func (d *Datastore) importPreparedMemories(ctx context.Context, userID uuid.UUID, prepared []memoryImportPrepared) (imported int, duplicates int, err error) {
	if len(prepared) == 0 {
		return 0, 0, nil
	}

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	ids := make([]uuid.UUID, 0, len(prepared))
	for _, p := range prepared {
		ids = append(ids, p.candidate.record.ID)
	}
	existing, err := tx.Memory.Query().
		Where(memory.IDIn(ids...)).
		Select(memory.FieldID).
		All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, 0, err
	}
	existingIDs := make(map[uuid.UUID]struct{}, len(existing))
	for _, mem := range existing {
		existingIDs[mem.ID] = struct{}{}
	}

	toInsert := make([]memoryImportPrepared, 0, len(prepared))
	for _, p := range prepared {
		if _, exists := existingIDs[p.candidate.record.ID]; exists {
			duplicates++
			continue
		}
		toInsert = append(toInsert, p)
	}
	if len(toInsert) == 0 {
		_ = tx.Rollback()
		return 0, duplicates, nil
	}

	memoryCreates := make([]*ent.MemoryCreate, 0, len(toInsert))
	for _, p := range toInsert {
		create := tx.Memory.Create().
			SetID(p.candidate.record.ID).
			SetContent(p.candidate.record.Content).
			SetScope(p.candidate.scope).
			SetStatus(memory.StatusActive).
			SetConfidence(models.DefaultMemoryConfidence).
			SetOwnerID(userID).
			SetCreatedAt(p.candidate.record.CreatedAt)
		if p.candidate.chatID != nil {
			create.SetChatID(*p.candidate.chatID)
		}
		if p.candidate.pinnedPersonalityID != nil {
			create.SetPinnedPersonalityID(*p.candidate.pinnedPersonalityID)
		}
		memoryCreates = append(memoryCreates, create)
	}
	if err := tx.Memory.CreateBulk(memoryCreates...).
		OnConflictColumns(memory.FieldID).
		DoNothing().
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return 0, 0, err
	}

	embeddingCreates := make([]*ent.EmbeddingCreate, 0, len(toInsert))
	for _, p := range toInsert {
		embeddingCreates = append(embeddingCreates, tx.Embedding.Create().
			// Some long-lived deployments predate the unique embedding_memory constraint.
			// Use the deterministic imported memory ID as the embedding primary key so retries
			// remain conflict-safe without requiring that legacy constraint.
			SetID(p.candidate.record.ID).
			SetEmbedding(pgvector.NewVector(p.embedding)).
			SetMemoryID(p.candidate.record.ID))
	}
	if err := tx.Embedding.CreateBulk(embeddingCreates...).
		OnConflictColumns(embedding.FieldID).
		DoNothing().
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return len(toInsert), duplicates, nil
}

func buildImportEmbeddingsBatch(
	ctx context.Context,
	chunk []memoryImportCandidate,
	createEmbeddings MemoryImportBatchEmbeddingFunc,
) ([]memoryImportPrepared, error) {
	inputs := make([]string, len(chunk))
	for i, candidate := range chunk {
		inputs[i] = candidate.record.Content
	}
	embeddings, err := createEmbeddings(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if len(embeddings) != len(chunk) {
		return nil, fmt.Errorf("create embeddings: got %d vectors for %d memories", len(embeddings), len(chunk))
	}

	prepared := make([]memoryImportPrepared, len(chunk))
	for i, candidate := range chunk {
		prepared[i] = memoryImportPrepared{
			candidate: candidate,
			embedding: embeddings[i],
		}
	}
	return prepared, nil
}

func buildImportEmbeddingsChunk(
	ctx context.Context,
	chunk []memoryImportCandidate,
	createEmbedding func(context.Context, string) ([]float32, error),
) ([]memoryImportPrepared, error) {
	prepared := make([]memoryImportPrepared, len(chunk))
	type embedJob struct {
		idx       int
		candidate memoryImportCandidate
	}
	type embedResult struct {
		idx       int
		embedding []float32
		err       error
	}

	jobs := make(chan embedJob)
	results := make(chan embedResult, len(chunk))

	workerCount := memoryImportEmbeddingWorkers
	if workerCount > len(chunk) {
		workerCount = len(chunk)
	}
	if workerCount < 1 {
		return prepared, nil
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for job := range jobs {
				embedding, err := createEmbedding(workerCtx, job.candidate.record.Content)
				results <- embedResult{
					idx:       job.idx,
					embedding: embedding,
					err:       err,
				}
				if err != nil {
					cancel()
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		defer close(jobs)
		for i, candidate := range chunk {
			select {
			case <-workerCtx.Done():
				return
			case jobs <- embedJob{idx: i, candidate: candidate}:
			}
		}
	}()

	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("create embedding for memory %s: %w", chunk[res.idx].record.ID, res.err)
		}
		prepared[res.idx] = memoryImportPrepared{
			candidate: chunk[res.idx],
			embedding: res.embedding,
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return prepared, nil
}

func parseMemoryImportFile(zf *zip.File, logger *zap.Logger) ([]memoryImportCandidate, int, models.MemoryImportInvalidReasons, error) {
	name := filepath.Base(zf.Name)
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		return nil, 0, models.MemoryImportInvalidReasons{}, nil
	}

	var scope memory.Scope
	var pinnedPersonalityID *uuid.UUID
	switch {
	case name == "chat.json":
		scope = memory.ScopeChat
	case name == "user.json":
		scope = memory.ScopeUser
	case strings.HasPrefix(name, "personality-") && strings.HasSuffix(name, ".json"):
		scope = memory.ScopeUser
		rawID := strings.TrimSuffix(strings.TrimPrefix(name, "personality-"), ".json")
		pid, err := uuid.Parse(rawID)
		if err != nil {
			return nil, 0, models.MemoryImportInvalidReasons{}, fmt.Errorf("invalid personality file %q: %w", name, err)
		}
		pinnedPersonalityID = &pid
	default:
		return nil, 0, models.MemoryImportInvalidReasons{}, nil
	}

	rc, err := zf.Open()
	if err != nil {
		return nil, 0, models.MemoryImportInvalidReasons{}, fmt.Errorf("open zip entry %q: %w", zf.Name, err)
	}
	defer rc.Close()

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 64*1024), memoryImportMaxJSONLine)
	records := make([]memoryImportCandidate, 0)
	invalidCount := 0
	invalidReasons := models.MemoryImportInvalidReasons{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var rec models.MemoryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			invalidCount++
			invalidReasons.MalformedJSON++
			logInvalidMemoryImportRecord(logger, zf.Name, lineNumber, "malformed_json", uuid.Nil)
			continue
		}
		if rec.ID == uuid.Nil {
			invalidCount++
			invalidReasons.MissingID++
			logInvalidMemoryImportRecord(logger, zf.Name, lineNumber, "missing_id", uuid.Nil)
			continue
		}
		if strings.TrimSpace(rec.Content) == "" {
			invalidCount++
			invalidReasons.EmptyContent++
			logInvalidMemoryImportRecord(logger, zf.Name, lineNumber, "empty_content", rec.ID)
			continue
		}
		if rec.CreatedAt.IsZero() {
			invalidCount++
			invalidReasons.MissingCreatedAt++
			logInvalidMemoryImportRecord(logger, zf.Name, lineNumber, "missing_created_at", rec.ID)
			continue
		}
		if scope == memory.ScopeChat && rec.ChatID == nil {
			invalidCount++
			invalidReasons.MissingChatID++
			logInvalidMemoryImportRecord(logger, zf.Name, lineNumber, "missing_chat_id", rec.ID)
			continue
		}

		content := strings.TrimSpace(rec.Content)
		rec.Content = content
		candidate := memoryImportCandidate{
			record:              rec,
			scope:               scope,
			pinnedPersonalityID: pinnedPersonalityID,
		}
		if scope == memory.ScopeChat {
			candidate.chatID = rec.ChatID
		}
		records = append(records, candidate)
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, invalidCount, invalidReasons, fmt.Errorf("import line exceeds %d MiB in %q", memoryImportMaxJSONLine>>20, zf.Name)
		}
		return nil, invalidCount, invalidReasons, fmt.Errorf("scan zip entry %q: %w", zf.Name, err)
	}

	return records, invalidCount, invalidReasons, nil
}

func logInvalidMemoryImportRecord(logger *zap.Logger, entry string, line int, reason string, memoryID uuid.UUID) {
	if logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("entry", entry),
		zap.Int("line", line),
		zap.String("reason", reason),
	}
	if memoryID != uuid.Nil {
		fields = append(fields, zap.String("memory_id", memoryID.String()))
	}
	logger.Warn("memory import skipped invalid record", fields...)
}

func (d *Datastore) existingAnyMemoryIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	set := make(map[uuid.UUID]struct{})
	if len(ids) == 0 {
		return set, nil
	}

	existing, err := d.dbClient.Memory.Query().
		Where(memory.IDIn(ids...)).
		Select(memory.FieldID).
		All(ctx)
	if err != nil {
		return nil, err
	}

	for _, m := range existing {
		set[m.ID] = struct{}{}
	}

	return set, nil
}

func (d *Datastore) existingChatIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	set := make(map[uuid.UUID]struct{})
	if len(ids) == 0 {
		return set, nil
	}
	chats, err := d.dbClient.Chat.Query().
		Where(
			entchat.IDIn(ids...),
			entchat.HasOwnerWith(user.ID(userID)),
		).
		Select(entchat.FieldID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range chats {
		set[c.ID] = struct{}{}
	}
	return set, nil
}

func (d *Datastore) existingPersonalityIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	set := make(map[uuid.UUID]struct{})
	if len(ids) == 0 {
		return set, nil
	}
	ps, err := d.dbClient.Personality.Query().
		Where(
			entpersonality.IDIn(ids...),
			entpersonality.HasUserWith(user.ID(userID)),
		).
		Select(entpersonality.FieldID).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		set[p.ID] = struct{}{}
	}
	return set, nil
}

// streamMemorySection creates a named file in zw and encodes memories as JSONL,
// fetching in cursor-based batches via fetch until fewer than batchSize rows are
// returned. Each record is encoded immediately — nothing accumulates in memory.
//
// fetch receives the cursor from the previous batch (zero values on the first
// call) and returns the next batch. It is the caller's responsibility to apply
// the cursor predicate and ORDER/LIMIT clauses inside fetch.
//
// Extracting this helper makes the pagination+encoding logic unit-testable
// without a database by passing a hand-crafted fetch stub.
func streamMemorySection(
	ctx context.Context,
	zw *zip.Writer,
	filename string,
	batchSize int,
	fetch func(ctx context.Context, lastCreatedAt time.Time, lastID uuid.UUID) ([]*ent.Memory, error),
) error {
	f, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("creating %s in zip: %w", filename, err)
	}
	enc := json.NewEncoder(f)
	var lastCreatedAt time.Time
	var lastID uuid.UUID
	for {
		batch, err := fetch(ctx, lastCreatedAt, lastID)
		if err != nil {
			return fmt.Errorf("querying batch: %w", err)
		}
		for _, m := range batch {
			if err := enc.Encode(toMemoryRecord(m)); err != nil {
				return fmt.Errorf("encoding record: %w", err)
			}
		}
		if len(batch) < batchSize {
			break
		}
		last := batch[len(batch)-1]
		lastCreatedAt = last.CreatedAt
		lastID = last.ID
	}
	return nil
}

// ExportMemories streams all memories for a user as a ZIP archive to w.
//
// The ZIP contains one JSONL file per memory category:
//   - chat.json: chat-scoped memories
//   - user.json: user-scoped unpinned memories
//   - personality-{uuid}.json: one file per personality with pinned memories
//
// Records are encoded directly into each ZIP entry as batches arrive from the
// DB — nothing is buffered in memory beyond a single batch of batchSize rows.
func (d *Datastore) ExportMemories(ctx context.Context, userID uuid.UUID, w io.Writer) (err error) {
	const batchSize = 100

	subject := userID
	defer func() {
		if err != nil {
			d.writeAuditLog(ctx, auditEntry{
				Category:      auditCategoryMemoryPack,
				Action:        "export",
				Message:       fmt.Sprintf("user memory ZIP export failed for user %s", userID),
				SubjectUserID: &subject,
				Metadata:      map[string]any{"ok": false, "error": err.Error()},
			})
			return
		}
		d.writeAuditLog(ctx, auditEntry{
			Category:      auditCategoryMemoryPack,
			Action:        "export",
			Message:       fmt.Sprintf("user memory ZIP export completed for user %s", userID),
			SubjectUserID: &subject,
			Metadata:      map[string]any{"ok": true},
		})
	}()

	zw := zip.NewWriter(w)
	defer func() {
		if closeErr := zw.Close(); closeErr != nil {
			d.logger.Error(i18n.T("memory.zip_close_failed"), zap.Error(closeErr))
		}
	}()

	// --- chat.json ---
	if err = streamMemorySection(ctx, zw, "chat.json", batchSize, func(ctx context.Context, lastCreatedAt time.Time, lastID uuid.UUID) ([]*ent.Memory, error) {
		return memoryCursorPredicate(lastCreatedAt, lastID)(
			d.dbClient.Memory.Query().
				Where(
					memory.HasOwnerWith(user.ID(userID)),
					memory.ScopeEQ(memory.ScopeChat),
					memory.StatusEQ(memory.StatusActive),
				).
				WithChat().
				Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
				Limit(batchSize),
		).All(ctx)
	}); err != nil {
		d.logger.Error(i18n.T("memory.chat_stream_failed"), zap.Error(err))
		return err
	}

	// --- user.json ---
	if err = streamMemorySection(ctx, zw, "user.json", batchSize, func(ctx context.Context, lastCreatedAt time.Time, lastID uuid.UUID) ([]*ent.Memory, error) {
		return memoryCursorPredicate(lastCreatedAt, lastID)(
			d.dbClient.Memory.Query().
				Where(
					memory.HasOwnerWith(user.ID(userID)),
					memory.ScopeEQ(memory.ScopeUser),
					memory.StatusEQ(memory.StatusActive),
					memory.PinnedPersonalityIDIsNil(),
				).
				Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
				Limit(batchSize),
		).All(ctx)
	}); err != nil {
		d.logger.Error(i18n.T("memory.user_stream_failed"), zap.Error(err))
		return err
	}

	// --- personality-{id}.json files ---
	//
	// Discover the distinct personalities that have pinned memories, then stream
	// each personality's memories into its own file. GroupBy on a single field
	// with no aggregation is the idiomatic Ent way to get a deduplicated column.
	var pinnedGroups []struct {
		PinnedPersonalityID uuid.UUID `json:"pinned_personality_id"`
	}
	if err = d.dbClient.Memory.Query().
		Where(
			memory.HasOwnerWith(user.ID(userID)),
			memory.ScopeEQ(memory.ScopeUser),
			memory.StatusEQ(memory.StatusActive),
			memory.PinnedPersonalityIDNotNil(),
		).
		GroupBy(memory.FieldPinnedPersonalityID).
		Aggregate().
		Scan(ctx, &pinnedGroups); err != nil {
		d.logger.Error(i18n.T("memory.pinned_ids_query_failed"), zap.Error(err))
		return err
	}
	pinnedIDs := make([]uuid.UUID, 0, len(pinnedGroups))
	for _, g := range pinnedGroups {
		pinnedIDs = append(pinnedIDs, g.PinnedPersonalityID)
	}
	// Sort for deterministic file ordering across runs.
	sort.Slice(pinnedIDs, func(i, j int) bool {
		return pinnedIDs[i].String() < pinnedIDs[j].String()
	})

	for _, pID := range pinnedIDs {
		filename := fmt.Sprintf("personality-%s.json", pID)
		if err = streamMemorySection(ctx, zw, filename, batchSize, func(ctx context.Context, lastCreatedAt time.Time, lastID uuid.UUID) ([]*ent.Memory, error) {
			return memoryCursorPredicate(lastCreatedAt, lastID)(
				d.dbClient.Memory.Query().
					Where(
						memory.HasOwnerWith(user.ID(userID)),
						memory.ScopeEQ(memory.ScopeUser),
						memory.StatusEQ(memory.StatusActive),
						memory.PinnedPersonalityIDNotNil(),
						memory.HasPinnedPersonalityWith(entpersonality.ID(pID)),
					).
					Order(ent.Asc(memory.FieldCreatedAt), ent.Asc(memory.FieldID)).
					Limit(batchSize),
			).All(ctx)
		}); err != nil {
			d.logger.Error(i18n.T1("memory.pinned_stream_failed", "PersonalityID", pID.String()), zap.Error(err))
			return err
		}
	}

	return nil
}

// toMemoryRecord converts an ent.Memory to a models.MemoryRecord.
// The Chat edge must be preloaded (WithChat) for ChatID and ChatName to populate.
func toMemoryRecord(m *ent.Memory) models.MemoryRecord {
	rec := models.MemoryRecord{
		ID:        m.ID,
		Content:   m.Content,
		CreatedAt: m.CreatedAt,
	}

	if m.Scope == memory.ScopeChat && m.Edges.Chat != nil {
		chatID := m.Edges.Chat.ID
		chatName := m.Edges.Chat.Name
		rec.ChatID = &chatID
		rec.ChatName = &chatName
	}

	return rec
}

func (d *Datastore) GetRelatedMemories(ctx context.Context, userId, chatId uuid.UUID, queryEmbedding []float32, activePersonalityID uuid.UUID) ([]*models.Memory, error) {

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

	embVec := pgvector.NewVector(queryEmbedding)
	// Format vector as PostgreSQL array literal to avoid parameter binding issues
	vectorStr := embVec.String()

	// Chat-scoped memories
	chatScopedPredicate := memory.And(
		memory.ScopeEQ(memory.ScopeChat),
		memory.HasChatWith(entchat.ID(chatId)),
	)

	// User-scoped memories with pinning logic
	var userScopedPredicate func(*sql.Selector)
	if activePersonalityID != uuid.Nil {
		// Include unpinned OR pinned to active personality
		userScopedPredicate = memory.And(
			memory.ScopeEQ(memory.ScopeUser),
			memory.Or(
				memory.PinnedPersonalityIDIsNil(),
				memory.PinnedPersonalityIDEQ(activePersonalityID),
			),
		)
	} else {
		// include unpinned User-scoped memories
		userScopedPredicate = memory.And(
			memory.ScopeEQ(memory.ScopeUser),
			memory.PinnedPersonalityIDIsNil(),
		)
	}

	dbEmbeddings, err := tx.Embedding.Query().
		Where(
			embedding.HasMemoryWith(
				memory.HasOwnerWith(user.ID(userId)),
				memory.StatusEQ(memory.StatusActive),
				memory.Or(
					chatScopedPredicate,
					userScopedPredicate,
				),
			),
		).
		Where(func(s *sql.Selector) {
			// Use string formatting to embed vector and threshold directly in SQL
			s.Where(sql.ExprP(fmt.Sprintf("embedding <-> '%s' <= %f", vectorStr, MemoryRelevanceThreshold)))
		}).
		Order(func(s *sql.Selector) {
			// Use string formatting to embed vector directly in SQL
			s.OrderExpr(sql.Expr(fmt.Sprintf("embedding <-> '%s'", vectorStr)))
		}).
		Limit(5).
		WithMemory(func(q *ent.MemoryQuery) {
			q.WithChat()
		}).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "embeddings"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	// Use Memory entities directly from embeddings to preserve similarity ordering, and attach the
	// retrieval relevance (cosine similarity) computed from the already-loaded vectors so the agent
	// can see how strong each match was.
	memories := make([]*models.Memory, len(dbEmbeddings))
	for i, dbEmbedding := range dbEmbeddings {
		m := toMemoryModel(dbEmbedding.Edges.Memory)
		if rel, ok := embeddingCosineRelevance(queryEmbedding, dbEmbedding.Embedding); ok {
			m.Relevance = &rel
		}
		memories[i] = m
	}

	// Retrieved memory logging removed

	return memories, nil
}

// GetRelatedSummaryMemories returns Summary-scope memories (per-chat checkpoint summaries) ranked
// by vector distance to queryEmbedding. GetRelatedMemories intentionally excludes Summary-scope
// rows (they are internal checkpoint state, not facts); this is the counterpart recall's
// source_type=summaries search uses instead. limit is clamped to [1, 20], defaulting to 5.
func (d *Datastore) GetRelatedSummaryMemories(ctx context.Context, userID uuid.UUID, queryEmbedding []float32, limit int) ([]*models.Memory, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	embVec := pgvector.NewVector(queryEmbedding)
	// Format vector as PostgreSQL array literal to avoid parameter binding issues
	vectorStr := embVec.String()

	dbEmbeddings, err := d.dbClient.Embedding.Query().
		Where(
			embedding.HasMemoryWith(
				memory.HasOwnerWith(user.ID(userID)),
				memory.StatusEQ(memory.StatusActive),
				memory.ScopeEQ(memory.ScopeSummary),
			),
		).
		Where(func(s *sql.Selector) {
			// Use string formatting to embed vector and threshold directly in SQL
			s.Where(sql.ExprP(fmt.Sprintf("embedding <-> '%s' <= %f", vectorStr, MemoryRelevanceThreshold)))
		}).
		Order(func(s *sql.Selector) {
			// Use string formatting to embed vector directly in SQL
			s.OrderExpr(sql.Expr(fmt.Sprintf("embedding <-> '%s'", vectorStr)))
		}).
		Limit(limit).
		WithMemory(func(q *ent.MemoryQuery) {
			q.WithChat()
		}).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "summary embeddings"), zap.Error(err))
		return nil, err
	}

	memories := make([]*models.Memory, len(dbEmbeddings))
	for i, dbEmbedding := range dbEmbeddings {
		m := toMemoryModel(dbEmbedding.Edges.Memory)
		if rel, ok := embeddingCosineRelevance(queryEmbedding, dbEmbedding.Embedding); ok {
			m.Relevance = &rel
		}
		memories[i] = m
	}

	return memories, nil
}

// embeddingCosineRelevance returns the cosine similarity in [0,1] between the query embedding and a
// stored memory embedding — the retrieval relevance surfaced to the agent. Cosine is used directly
// (rather than derived from the L2 <-> distance the query filters on) so it is robust regardless of
// whether the embeddings are unit-normalized. Returns ok=false when a vector is absent or the
// lengths mismatch.
func embeddingCosineRelevance(query []float32, stored pgvector.Vector) (float64, bool) {
	s := stored.Slice()
	if len(query) == 0 || len(s) != len(query) {
		return 0, false
	}
	var dot, nq, ns float64
	for i := range query {
		q, v := float64(query[i]), float64(s[i])
		dot += q * v
		nq += q * q
		ns += v * v
	}
	if nq == 0 || ns == 0 {
		return 0, false
	}
	sim := dot / (math.Sqrt(nq) * math.Sqrt(ns))
	if sim < 0 {
		sim = 0
	}
	if sim > 1 {
		sim = 1
	}
	return sim, true
}
