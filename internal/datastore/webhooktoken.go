package datastore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/ent/webhooktoken"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const webhookTokenPrefix = "wht_"

func toWebhookTokenModel(entToken *ent.WebhookToken) *models.WebhookToken {
	if entToken == nil {
		return nil
	}

	tokenModel := &models.WebhookToken{
		ID:        entToken.ID,
		Name:      entToken.Name,
		Status:    models.WebhookTokenStatus(entToken.Status),
		CreatedAt: entToken.CreatedAt,
		UpdatedAt: entToken.UpdatedAt,
	}

	if entToken.Edges.Owner != nil {
		tokenModel.UserID = entToken.Edges.Owner.ID
	}
	if entToken.LastUsedAt != nil {
		tokenModel.LastUsedAt = entToken.LastUsedAt
	}

	return tokenModel
}

func generateWebhookTokenSecret() (string, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", fmt.Errorf("generate webhook token secret: %w", err)
	}
	return webhookTokenPrefix + base64.RawURLEncoding.EncodeToString(secretBytes), nil
}

func hashWebhookToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateWebhookToken creates a new user-scoped webhook token and returns the raw token once.
func (d *Datastore) CreateWebhookToken(ctx context.Context, userID uuid.UUID, name string) (*models.WebhookToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", ErrInvalidRequestBody
	}

	rawSecret, err := generateWebhookTokenSecret()
	if err != nil {
		d.logger.Error("failed to generate webhook token secret", zap.Error(err))
		return nil, "", err
	}
	tokenHash := hashWebhookToken(rawSecret)

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error("failed to start transaction", zap.Error(err))
		return nil, "", err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	userExists, err := tx.User.Query().Where(user.ID(userID)).Exist(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return nil, "", err
	}
	if !userExists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return nil, "", ErrUnauthorized
	}

	entToken, err := tx.WebhookToken.Create().
		SetName(name).
		SetTokenHash(tokenHash).
		SetStatus(webhooktoken.StatusActive).
		SetOwnerID(userID).
		Save(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return nil, "", err
	}

	entToken, err = tx.WebhookToken.Query().
		Where(webhooktoken.ID(entToken.ID)).
		WithOwner().
		Only(ctx)
	if err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error("failed to rollback transaction", zap.Error(rerr))
		}
		return nil, "", err
	}

	if err := tx.Commit(); err != nil {
		return nil, "", err
	}

	return toWebhookTokenModel(entToken), rawSecret, nil
}

// ListWebhookTokens returns all webhook tokens for a user.
func (d *Datastore) ListWebhookTokens(ctx context.Context, userID uuid.UUID) ([]*models.WebhookToken, error) {
	entTokens, err := d.dbClient.WebhookToken.Query().
		Where(webhooktoken.HasOwnerWith(user.ID(userID))).
		WithOwner().
		Order(ent.Desc(webhooktoken.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*models.WebhookToken, 0, len(entTokens))
	for _, entToken := range entTokens {
		out = append(out, toWebhookTokenModel(entToken))
	}
	return out, nil
}

// RevokeWebhookToken marks a webhook token as revoked for the owner.
func (d *Datastore) RevokeWebhookToken(ctx context.Context, userID, tokenID uuid.UUID) error {
	updated, err := d.dbClient.WebhookToken.Update().
		Where(
			webhooktoken.ID(tokenID),
			webhooktoken.HasOwnerWith(user.ID(userID)),
		).
		SetStatus(webhooktoken.StatusRevoked).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		return ErrWebhookTokenNotFound
	}
	return nil
}

// AuthenticateWebhookToken validates a raw webhook token and returns its request principal.
// The principal intentionally inherits the owning user's effective role/plan/timezone so
// webhook calls execute with the same scope/permissions as that user.
func (d *Datastore) AuthenticateWebhookToken(ctx context.Context, rawToken string) (*models.WebhookAuthPrincipal, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrWebhookTokenInvalid
	}

	tokenHash := hashWebhookToken(rawToken)
	entToken, err := d.dbClient.WebhookToken.Query().
		Where(
			webhooktoken.TokenHashEQ(tokenHash),
			webhooktoken.StatusEQ(webhooktoken.StatusActive),
		).
		WithOwner(func(q *ent.UserQuery) {
			q.WithRoles()
		}).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrWebhookTokenInvalid
		}
		return nil, err
	}

	if subtle.ConstantTimeCompare([]byte(entToken.TokenHash), []byte(tokenHash)) != 1 {
		return nil, ErrWebhookTokenInvalid
	}
	if entToken.Edges.Owner == nil {
		return nil, ErrWebhookTokenInvalid
	}

	owner := entToken.Edges.Owner
	roleName := "user"
	for _, role := range owner.Edges.Roles {
		if role.Name == "super_admin" || role.Name == "admin" {
			roleName = role.Name
			break
		}
	}

	if _, touchErr := d.dbClient.WebhookToken.UpdateOneID(entToken.ID).
		SetLastUsedAt(time.Now()).
		Save(ctx); touchErr != nil {
		d.logger.Warn("failed to update webhook token last_used_at",
			zap.String("token_id", entToken.ID.String()),
			zap.Error(touchErr))
	}

	return &models.WebhookAuthPrincipal{
		UserID:         owner.ID,
		Role:           roleName,
		Timezone:       owner.Timezone,
		WebhookTokenID: entToken.ID,
	}, nil
}
