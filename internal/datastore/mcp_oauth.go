package datastore

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	entoauth "github.com/theimaginaryfoundation/what-iff/ent/mcpoauthsession"
	entmcp "github.com/theimaginaryfoundation/what-iff/ent/mcpserver"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func (d *Datastore) CreateMCPOAuthSession(ctx context.Context, userID, mcpServerID uuid.UUID, state, codeVerifier, redirectAfter string, expiresAt time.Time) (*models.MCPOAuthSession, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	exists, err := tx.MCPServer.Query().
		Where(entmcp.ID(mcpServerID), entmcp.HasOwnerWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if !exists {
		_ = tx.Rollback()
		return nil, ErrMCPServerNotFound
	}

	s, err := tx.MCPOAuthSession.Create().
		SetOwnerID(userID).
		SetMcpServerID(mcpServerID).
		SetState(strings.TrimSpace(state)).
		SetCodeVerifier(strings.TrimSpace(codeVerifier)).
		SetRedirectAfter(strings.TrimSpace(redirectAfter)).
		SetExpiresAt(expiresAt.UTC()).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &models.MCPOAuthSession{
		ID:            s.ID,
		UserID:        userID,
		MCPServerID:   mcpServerID,
		State:         s.State,
		CodeVerifier:  s.CodeVerifier,
		ExpiresAt:     s.ExpiresAt,
		ConsumedAt:    s.ConsumedAt,
		RedirectAfter: s.RedirectAfter,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}, nil
}

func (d *Datastore) ConsumeMCPOAuthSessionByState(ctx context.Context, state string) (*models.MCPOAuthSession, *models.MCPServer, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	raw, err := tx.MCPOAuthSession.Query().
		Where(entoauth.StateEQ(strings.TrimSpace(state))).
		WithOwner().
		WithMcpServer(func(q *ent.MCPServerQuery) { q.WithOwner() }).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return nil, nil, ErrMCPOAuthSessionNotFound
		}
		return nil, nil, err
	}
	if raw.ConsumedAt != nil {
		_ = tx.Rollback()
		return nil, nil, ErrMCPOAuthSessionConsumed
	}
	if time.Now().UTC().After(raw.ExpiresAt) {
		_ = tx.Rollback()
		return nil, nil, ErrMCPOAuthSessionExpired
	}

	now := time.Now().UTC()
	if _, err := tx.MCPOAuthSession.UpdateOneID(raw.ID).SetConsumedAt(now).Save(ctx); err != nil {
		_ = tx.Rollback()
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	session := &models.MCPOAuthSession{
		ID:            raw.ID,
		UserID:        raw.Edges.Owner.ID,
		MCPServerID:   raw.Edges.McpServer.ID,
		State:         raw.State,
		CodeVerifier:  raw.CodeVerifier,
		ExpiresAt:     raw.ExpiresAt,
		ConsumedAt:    &now,
		RedirectAfter: raw.RedirectAfter,
		CreatedAt:     raw.CreatedAt,
		UpdatedAt:     raw.UpdatedAt,
	}
	server := d.toMCPServerModel(raw.Edges.McpServer)
	return session, server, nil
}

func (d *Datastore) SaveMCPServerOAuthTokens(ctx context.Context, userID, mcpServerID uuid.UUID, set models.MCPOAuthTokenSet) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	upd := tx.MCPServer.Update().
		Where(entmcp.ID(mcpServerID), entmcp.HasOwnerWith(user.ID(userID)))
	if strings.TrimSpace(set.AccessToken) != "" {
		cipher, err := d.encryptTokenForWrite(set.AccessToken)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		upd = upd.SetOauthAccessToken(cipher)
	}
	if strings.TrimSpace(set.RefreshToken) != "" {
		cipher, err := d.encryptTokenForWrite(set.RefreshToken)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		upd = upd.SetOauthRefreshToken(cipher)
	}
	upd = upd.SetOauthRefreshFailCount(0).
		SetStatus(models.MCPServerStatusActive).
		SetStatusReason("")
	if set.AccessTokenExpiresAt != nil {
		upd = upd.SetOauthAccessTokenExpiresAt(set.AccessTokenExpiresAt.UTC())
	}
	if set.RefreshTokenExpiresAt != nil {
		upd = upd.SetOauthRefreshTokenExpiresAt(set.RefreshTokenExpiresAt.UTC())
	}
	if set.AuthenticatedAt != nil {
		upd = upd.SetOauthAuthenticatedAt(set.AuthenticatedAt.UTC())
	}
	if set.LastRefreshAt != nil {
		upd = upd.SetOauthLastRefreshAt(set.LastRefreshAt.UTC())
	}
	if _, err := upd.Save(ctx); err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return ErrMCPServerNotFound
		}
		return err
	}
	return tx.Commit()
}

func (d *Datastore) MarkMCPServerOAuthRefreshFailure(ctx context.Context, userID, mcpServerID uuid.UUID, reason string, terminal bool) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()

	server, err := tx.MCPServer.Query().
		Where(entmcp.ID(mcpServerID), entmcp.HasOwnerWith(user.ID(userID))).
		Only(ctx)
	if err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return ErrMCPServerNotFound
		}
		return err
	}
	failCount := server.OauthRefreshFailCount + 1
	status := models.MCPServerStatusRefreshError
	if terminal {
		status = models.MCPServerStatusInvalid
	}
	if _, err := tx.MCPServer.UpdateOneID(server.ID).
		SetOauthRefreshFailCount(failCount).
		SetStatus(status).
		SetStatusReason(strings.TrimSpace(reason)).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Datastore) ListOAuthMCPServersDueForRefresh(ctx context.Context, before time.Time, limit int) ([]*models.MCPServer, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.dbClient.MCPServer.Query().
		Where(
			entmcp.AuthModeEQ(models.MCPServerAuthModeOAuth),
			entmcp.StatusNotIn(models.MCPServerStatusDisabled, models.MCPServerStatusInvalid),
			entmcp.OauthAccessTokenNotNil(),
			entmcp.OauthRefreshTokenNotNil(),
			entmcp.Or(
				entmcp.OauthAccessTokenExpiresAtLTE(before),
				entmcp.StatusEQ(models.MCPServerStatusRefreshError),
			),
		).
		WithOwner().
		Limit(limit).
		Order(ent.Asc(entmcp.FieldOauthAccessTokenExpiresAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*models.MCPServer, 0, len(rows))
	for _, row := range rows {
		m := d.toMCPServerModel(row)
		if m == nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (d *Datastore) MarkMCPServerOAuthExpiring(ctx context.Context, userID, mcpServerID uuid.UUID, reason string) error {
	if _, err := d.dbClient.MCPServer.Update().
		Where(entmcp.ID(mcpServerID), entmcp.HasOwnerWith(user.ID(userID))).
		SetStatus(models.MCPServerStatusExpiring).
		SetStatusReason(strings.TrimSpace(reason)).
		Save(ctx); err != nil {
		if ent.IsNotFound(err) {
			return ErrMCPServerNotFound
		}
		if d.logger != nil {
			d.logger.Warn("failed to mark mcp server expiring", zap.Error(err))
		}
		return err
	}
	return nil
}
