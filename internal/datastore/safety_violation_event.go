package datastore

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/safetyviolationevent"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

type AdminListSafetyViolationEventsFilters struct {
	Provider *string
	Search   *string
}

func toSafetyViolationEventModel(e *ent.SafetyViolationEvent, fallbackUserID *uuid.UUID) *models.SafetyViolationEvent {
	if e == nil {
		return nil
	}
	m := &models.SafetyViolationEvent{
		ID:              e.ID,
		OccurredAt:      e.OccurredAt,
		Provider:        models.SafetyViolationProvider(e.Provider),
		ViolationType:   "",
		ProviderCode:    "",
		ProviderMessage: e.ProviderMessage,
		RawError:        e.RawError,
		UserID:          uuid.Nil,
		ChatName:        e.ChatName,
		ChatMessageText: e.ChatMessageText,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
	if e.ViolationType != nil {
		m.ViolationType = *e.ViolationType
	}
	if e.ProviderCode != nil {
		m.ProviderCode = *e.ProviderCode
	}
	if fallbackUserID != nil {
		m.UserID = *fallbackUserID
	} else if e.Edges.User != nil {
		m.UserID = e.Edges.User.ID
	}
	if e.ChatID != nil {
		id := *e.ChatID
		m.ChatID = &id
	}
	if e.ChatMessageID != nil {
		id := *e.ChatMessageID
		m.ChatMessageID = &id
	}
	return m
}

func (d *Datastore) CreateSafetyViolationEvent(ctx context.Context, input models.CreateSafetyViolationEventInput) (*models.SafetyViolationEvent, error) {
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

	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}

	create := tx.SafetyViolationEvent.Create().
		SetOccurredAt(occurredAt).
		SetProvider(safetyviolationevent.Provider(input.Provider)).
		SetProviderMessage(input.ProviderMessage).
		SetRawError(input.RawError).
		SetUserID(input.UserID).
		SetChatName(input.ChatName).
		SetChatMessageText(input.ChatMessageText)

	if strings.TrimSpace(input.ViolationType) != "" {
		create.SetViolationType(strings.TrimSpace(input.ViolationType))
	}
	if strings.TrimSpace(input.ProviderCode) != "" {
		create.SetProviderCode(strings.TrimSpace(input.ProviderCode))
	}
	if input.ChatID != nil {
		create.SetChatID(*input.ChatID)
	}
	if input.ChatMessageID != nil {
		create.SetChatMessageID(*input.ChatMessageID)
	}

	entEvent, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "safety violation event"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toSafetyViolationEventModel(entEvent, &input.UserID), nil
}

func (d *Datastore) AdminListSafetyViolationEvents(ctx context.Context, pageNum, pageSize int, filters AdminListSafetyViolationEventsFilters) (*models.PaginatedResponse, error) {
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

	query := tx.SafetyViolationEvent.Query()
	if filters.Provider != nil && strings.TrimSpace(*filters.Provider) != "" {
		query = query.Where(safetyviolationevent.ProviderEQ(safetyviolationevent.Provider(strings.TrimSpace(*filters.Provider))))
	}
	if filters.Search != nil && strings.TrimSpace(*filters.Search) != "" {
		search := strings.TrimSpace(*filters.Search)
		query = query.Where(safetyviolationevent.Or(
			safetyviolationevent.ProviderMessageContainsFold(search),
			safetyviolationevent.RawErrorContainsFold(search),
			safetyviolationevent.ChatNameContainsFold(search),
			safetyviolationevent.ChatMessageTextContainsFold(search),
		))
	}

	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "safety violation events"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	pagination := utils.NormalizePagination(pageNum, pageSize)
	entEvents, err := query.
		WithUser().
		Offset(pagination.Offset).
		Limit(pagination.PageSize).
		Order(ent.Desc(safetyviolationevent.FieldOccurredAt)).
		All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("query.failed", "Entity", "safety violation events"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	results := make([]any, len(entEvents))
	for i, ev := range entEvents {
		results[i] = toSafetyViolationEventModel(ev, nil)
	}
	return utils.BuildPaginatedResponse(results, totalCount, pagination), nil
}

func (d *Datastore) AdminGetSafetyViolationEvent(ctx context.Context, id uuid.UUID) (*models.SafetyViolationEvent, error) {
	event, err := d.dbClient.SafetyViolationEvent.Query().
		Where(safetyviolationevent.ID(id)).
		WithUser().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrSafetyViolationEventNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "safety violation event"), zap.Error(err))
		return nil, err
	}
	return toSafetyViolationEventModel(event, nil), nil
}
