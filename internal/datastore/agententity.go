package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/agententity"
	entpersonality "github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

var (
	// ErrAgentEntityNotFound is returned when no entity with that key (or id) is visible.
	ErrAgentEntityNotFound = errors.New("entity not found")
	// ErrAgentEntityExists is returned by CreateAgentEntity when the key is already taken by an
	// entity the writer can see.
	ErrAgentEntityExists = errors.New("entity already exists")
	// ErrAgentEntityConflict is returned when an update kept losing to concurrent writers.
	ErrAgentEntityConflict = errors.New("entity is being changed by another writer")
)

// agentEntityUpdateAttempts bounds the compare-and-swap retries of UpdateAgentEntity.
const agentEntityUpdateAttempts = 3

func toAgentEntityModel(e *ent.AgentEntity) *models.AgentEntity {
	m := &models.AgentEntity{
		ID:                  e.ID,
		Key:                 e.Key,
		Title:               e.Title,
		Body:                e.Body,
		PinnedPersonalityID: e.PinnedPersonalityID,
		LastPersonalityID:   e.LastPersonalityID,
		LastChatID:          e.LastChatID,
		CreatedAt:           e.CreatedAt,
		UpdatedAt:           e.UpdatedAt,
		DeletedAt:           e.DeletedAt,
	}
	if e.Edges.Owner != nil {
		m.UserID = e.Edges.Owner.ID
	}
	if e.Edges.PinnedPersonality != nil {
		m.PinnedPersonalityName = e.Edges.PinnedPersonality.Name
	}
	return m
}

// liveAgentEntities matches the user's entities that are not tombstones.
func liveAgentEntities(userID uuid.UUID) predicate.AgentEntity {
	return agententity.And(agententity.HasOwnerWith(user.ID(userID)), agententity.DeletedAtIsNil())
}

// visibleAgentEntities matches the user's live entities a conversation with activePersonalityID
// sees: the global ones and those pinned to that personality, the same rule as memories.
func visibleAgentEntities(userID, activePersonalityID uuid.UUID) predicate.AgentEntity {
	pinned := agententity.PinnedPersonalityIDIsNil()
	if activePersonalityID != uuid.Nil {
		pinned = agententity.Or(pinned, agententity.PinnedPersonalityIDEQ(activePersonalityID))
	}
	return agententity.And(liveAgentEntities(userID), pinned)
}

// resolveAgentEntity returns the visible entity with normalized key. When both a pinned and a
// global entity have the key, the pinned one wins, so a personality's own page shadows the shared
// one.
func (d *Datastore) resolveAgentEntity(ctx context.Context, userID, activePersonalityID uuid.UUID, key string) (*ent.AgentEntity, error) {
	rows, err := d.dbClient.AgentEntity.Query().
		Where(visibleAgentEntities(userID, activePersonalityID), agententity.KeyEQ(key)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	var found *ent.AgentEntity
	for _, row := range rows {
		if found == nil || row.PinnedPersonalityID != nil {
			found = row
		}
	}
	if found == nil {
		return nil, ErrAgentEntityNotFound
	}
	return found, nil
}

// GetAgentEntity returns the entity named key that a conversation with activePersonalityID sees.
// key is normalized here. It returns ErrAgentEntityNotFound when there is none.
func (d *Datastore) GetAgentEntity(ctx context.Context, userID, activePersonalityID uuid.UUID, key string) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	row, err := d.resolveAgentEntity(ctx, userID, activePersonalityID, norm)
	if err != nil {
		if !errors.Is(err, ErrAgentEntityNotFound) {
			d.logger.Error("failed to get entity", zap.Error(err))
		}
		return nil, err
	}
	m := toAgentEntityModel(row)
	m.UserID = userID
	return m, nil
}

// CreateAgentEntity writes a new entity named key. It is pinned to the writer's personality when
// that personality has auto_pin_memories on, and global otherwise. It returns ErrAgentEntityExists
// when the writer can already see an entity with the key (global or pinned), so a personality never
// silently shadows a shared page.
func (d *Datastore) CreateAgentEntity(ctx context.Context, userID uuid.UUID, w models.AgentEntityWriter, key, body string) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	if err := models.CheckEntityBody(body); err != nil {
		return nil, err
	}
	var pinned *uuid.UUID
	if w.ActivePersonalityID != uuid.Nil {
		p, err := d.dbClient.Personality.Query().
			Where(entpersonality.ID(w.ActivePersonalityID), entpersonality.HasUserWith(user.ID(userID))).
			Only(ctx)
		if ent.IsNotFound(err) {
			return nil, ErrPersonalityNotFound
		}
		if err != nil {
			d.logger.Error("failed to load the personality for an entity write", zap.Error(err))
			return nil, err
		}
		if p.AutoPinMemories {
			pinned = &p.ID
		}
	}
	if _, err := d.resolveAgentEntity(ctx, userID, w.ActivePersonalityID, norm); err == nil {
		return nil, ErrAgentEntityExists
	} else if !errors.Is(err, ErrAgentEntityNotFound) {
		d.logger.Error("failed to check for an existing entity", zap.Error(err))
		return nil, err
	}
	create := d.dbClient.AgentEntity.Create().
		SetOwnerID(userID).
		SetKey(norm).
		SetTitle(models.EntityTitle(key)).
		SetBody(body).
		SetNillablePinnedPersonalityID(pinned)
	setAgentEntityWriterOnCreate(create, w)
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		// A concurrent writer took the key between the check and the insert.
		return nil, ErrAgentEntityExists
	}
	if err != nil {
		d.logger.Error("failed to create entity", zap.Error(err))
		return nil, err
	}
	m := toAgentEntityModel(row)
	m.UserID = userID
	return m, nil
}

func setAgentEntityWriterOnCreate(c *ent.AgentEntityCreate, w models.AgentEntityWriter) {
	if w.ActivePersonalityID != uuid.Nil {
		c.SetLastPersonalityID(w.ActivePersonalityID)
	}
	if w.ChatID != uuid.Nil {
		c.SetLastChatID(w.ChatID)
	}
}

// UpdateAgentEntity changes the body of the entity named key that the writer sees. change gets the
// current body and returns the new one (a replacement, or the body with text appended); returning
// an error aborts the update with that error. The write is a compare-and-swap on the body, retried
// a few times, so two concurrent appends both land instead of one overwriting the other.
// It returns ErrAgentEntityNotFound, models.ErrEntityBodyTooLong or ErrAgentEntityConflict.
func (d *Datastore) UpdateAgentEntity(ctx context.Context, userID uuid.UUID, w models.AgentEntityWriter, key string, change func(body string) (string, error)) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < agentEntityUpdateAttempts; attempt++ {
		cur, err := d.resolveAgentEntity(ctx, userID, w.ActivePersonalityID, norm)
		if err != nil {
			if !errors.Is(err, ErrAgentEntityNotFound) {
				d.logger.Error("failed to load entity for update", zap.Error(err))
			}
			return nil, err
		}
		next, err := change(cur.Body)
		if err != nil {
			return nil, err
		}
		if err := models.CheckEntityBody(next); err != nil {
			return nil, err
		}
		upd := d.dbClient.AgentEntity.Update().
			Where(agententity.ID(cur.ID), agententity.BodyEQ(cur.Body), agententity.DeletedAtIsNil()).
			SetBody(next)
		if w.ActivePersonalityID != uuid.Nil {
			upd.SetLastPersonalityID(w.ActivePersonalityID)
		} else {
			upd.ClearLastPersonalityID()
		}
		if w.ChatID != uuid.Nil {
			upd.SetLastChatID(w.ChatID)
		} else {
			upd.ClearLastChatID()
		}
		n, err := upd.Save(ctx)
		if err != nil {
			d.logger.Error("failed to update entity", zap.Error(err))
			return nil, err
		}
		if n == 1 {
			row, err := d.dbClient.AgentEntity.Get(ctx, cur.ID)
			if err != nil {
				return nil, err
			}
			m := toAgentEntityModel(row)
			m.UserID = userID
			return m, nil
		}
	}
	return nil, ErrAgentEntityConflict
}

// ListAgentEntities lists the user's entities, newest change first, for the Memories panel. It
// returns the page and the total matching count.
func (d *Datastore) ListAgentEntities(ctx context.Context, userID uuid.UUID, f models.AgentEntityFilters) ([]*models.AgentEntity, int, error) {
	q := d.dbClient.AgentEntity.Query().Where(liveAgentEntities(userID))
	if f.Query != nil && *f.Query != "" {
		q = q.Where(agententity.Or(
			agententity.KeyContainsFold(*f.Query),
			agententity.TitleContainsFold(*f.Query),
			agententity.BodyContainsFold(*f.Query),
		))
	}
	if f.PinnedPersonalityID != nil {
		if *f.PinnedPersonalityID == uuid.Nil {
			q = q.Where(agententity.PinnedPersonalityIDIsNil())
		} else {
			q = q.Where(agententity.PinnedPersonalityIDEQ(*f.PinnedPersonalityID))
		}
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		d.logger.Error("failed to count entities", zap.Error(err))
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := q.
		WithPinnedPersonality().
		Order(ent.Desc(agententity.FieldUpdatedAt), ent.Asc(agententity.FieldID)).
		Limit(limit).
		Offset(max(f.Offset, 0)).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list entities", zap.Error(err))
		return nil, 0, err
	}
	out := make([]*models.AgentEntity, 0, len(rows))
	for _, row := range rows {
		m := toAgentEntityModel(row)
		m.UserID = userID
		out = append(out, m)
	}
	return out, total, nil
}

// SetAgentEntityBody replaces the body of the user's entity id from the UI. The edit is the
// user's, so the agent provenance is cleared. It returns ErrAgentEntityNotFound or
// models.ErrEntityBodyTooLong.
func (d *Datastore) SetAgentEntityBody(ctx context.Context, userID, id uuid.UUID, body string) (*models.AgentEntity, error) {
	if err := models.CheckEntityBody(body); err != nil {
		return nil, err
	}
	n, err := d.dbClient.AgentEntity.Update().
		Where(agententity.ID(id), liveAgentEntities(userID)).
		SetBody(body).
		ClearLastPersonalityID().
		ClearLastChatID().
		Save(ctx)
	if err != nil {
		d.logger.Error("failed to set entity body", zap.Error(err))
		return nil, err
	}
	if n == 0 {
		return nil, ErrAgentEntityNotFound
	}
	row, err := d.dbClient.AgentEntity.Query().Where(agententity.ID(id)).WithPinnedPersonality().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("reload entity: %w", err)
	}
	m := toAgentEntityModel(row)
	m.UserID = userID
	return m, nil
}

// DeleteAgentEntity deletes the user's entity id from the UI. The user's delete removes the row
// (a tombstone too). It returns ErrAgentEntityNotFound when id is not one of the user's entities.
func (d *Datastore) DeleteAgentEntity(ctx context.Context, userID, id uuid.UUID) error {
	n, err := d.dbClient.AgentEntity.Delete().
		Where(agententity.ID(id), agententity.HasOwnerWith(user.ID(userID))).
		Exec(ctx)
	if err != nil {
		d.logger.Error("failed to delete entity", zap.Error(err))
		return err
	}
	if n == 0 {
		return ErrAgentEntityNotFound
	}
	return nil
}

// agentEntityListScanMax bounds the rows ListVisibleAgentEntities reads to page and de-duplicate.
const agentEntityListScanMax = 1000

// ListVisibleAgentEntities lists the live entities a conversation with activePersonalityID sees
// whose normalized key starts with prefix ("" lists them all), in key order, with the total. A key
// held both globally and by the personality is listed once, as the pinned entity, the one a read
// returns. Bodies are not loaded.
func (d *Datastore) ListVisibleAgentEntities(ctx context.Context, userID, activePersonalityID uuid.UUID, prefix string, limit int) ([]*models.AgentEntity, int, error) {
	q := d.dbClient.AgentEntity.Query().Where(visibleAgentEntities(userID, activePersonalityID))
	if norm := strings.ToLower(strings.Join(strings.Fields(prefix), " ")); norm != "" {
		q = q.Where(agententity.KeyHasPrefix(norm))
	}
	rows, err := q.
		Select(agententity.FieldKey, agententity.FieldTitle, agententity.FieldPinnedPersonalityID, agententity.FieldCreatedAt, agententity.FieldUpdatedAt).
		Order(ent.Asc(agententity.FieldKey)).
		Limit(agentEntityListScanMax).
		All(ctx)
	if err != nil {
		d.logger.Error("failed to list visible entities", zap.Error(err))
		return nil, 0, err
	}
	// Rows of one key are adjacent; keep one per key, the pinned one when there are two.
	var keep []*ent.AgentEntity
	for _, row := range rows {
		if n := len(keep); n > 0 && keep[n-1].Key == row.Key {
			if row.PinnedPersonalityID != nil {
				keep[n-1] = row
			}
			continue
		}
		keep = append(keep, row)
	}
	out := make([]*models.AgentEntity, 0, min(len(keep), max(limit, 0)))
	for _, row := range keep[:min(len(keep), max(limit, 0))] {
		m := toAgentEntityModel(row)
		m.UserID = userID
		out = append(out, m)
	}
	return out, len(keep), nil
}

// TombstoneAgentEntity deletes the entity named key that the writer sees, as an agent: the row is
// kept with deleted_at set and the writer recorded, hidden from every read, listing and the UI, so
// the delete can be audited or undone. The key is free again at once. It returns the tombstone, or
// ErrAgentEntityNotFound.
func (d *Datastore) TombstoneAgentEntity(ctx context.Context, userID uuid.UUID, w models.AgentEntityWriter, key string) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	cur, err := d.resolveAgentEntity(ctx, userID, w.ActivePersonalityID, norm)
	if err != nil {
		if !errors.Is(err, ErrAgentEntityNotFound) {
			d.logger.Error("failed to load entity for delete", zap.Error(err))
		}
		return nil, err
	}
	upd := d.dbClient.AgentEntity.Update().
		Where(agententity.ID(cur.ID), agententity.DeletedAtIsNil()).
		SetDeletedAt(time.Now())
	if w.ActivePersonalityID != uuid.Nil {
		upd.SetLastPersonalityID(w.ActivePersonalityID)
	} else {
		upd.ClearLastPersonalityID()
	}
	if w.ChatID != uuid.Nil {
		upd.SetLastChatID(w.ChatID)
	} else {
		upd.ClearLastChatID()
	}
	n, err := upd.Save(ctx)
	if err != nil {
		d.logger.Error("failed to delete entity", zap.Error(err))
		return nil, err
	}
	if n == 0 {
		return nil, ErrAgentEntityNotFound // deleted by a concurrent writer
	}
	row, err := d.dbClient.AgentEntity.Get(ctx, cur.ID)
	if err != nil {
		return nil, fmt.Errorf("reload entity: %w", err)
	}
	m := toAgentEntityModel(row)
	m.UserID = userID
	return m, nil
}
