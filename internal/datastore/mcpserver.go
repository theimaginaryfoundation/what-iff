package datastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entchat "github.com/theimaginaryfoundation/what-iff/ent/chat"
	entmcp "github.com/theimaginaryfoundation/what-iff/ent/mcpserver"
	"github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (d *Datastore) toMCPServerModel(e *ent.MCPServer) *models.MCPServer {
	if e == nil {
		return nil
	}

	userID := uuid.Nil
	if e.Edges.Owner != nil {
		userID = e.Edges.Owner.ID
	}

	decryptedToken := ""
	errorMessage := ""
	if strings.TrimSpace(e.AuthToken) != "" {
		token, err := d.decryptTokenForRead(e.AuthToken)
		if err != nil {
			// Never return ciphertext as a usable auth token.
			decryptedToken = ""
			errorMessage = "Authentication token decryption failed. Please re-enter and save the token."
			if d.logger != nil {
				d.logger.Error("failed to decrypt mcp auth token",
					zap.String("mcp_server_id", e.ID.String()),
					zap.Error(err))
			}
		} else {
			decryptedToken = token
		}
	}

	m := &models.MCPServer{
		ID:             e.ID,
		UserID:         userID,
		Name:           e.Name,
		Description:    e.Description,
		ServerURL:      e.ServerURL,
		AuthToken:      decryptedToken,
		ErrorMessage:   errorMessage,
		DefaultEnabled: e.DefaultEnabled,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
	}
	if len(e.Edges.Rituals) > 0 {
		m.RitualIDs = make([]uuid.UUID, len(e.Edges.Rituals))
		for i, r := range e.Edges.Rituals {
			m.RitualIDs[i] = r.ID
		}
	}
	return m
}

func (d *Datastore) CreateMCPServer(ctx context.Context, userID uuid.UUID, server models.MCPServer) (*models.MCPServer, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
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
		d.logger.Error("failed to query user", zap.Error(err))
		tx.Rollback()
		return nil, err
	}
	if !userExists {
		tx.Rollback()
		return nil, ErrUnauthorized
	}

	create := tx.MCPServer.Create().
		SetName(server.Name).
		SetDescription(server.Description).
		SetServerURL(server.ServerURL).
		SetDefaultEnabled(server.DefaultEnabled).
		SetOwnerID(userID)

	if strings.TrimSpace(server.AuthToken) != "" {
		encryptedToken, err := d.encryptTokenForWrite(server.AuthToken)
		if err != nil {
			d.logger.Error("failed to encrypt mcp auth token", zap.Error(err))
			tx.Rollback()
			return nil, err
		}
		create.SetAuthToken(encryptedToken)
	}

	entServer, err := create.Save(ctx)
	if err != nil {
		d.logger.Error("failed to create mcp server", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	entServer, err = tx.MCPServer.Query().
		Where(entmcp.ID(entServer.ID)).
		WithOwner().
		WithRituals().
		Only(ctx)
	if err != nil {
		d.logger.Error("failed to load mcp server owner", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	model := d.toMCPServerModel(entServer)
	if model != nil && strings.TrimSpace(model.ErrorMessage) != "" {
		d.logger.Error("mcp server create failed token verification",
			zap.String("mcp_server_id", entServer.ID.String()),
			zap.String("error_message", model.ErrorMessage))
		tx.Rollback()
		return nil, fmt.Errorf("failed to verify mcp server token after create: %w", ErrInternalDatastore)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, err
	}
	return model, nil
}

func (d *Datastore) GetMCPServer(ctx context.Context, userID, id uuid.UUID) (*models.MCPServer, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	entServer, err := tx.MCPServer.Query().
		Where(
			entmcp.ID(id),
			entmcp.HasOwnerWith(user.ID(userID)),
		).
		WithOwner().
		WithRituals().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			tx.Rollback()
			return nil, ErrMCPServerNotFound
		}
		d.logger.Error("failed to get mcp server", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, err
	}
	return d.toMCPServerModel(entServer), nil
}

func (d *Datastore) ListMCPServers(ctx context.Context, userID uuid.UUID, pageNum, pageSize int, filters models.MCPServerFilters) (*models.PaginatedResponse, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	query := tx.MCPServer.Query().
		Where(entmcp.HasOwnerWith(user.ID(userID))).
		WithOwner().
		WithRituals()

	if filters.Query != nil && strings.TrimSpace(*filters.Query) != "" {
		q := strings.TrimSpace(*filters.Query)
		query = query.Where(entmcp.Or(
			entmcp.NameContainsFold(q),
			entmcp.DescriptionContainsFold(q),
		))
	}

	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error("failed to count mcp servers", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	offset := (pageNum - 1) * pageSize
	servers, err := query.
		Offset(offset).
		Limit(pageSize).
		Order(ent.Desc(entmcp.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list mcp servers", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	results := make([]any, len(servers))
	for i := range servers {
		results[i] = d.toMCPServerModel(servers[i])
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, err
	}
	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

func (d *Datastore) validateUserOwnsRitualIDs(ctx context.Context, tx *ent.Tx, userID uuid.UUID, ritualIDs []uuid.UUID) error {
	if len(ritualIDs) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(ritualIDs))
	unique := make([]uuid.UUID, 0, len(ritualIDs))
	for _, id := range ritualIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	n, err := tx.Ritual.Query().
		Where(
			ritual.IDIn(unique...),
			ritual.HasOwnerWith(user.ID(userID)),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if n != len(unique) {
		return ErrInvalidRequestBody
	}
	return nil
}

func (d *Datastore) validateUserOwnsMCPServerIDs(ctx context.Context, tx *ent.Tx, userID uuid.UUID, mcpServerIDs []uuid.UUID) error {
	if len(mcpServerIDs) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(mcpServerIDs))
	unique := make([]uuid.UUID, 0, len(mcpServerIDs))
	for _, id := range mcpServerIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	n, err := tx.MCPServer.Query().
		Where(
			entmcp.IDIn(unique...),
			entmcp.HasOwnerWith(user.ID(userID)),
		).
		Count(ctx)
	if err != nil {
		return err
	}
	if n != len(unique) {
		return ErrInvalidRequestBody
	}
	return nil
}

// UpdateMCPServer updates an existing MCP server owned by the user.
// Auth token update semantics are controlled via authTokenUpdate.
// ritualIDsUpdate: nil means do not change ritual links; non-nil replaces the set (empty clears).
func (d *Datastore) UpdateMCPServer(ctx context.Context, userID uuid.UUID, server models.MCPServer, authTokenUpdate models.MCPServerAuthTokenUpdate, ritualIDsUpdate *[]uuid.UUID) (*models.MCPServer, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.MCPServer.Query().
		Where(
			entmcp.ID(server.ID),
			entmcp.HasOwnerWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error("failed to query mcp server", zap.Error(err))
		tx.Rollback()
		return nil, err
	}
	if !exists {
		tx.Rollback()
		return nil, ErrMCPServerNotFound
	}

	update := tx.MCPServer.UpdateOneID(server.ID).
		SetName(server.Name).
		SetDescription(server.Description).
		SetServerURL(server.ServerURL).
		SetDefaultEnabled(server.DefaultEnabled)

	if authTokenUpdate.Provided {
		if authTokenUpdate.Clear {
			update.ClearAuthToken()
		} else if token := strings.TrimSpace(authTokenUpdate.Value); token != "" {
			encryptedToken, err := d.encryptTokenForWrite(token)
			if err != nil {
				d.logger.Error("failed to encrypt mcp auth token for update", zap.Error(err))
				tx.Rollback()
				return nil, err
			}
			update.SetAuthToken(encryptedToken)
		}
	}

	entServer, err := update.Save(ctx)
	if err != nil {
		d.logger.Error("failed to update mcp server", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	if ritualIDsUpdate != nil {
		if err := d.validateUserOwnsRitualIDs(ctx, tx, userID, *ritualIDsUpdate); err != nil {
			tx.Rollback()
			return nil, err
		}
		edgeUpd := tx.MCPServer.UpdateOneID(entServer.ID).ClearRituals()
		if len(*ritualIDsUpdate) > 0 {
			edgeUpd = edgeUpd.AddRitualIDs((*ritualIDsUpdate)...)
		}
		if _, err := edgeUpd.Save(ctx); err != nil {
			d.logger.Error("failed to update mcp server ritual edges", zap.Error(err))
			tx.Rollback()
			return nil, err
		}
	}

	entServer, err = tx.MCPServer.Query().
		Where(entmcp.ID(entServer.ID)).
		WithOwner().
		WithRituals().
		Only(ctx)
	if err != nil {
		d.logger.Error("failed to load mcp server owner", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	model := d.toMCPServerModel(entServer)
	if model != nil && strings.TrimSpace(model.ErrorMessage) != "" {
		d.logger.Error("mcp server update failed token verification",
			zap.String("mcp_server_id", entServer.ID.String()),
			zap.String("error_message", model.ErrorMessage))
		tx.Rollback()
		return nil, fmt.Errorf("failed to verify mcp server token after update: %w", ErrInternalDatastore)
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, err
	}
	return model, nil
}

func (d *Datastore) DeleteMCPServer(ctx context.Context, userID, id uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.MCPServer.Query().
		Where(
			entmcp.ID(id),
			entmcp.HasOwnerWith(user.ID(userID)),
		).
		Exist(ctx)
	if err != nil {
		d.logger.Error("failed to query mcp server", zap.Error(err))
		tx.Rollback()
		return err
	}
	if !exists {
		tx.Rollback()
		return ErrMCPServerNotFound
	}

	if err := tx.MCPServer.DeleteOneID(id).Exec(ctx); err != nil {
		d.logger.Error("failed to delete mcp server", zap.Error(err))
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return err
	}
	return nil
}

func (d *Datastore) ListDefaultEnabledMCPServers(ctx context.Context, userID uuid.UUID) ([]*models.MCPServer, error) {
	servers, err := d.dbClient.MCPServer.Query().
		Where(
			entmcp.HasOwnerWith(user.ID(userID)),
			entmcp.DefaultEnabledEQ(true),
		).
		WithOwner().
		Order(ent.Desc(entmcp.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list default-enabled mcp servers", zap.Error(err))
		return nil, err
	}

	out := make([]*models.MCPServer, 0, len(servers))
	for _, srv := range servers {
		out = append(out, d.toMCPServerModel(srv))
	}
	return out, nil
}

func (d *Datastore) ListChatMCPServers(ctx context.Context, userID, chatID uuid.UUID) ([]*models.MCPServer, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, err
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
		d.logger.Error("failed to query chat", zap.Error(err))
		tx.Rollback()
		return nil, err
	}
	if !chatExists {
		tx.Rollback()
		return nil, ErrChatNotFound
	}

	servers, err := tx.MCPServer.Query().
		Where(
			entmcp.HasOwnerWith(user.ID(userID)),
			entmcp.HasChatsWith(entchat.ID(chatID)),
		).
		WithOwner().
		Order(ent.Desc(entmcp.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list chat mcp servers", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, err
	}

	out := make([]*models.MCPServer, 0, len(servers))
	for _, srv := range servers {
		out = append(out, d.toMCPServerModel(srv))
	}
	return out, nil
}

func (d *Datastore) ListRitualMCPServers(ctx context.Context, userID uuid.UUID, ritualIDs []uuid.UUID) ([]*models.MCPServer, error) {
	if len(ritualIDs) == 0 {
		return []*models.MCPServer{}, nil
	}

	servers, err := d.dbClient.MCPServer.Query().
		Where(
			entmcp.HasOwnerWith(user.ID(userID)),
			entmcp.HasRitualsWith(ritual.IDIn(ritualIDs...)),
		).
		WithOwner().
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list ritual mcp servers", zap.Error(err))
		return nil, err
	}

	out := make([]*models.MCPServer, 0, len(servers))
	for _, srv := range servers {
		out = append(out, d.toMCPServerModel(srv))
	}
	return out, nil
}

func (d *Datastore) ListAvailableChatMCPServers(ctx context.Context, userID, chatID uuid.UUID, pageNum, pageSize int, filters models.MCPServerFilters) (*models.PaginatedResponse, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, err
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
		d.logger.Error("failed to query chat", zap.Error(err))
		tx.Rollback()
		return nil, err
	}
	if !chatExists {
		tx.Rollback()
		return nil, ErrChatNotFound
	}

	query := tx.MCPServer.Query().
		Where(
			entmcp.HasOwnerWith(user.ID(userID)),
			entmcp.Not(entmcp.HasChatsWith(entchat.ID(chatID))),
		).
		WithOwner()

	if filters.Query != nil && strings.TrimSpace(*filters.Query) != "" {
		q := strings.TrimSpace(*filters.Query)
		query = query.Where(entmcp.Or(
			entmcp.NameContainsFold(q),
			entmcp.DescriptionContainsFold(q),
		))
	}

	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error("failed to count available chat mcp servers", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	offset := (pageNum - 1) * pageSize
	servers, err := query.
		Offset(offset).
		Limit(pageSize).
		Order(ent.Desc(entmcp.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list available chat mcp servers", zap.Error(err))
		tx.Rollback()
		return nil, err
	}

	results := make([]any, len(servers))
	for i := range servers {
		results[i] = d.toMCPServerModel(servers[i])
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return nil, err
	}
	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: totalCount,
		Page:       pageNum,
	}, nil
}

func (d *Datastore) encryptTokenForWrite(plaintext string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", nil
	}
	if d.tokenCrypto == nil {
		if d.logger != nil {
			d.logger.Error("token crypto is not configured during mcp token encryption",
				zap.String("required_env_var", TokenEncryptionSecretEnvVar),
				zap.Error(errTokenEncryptionSecretNotConfigured))
		}
		return "", errTokenEncryptionSecretNotConfigured
	}
	return d.tokenCrypto.Encrypt(plaintext)
}

func (d *Datastore) decryptTokenForRead(ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	if d.tokenCrypto == nil {
		if d.logger != nil {
			d.logger.Error("token crypto is not configured during mcp token decryption",
				zap.String("required_env_var", TokenEncryptionSecretEnvVar),
				zap.Error(errTokenEncryptionSecretNotConfigured))
		}
		return "", errTokenEncryptionSecretNotConfigured
	}
	return d.tokenCrypto.Decrypt(ciphertext)
}

func (d *Datastore) AddMCPServerToChat(ctx context.Context, userID, chatID, mcpServerID uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
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
		d.logger.Error("failed to query chat", zap.Error(err))
		tx.Rollback()
		return err
	}
	if !chatExists {
		tx.Rollback()
		return ErrChatNotFound
	}

	serverExists, err := tx.MCPServer.Query().
		Where(entmcp.ID(mcpServerID), entmcp.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		d.logger.Error("failed to query mcp server", zap.Error(err))
		tx.Rollback()
		return err
	}
	if !serverExists {
		tx.Rollback()
		return ErrMCPServerNotFound
	}

	if _, err := tx.Chat.UpdateOneID(chatID).AddMcpServerIDs(mcpServerID).Save(ctx); err != nil {
		d.logger.Error("failed to add mcp server to chat", zap.Error(err))
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return err
	}
	return nil
}

func (d *Datastore) RemoveMCPServerFromChat(ctx context.Context, userID, chatID, mcpServerID uuid.UUID) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
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
		d.logger.Error("failed to query chat", zap.Error(err))
		tx.Rollback()
		return err
	}
	if !chatExists {
		tx.Rollback()
		return ErrChatNotFound
	}

	serverExists, err := tx.MCPServer.Query().
		Where(entmcp.ID(mcpServerID), entmcp.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		d.logger.Error("failed to query mcp server", zap.Error(err))
		tx.Rollback()
		return err
	}
	if !serverExists {
		tx.Rollback()
		return ErrMCPServerNotFound
	}

	if _, err := tx.Chat.UpdateOneID(chatID).RemoveMcpServerIDs(mcpServerID).Save(ctx); err != nil {
		d.logger.Error("failed to remove mcp server from chat", zap.Error(err))
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error("failed to commit transaction", zap.Error(err))
		return err
	}
	return nil
}
