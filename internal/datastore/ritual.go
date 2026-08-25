package datastore

import (
	"context"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Convert from Ent ritual to model
func toRitualModel(e *ent.Ritual) *models.Ritual {
	if e == nil {
		return nil
	}

	ritualModel := models.Ritual{
		ID:          e.ID,
		Name:        e.Name,
		Description: e.Description,
		Content:     e.Content,
		Hotkeys:     e.Hotkeys,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
	if len(e.Edges.McpServers) > 0 {
		ids := make([]uuid.UUID, len(e.Edges.McpServers))
		for i, server := range e.Edges.McpServers {
			ids[i] = server.ID
		}
		ritualModel.MCPServerIDs = &ids
	}

	if e.Edges.Personality != nil {
		ritualModel.PersonalityID = e.Edges.Personality.ID
	}

	return &ritualModel
}

// CreateRitual persists a new ritual to the datastore
func (d *Datastore) CreateRitual(ctx context.Context, userID uuid.UUID, ritual models.Ritual) (*models.Ritual, error) {
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

	// Create ritual
	create := tx.Ritual.Create().
		SetName(ritual.Name).
		SetDescription(ritual.Description).
		SetContent(ritual.Content).
		SetHotkeys(ritual.Hotkeys).
		SetOwnerID(userID)
	if ritual.MCPServerIDs != nil && len(*ritual.MCPServerIDs) > 0 {
		create.AddMcpServerIDs((*ritual.MCPServerIDs)...)
	}

	if ritual.PersonalityID != uuid.Nil {
		create.SetPersonalityID(ritual.PersonalityID)
	}

	entRitual, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load relationships needed for model conversion
	entRitual, err = tx.Ritual.Query().
		Where(entritual.ID(entRitual.ID)).
		WithOwner().
		WithPersonality().
		WithMcpServers().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("ritual.relationships_load_failed"), zap.Error(err))
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

	return toRitualModel(entRitual), nil
}

// GetRitual retrieves a ritual from the datastore by ID
func (d *Datastore) GetRitual(ctx context.Context, userID, id uuid.UUID) (*models.Ritual, error) {
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

	// Query ritual with authorization check
	entRitual, err := tx.Ritual.Query().
		Where(
			entritual.ID(id),
			entritual.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithPersonality().
		WithMcpServers().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("ritual.not_found_or_unauthorized", "RitualID", id.String(), "UserID", userID.String()))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRitualNotFound
		}

		d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
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

	return toRitualModel(entRitual), nil
}

// GetRitualsByIDs retrieves multiple rituals by their IDs for a specific user
// This method ensures the user owns all requested rituals for security
func (d *Datastore) GetRitualsByIDs(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID) ([]*models.Ritual, error) {
	// Early return for empty list
	if len(ritualIDs) == 0 {
		return []*models.Ritual{}, nil
	}

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

	// Query rituals with authorization check - only get rituals owned by the user
	entRituals, err := tx.Ritual.Query().
		Where(
			entritual.IDIn(ritualIDs...),
			entritual.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithPersonality().
		WithMcpServers().
		All(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "rituals by IDs"), zap.Error(err))
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

	// Convert to model types
	ritualModels := make([]*models.Ritual, len(entRituals))
	for i, entRitual := range entRituals {
		ritualModels[i] = toRitualModel(entRitual)
	}

	// Log if we didn't find all requested rituals (could indicate unauthorized access attempt or missing rituals)
	if len(ritualModels) != len(ritualIDs) {
		d.logger.Warn(i18n.T3("ritual.not_all_found", "UserID", userID.String(), "RequestedCount", len(ritualIDs), "FoundCount", len(ritualModels)))
	}

	return ritualModels, nil
}

// ListRituals returns a paginated list of rituals for a user that match optional filter criteria
func (d *Datastore) ListRituals(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.RitualFilters) (*models.PaginatedResponse, error) {
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
	query := tx.Ritual.Query().
		Where(
			entritual.HasOwnerWith(
				user.ID(userID),
			),
		).
		WithOwner().
		WithPersonality().
		WithMcpServers()

	// Apply filters if provided
	if filters.Name != nil && *filters.Name != "" {
		query = query.Where(entritual.NameContainsFold(*filters.Name))
	}

	if filters.Query != nil && *filters.Query != "" {
		query = query.Where(
			entritual.Or(
				entritual.NameContainsFold(*filters.Query),
				entritual.DescriptionContainsFold(*filters.Query),
				entritual.ContentContainsFold(*filters.Query),
			),
		)
	}

	if filters.PersonalityID != nil {
		query = query.Where(entritual.HasPersonalityWith(personality.ID(*filters.PersonalityID)))
	}

	if len(filters.PersonalityIDs) > 0 {
		query = query.Where(entritual.HasPersonalityWith(personality.IDIn(filters.PersonalityIDs...)))
	}

	if filters.GlobalOnly != nil && *filters.GlobalOnly {
		query = query.Where(entritual.Not(entritual.HasPersonality()))
	}

	if filters.MinDate != nil {
		query = query.Where(entritual.CreatedAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(entritual.CreatedAtLTE(*filters.MaxDate))
	}

	if filters.HasHotkeys != nil {
		if *filters.HasHotkeys {
			query = query.Where(entritual.HotkeysNEQ(""))
		} else {
			query = query.Where(entritual.HotkeysEQ(""))
		}
	}

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "rituals"), zap.Error(err))
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
		Limit(pageSize)

	switch {
	case filters.Sort == nil, *filters.Sort == models.RitualSortCreatedDesc:
		query = query.Order(ent.Desc(entritual.FieldCreatedAt))
	case *filters.Sort == models.RitualSortNameAsc:
		query = query.Order(ent.Asc(entritual.FieldName))
	case *filters.Sort == models.RitualSortUpdatedDesc:
		query = query.Order(ent.Desc(entritual.FieldUpdatedAt))
	default:
		query = query.Order(ent.Desc(entritual.FieldCreatedAt))
	}

	// Execute query
	entRituals, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "rituals"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	ritualModels := make([]any, len(entRituals))
	for i, entRitual := range entRituals {
		ritualModels[i] = toRitualModel(entRitual)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    ritualModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

// UpdateRitual updates an existing ritual
func (d *Datastore) UpdateRitual(ctx context.Context, userID uuid.UUID, ritual models.Ritual) (*models.Ritual, error) {
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

	// Check if ritual exists and belongs to the user
	exists, err := tx.Ritual.Query().
		Where(
			entritual.ID(ritual.ID),
			entritual.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if !exists {
		d.logger.Error(i18n.T2("ritual.not_found_or_unauthorized", "RitualID", ritual.ID.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrRitualNotFound
	}

	if ritual.MCPServerIDs != nil {
		if err := d.validateUserOwnsMCPServerIDs(ctx, tx, userID, *ritual.MCPServerIDs); err != nil {
			if err == ErrInvalidRequestBody {
				if rerr := tx.Rollback(); rerr != nil {
					d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
				}
				return nil, err
			}
			d.logger.Error(i18n.T1("query.failed", "Entity", "mcp server"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
	}

	// Update ritual
	update := tx.Ritual.UpdateOneID(ritual.ID).
		SetName(ritual.Name).
		SetDescription(ritual.Description).
		SetContent(ritual.Content).
		SetHotkeys(ritual.Hotkeys)
	if ritual.MCPServerIDs != nil {
		update.ClearMcpServers()
		if len(*ritual.MCPServerIDs) > 0 {
			update.AddMcpServerIDs((*ritual.MCPServerIDs)...)
		}
	}

	if ritual.PersonalityID != uuid.Nil {
		update.SetPersonalityID(ritual.PersonalityID)
	} else {
		update.ClearPersonality()
	}

	entRitual, err := update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Load the relationships
	entRitual, err = tx.Ritual.Query().
		Where(entritual.ID(entRitual.ID)).
		WithOwner().
		WithPersonality().
		WithMcpServers().
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T("ritual.relationships_load_failed"), zap.Error(err))
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

	return toRitualModel(entRitual), nil
}

// DeleteRitual deletes a ritual
func (d *Datastore) DeleteRitual(ctx context.Context, userID, id uuid.UUID) error {
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

	// Check if ritual exists and belongs to the user
	exists, err := tx.Ritual.Query().
		Where(
			entritual.ID(id),
			entritual.HasOwnerWith(
				user.ID(userID),
			),
		).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		d.logger.Error(i18n.T2("ritual.not_found_or_unauthorized", "RitualID", id.String(), "UserID", userID.String()))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrRitualNotFound
	}

	// Delete ritual
	err = tx.Ritual.DeleteOneID(id).Exec(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "ritual"), zap.Error(err))
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

// GetAvailableRituals returns a paginated list of available rituals for a particular chat that match optional filter criteria
// Rituals are filtered to match the personality of the chat (or no personality if chat has no personality)
func (d *Datastore) GetAvailableRituals(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.RitualFilters) (*models.PaginatedResponse, error) {
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
	dbChat, err := tx.Chat.Query().
		Where(chat.ID(chatID), chat.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			d.logger.Error(i18n.T2("ritual.chat_not_found_or_unauthorized", "ChatID", chatID.String(), "UserID", userID.String()))
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

	// Build query with user authorization and personality filtering
	query := tx.Ritual.Query().
		Where(entritual.HasOwnerWith(user.ID(userID))).
		WithOwner().
		WithPersonality().
		WithMcpServers()

	// Filter rituals based on chat's personality
	if dbChat.Edges.Personality != nil {
		// Chat has a personality - get rituals that match this personality or are global for the user (no personality)
		query = query.Where(entritual.Or(entritual.HasPersonalityWith(personality.ID(dbChat.Edges.Personality.ID)), entritual.Not(entritual.HasPersonality())))
	} else {
		// Chat has no personality - get rituals that are global for the user (no personality)
		query = query.Where(entritual.Not(entritual.HasPersonality()))
	}

	// Apply additional filters if provided
	if filters.Name != nil && *filters.Name != "" {
		query = query.Where(entritual.NameContainsFold(*filters.Name))
	}

	if filters.Query != nil && *filters.Query != "" {
		query = query.Where(
			entritual.Or(
				entritual.NameContainsFold(*filters.Query),
				entritual.DescriptionContainsFold(*filters.Query),
				entritual.ContentContainsFold(*filters.Query),
			),
		)
	}

	if filters.MinDate != nil {
		query = query.Where(entritual.CreatedAtGTE(*filters.MinDate))
	}

	if filters.MaxDate != nil {
		query = query.Where(entritual.CreatedAtLTE(*filters.MaxDate))
	}

	if filters.HasHotkeys != nil {
		if *filters.HasHotkeys {
			query = query.Where(entritual.HotkeysNEQ(""))
		} else {
			query = query.Where(entritual.HotkeysEQ(""))
		}
	}

	// Note: PersonalityID filter from filters is ignored since we're already filtering by chat's personality

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "available rituals"), zap.Error(err))
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
		Order(ent.Desc(entritual.FieldCreatedAt))

	// Execute query
	entRituals, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "available rituals"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	ritualModels := make([]any, len(entRituals))
	for i, entRitual := range entRituals {
		ritualModels[i] = toRitualModel(entRitual)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return &models.PaginatedResponse{
		Results:    ritualModels,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}
