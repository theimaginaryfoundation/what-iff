package datastore

import (
	"context"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chat"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Convert from Ent ritual to model
func toRitualModel(e *ent.Ritual) *models.Ritual {
	if e == nil {
		return nil
	}

	license, compatibility := e.License, e.Compatibility
	meta := e.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	allowedTools := e.AllowedTools
	if allowedTools == nil {
		allowedTools = []string{}
	}
	disableModel, userInvocable := e.DisableModelInvocation, e.UserInvocable
	ritualModel := models.Ritual{
		ID:                     e.ID,
		Slug:                   e.Slug,
		Name:                   e.Name,
		Description:            e.Description,
		Content:                e.Content,
		Hotkeys:                e.Hotkeys,
		License:                &license,
		Compatibility:          &compatibility,
		Metadata:               &meta,
		AllowedTools:           &allowedTools,
		DisableModelInvocation: &disableModel,
		UserInvocable:          &userInvocable,
		CreatedAt:              e.CreatedAt,
		UpdatedAt:              e.UpdatedAt,
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

	// The slug is the skill's name in the Agent Skills sense; the handler derives it from the
	// display name when the request has none (skills.Normalize), so an empty one here is a bug.
	slug := strings.TrimSpace(ritual.Slug)
	if slug == "" {
		slug = skills.Slugify(ritual.Name)
	}
	taken, err := ritualSlugTaken(ctx, tx.Client(), userID, slug, uuid.Nil)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	if taken {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrRitualSlugTaken
	}

	// Create ritual
	create := tx.Ritual.Create().
		SetName(ritual.Name).
		SetSlug(slug).
		SetDescription(ritual.Description).
		SetContent(ritual.Content).
		SetHotkeys(ritual.Hotkeys).
		SetLicense(ritual.LicenseText()).
		SetCompatibility(ritual.CompatibilityText()).
		SetMetadata(ritual.MetadataMap()).
		SetAllowedTools(ritual.AllowedToolList()).
		SetDisableModelInvocation(!ritual.IsModelInvocable()).
		SetUserInvocable(ritual.IsUserInvocable()).
		SetOwnerID(userID)
	if ritual.MCPServerIDs != nil && len(*ritual.MCPServerIDs) > 0 {
		create.AddMcpServerIDs((*ritual.MCPServerIDs)...)
	}

	if ritual.PersonalityID != uuid.Nil {
		create.SetPersonalityID(ritual.PersonalityID)
	}

	entRitual, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRitualSlugTaken
		}
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
				entritual.SlugContainsFold(*filters.Query),
				entritual.NameContainsFold(*filters.Query),
				entritual.DescriptionContainsFold(*filters.Query),
				entritual.ContentContainsFold(*filters.Query),
			),
		)
	}

	if filters.Slug != nil && *filters.Slug != "" {
		query = query.Where(entritual.Slug(*filters.Slug))
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

	// Check if ritual exists and belongs to the user; the current row's slug tells whether the
	// bundle files need refiling.
	current, err := tx.Ritual.Query().
		Where(
			entritual.ID(ritual.ID),
			entritual.HasOwnerWith(
				user.ID(userID),
			),
		).
		Only(ctx)

	if err != nil && !ent.IsNotFound(err) {
		d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if current == nil {
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

	// Update ritual. An empty slug keeps the stored one; the pointer fields follow the same rule.
	update := tx.Ritual.UpdateOneID(ritual.ID).
		SetName(ritual.Name).
		SetDescription(ritual.Description).
		SetContent(ritual.Content).
		SetHotkeys(ritual.Hotkeys)
	if slug := strings.TrimSpace(ritual.Slug); slug != "" {
		taken, err := ritualSlugTaken(ctx, tx.Client(), userID, slug, ritual.ID)
		if err != nil {
			d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
		if taken {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRitualSlugTaken
		}
		update.SetSlug(slug)
	}
	if ritual.License != nil {
		update.SetLicense(*ritual.License)
	}
	if ritual.Compatibility != nil {
		update.SetCompatibility(*ritual.Compatibility)
	}
	if ritual.Metadata != nil {
		update.SetMetadata(ritual.MetadataMap())
	}
	if ritual.AllowedTools != nil {
		update.SetAllowedTools(ritual.AllowedToolList())
	}
	if ritual.DisableModelInvocation != nil {
		update.SetDisableModelInvocation(*ritual.DisableModelInvocation)
	}
	if ritual.UserInvocable != nil {
		update.SetUserInvocable(*ritual.UserInvocable)
	}
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
		if ent.IsConstraintError(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRitualSlugTaken
		}
		d.logger.Error(i18n.T1("update.failed", "Entity", "ritual"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}
	// The bundle's files are filed under skills/<slug>/…; a new slug moves them along (ADR 0x035 §3).
	if newSlug := strings.TrimSpace(ritual.Slug); newSlug != "" && newSlug != current.Slug {
		if err := moveSkillFilesToSlug(ctx, tx, ritual.ID, current.Slug, newSlug); err != nil {
			d.logger.Error("failed to refile a skill's files after its slug changed", zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}
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

	// Build query with user authorization and personality filtering. This is the user's
	// invocation surface (the /skill picker and hotkeys), so skills marked not user-invocable are
	// left out; the model may still load them (ADR 0x035 §1).
	query := tx.Ritual.Query().
		Where(entritual.HasOwnerWith(user.ID(userID)), entritual.UserInvocable(true)).
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

// ritualSlugTaken reports whether one of userID's other rituals (not excludeID) has slug.
func ritualSlugTaken(ctx context.Context, client *ent.Client, userID uuid.UUID, slug string, excludeID uuid.UUID) (bool, error) {
	q := client.Ritual.Query().Where(
		entritual.Slug(slug),
		entritual.HasOwnerWith(user.ID(userID)),
	)
	if excludeID != uuid.Nil {
		q = q.Where(entritual.IDNEQ(excludeID))
	}
	return q.Exist(ctx)
}

// RitualSlugTaken reports whether slug is already used by one of userID's rituals other than
// excludeID (uuid.Nil to check them all). Import uses it to pick a free slug.
func (d *Datastore) RitualSlugTaken(ctx context.Context, userID uuid.UUID, slug string, excludeID uuid.UUID) (bool, error) {
	return ritualSlugTaken(ctx, d.dbClient, userID, slug, excludeID)
}

// GetRitualBySlug returns userID's ritual with the given slug, or ErrRitualNotFound.
func (d *Datastore) GetRitualBySlug(ctx context.Context, userID uuid.UUID, slug string) (*models.Ritual, error) {
	entRitual, err := d.dbClient.Ritual.Query().
		Where(entritual.Slug(slug), entritual.HasOwnerWith(user.ID(userID))).
		WithPersonality().
		WithMcpServers().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrRitualNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "ritual"), zap.Error(err))
		return nil, err
	}
	return toRitualModel(entRitual), nil
}
