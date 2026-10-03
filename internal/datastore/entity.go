package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/entity"
	"github.com/theimaginaryfoundation/what-iff/ent/entityalias"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ErrEntityNotFound is returned when no active entity matches.
var ErrEntityNotFound = errors.New("entity not found")

// EntityConflictError reports an edit refused because the entity changed since the writer read it.
type EntityConflictError struct {
	Current int
}

func (e *EntityConflictError) Error() string {
	return fmt.Sprintf("entity changed: current revision is %d", e.Current)
}

// EntityAliasTakenError reports a name already used by another entity in the same scope.
type EntityAliasTakenError struct {
	Alias string
	Owner string
}

func (e *EntityAliasTakenError) Error() string {
	return fmt.Sprintf("the name %q already belongs to %q", e.Alias, e.Owner)
}

func entityScopeKey(pinned *uuid.UUID) uuid.UUID {
	if pinned == nil {
		return uuid.Nil
	}
	return *pinned
}

func toEntityModel(e *ent.Entity, userID uuid.UUID) *models.Entity {
	if e == nil {
		return nil
	}
	m := &models.Entity{
		ID:                  e.ID,
		UserID:              userID,
		Name:                e.Name,
		Type:                e.Type,
		Card:                e.Card,
		Revision:            e.Revision,
		State:               string(e.State),
		AuthorClass:         string(e.AuthorClass),
		PinnedPersonalityID: e.PinnedPersonalityID,
		CardUpdatedAt:       e.CardUpdatedAt,
		CreatedAt:           e.CreatedAt,
		UpdatedAt:           e.UpdatedAt,
	}
	for _, a := range e.Edges.Aliases {
		if models.NormalizeEntityName(a.Alias) != models.NormalizeEntityName(e.Name) {
			m.Aliases = append(m.Aliases, a.Alias)
		}
	}
	return m
}

// ListEntityAliasesForScope returns every matchable name of the active entities a personality can
// see: the user's unpinned entities plus those pinned to personalityID. limit caps the result.
func (d *Datastore) ListEntityAliasesForScope(ctx context.Context, userID, personalityID uuid.UUID, limit int) ([]models.EntityAliasMatch, error) {
	scopes := []uuid.UUID{uuid.Nil}
	if personalityID != uuid.Nil {
		scopes = append(scopes, personalityID)
	}
	rows, err := d.dbClient.EntityAlias.Query().
		Where(
			entityalias.OwnerID(userID),
			entityalias.ScopeKeyIn(scopes...),
			entityalias.HasEntityWith(entity.StateEQ(entity.StateActive)),
		).
		WithEntity(func(q *ent.EntityQuery) { q.Select(entity.FieldID) }).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.EntityAliasMatch, 0, len(rows))
	for _, r := range rows {
		if r.Edges.Entity == nil {
			continue
		}
		out = append(out, models.EntityAliasMatch{EntityID: r.Edges.Entity.ID, AliasNorm: r.AliasNorm, Pinned: r.ScopeKey != uuid.Nil})
	}
	return out, nil
}

// GetEntitiesByIDs loads active entities (with aliases) owned by userID, in the order given.
func (d *Datastore) GetEntitiesByIDs(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]*models.Entity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.dbClient.Entity.Query().
		Where(
			entity.IDIn(ids...),
			entity.HasOwnerWith(user.ID(userID)),
			entity.StateEQ(entity.StateActive),
		).
		WithAliases().
		All(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*ent.Entity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]*models.Entity, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, toEntityModel(r, userID))
		}
	}
	return out, nil
}

// FindEntityByName resolves a name (any alias) to the active entity a personality can see,
// preferring one pinned to that personality over an unpinned one.
func (d *Datastore) FindEntityByName(ctx context.Context, userID, personalityID uuid.UUID, name string) (*models.Entity, error) {
	norm := models.NormalizeEntityName(name)
	if norm == "" {
		return nil, ErrEntityNotFound
	}
	scopes := []uuid.UUID{uuid.Nil}
	if personalityID != uuid.Nil {
		scopes = append(scopes, personalityID)
	}
	aliases, err := d.dbClient.EntityAlias.Query().
		Where(
			entityalias.OwnerID(userID),
			entityalias.ScopeKeyIn(scopes...),
			entityalias.AliasNorm(norm),
			entityalias.HasEntityWith(entity.StateEQ(entity.StateActive)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return nil, ErrEntityNotFound
	}
	best := aliases[0]
	for _, a := range aliases {
		if a.ScopeKey != uuid.Nil {
			best = a
		}
	}
	row, err := best.QueryEntity().WithAliases().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	return toEntityModel(row, userID), nil
}

// ListEntities lists the active entities a personality can see, by name, optionally filtered by a
// name substring.
func (d *Datastore) ListEntities(ctx context.Context, userID, personalityID uuid.UUID, filter string, limit int) ([]*models.Entity, error) {
	q := d.dbClient.Entity.Query().
		Where(
			entity.HasOwnerWith(user.ID(userID)),
			entity.StateEQ(entity.StateActive),
		)
	if personalityID != uuid.Nil {
		q = q.Where(entity.Or(entity.PinnedPersonalityIDIsNil(), entity.PinnedPersonalityID(personalityID)))
	} else {
		q = q.Where(entity.PinnedPersonalityIDIsNil())
	}
	if f := strings.TrimSpace(filter); f != "" {
		q = q.Where(entity.NameContainsFold(f))
	}
	rows, err := q.WithAliases().Order(ent.Asc(entity.FieldName)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*models.Entity, 0, len(rows))
	for _, r := range rows {
		out = append(out, toEntityModel(r, userID))
	}
	return out, nil
}

// CountActiveEntities counts the user's active entities, for the per-user cap.
func (d *Datastore) CountActiveEntities(ctx context.Context, userID uuid.UUID) (int, error) {
	return d.dbClient.Entity.Query().
		Where(entity.HasOwnerWith(user.ID(userID)), entity.StateEQ(entity.StateActive)).
		Count(ctx)
}

// SaveEntity creates an entity (existingID nil) or updates one, replacing its card, type and
// names. An update must name the revision it was based on; a stale revision is refused with
// EntityConflictError. A name already used by another entity in the same scope is refused with
// EntityAliasTakenError.
func (d *Datastore) SaveEntity(ctx context.Context, userID uuid.UUID, existingID *uuid.UUID, baseRevision int, in models.EntityInput) (*models.Entity, error) {
	names := entityNames(in)
	scope := entityScopeKey(in.PinnedPersonalityID)

	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func(e error) (*models.Entity, error) {
		_ = tx.Rollback()
		return nil, e
	}

	// Refuse names that another entity already uses in this scope, naming the conflict.
	norms := make([]string, 0, len(names))
	for _, n := range names {
		norms = append(norms, n.norm)
	}
	taken := tx.EntityAlias.Query().
		Where(
			entityalias.OwnerID(userID),
			entityalias.ScopeKey(scope),
			entityalias.AliasNormIn(norms...),
		)
	if existingID != nil {
		taken = taken.Where(entityalias.Not(entityalias.HasEntityWith(entity.ID(*existingID))))
	}
	clash, err := taken.WithEntity().First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return rollback(err)
	}
	if clash != nil {
		owner := ""
		if clash.Edges.Entity != nil {
			owner = clash.Edges.Entity.Name
		}
		return rollback(&EntityAliasTakenError{Alias: clash.Alias, Owner: owner})
	}

	now := time.Now().UTC()
	var id uuid.UUID
	if existingID == nil {
		created, err := tx.Entity.Create().
			SetOwnerID(userID).
			SetName(in.Name).
			SetType(in.Type).
			SetCard(in.Card).
			SetAuthorClass(entity.AuthorClass(in.AuthorClass)).
			SetNillablePinnedPersonalityID(in.PinnedPersonalityID).
			SetCardUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return rollback(err)
		}
		id = created.ID
	} else {
		existing, err := tx.Entity.Query().
			Where(entity.ID(*existingID), entity.HasOwnerWith(user.ID(userID)), entity.StateEQ(entity.StateActive)).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return rollback(ErrEntityNotFound)
			}
			return rollback(err)
		}
		if baseRevision != existing.Revision {
			return rollback(&EntityConflictError{Current: existing.Revision})
		}
		n, err := tx.Entity.Update().
			Where(entity.ID(existing.ID), entity.Revision(existing.Revision)).
			SetName(in.Name).
			SetType(in.Type).
			SetCard(in.Card).
			SetAuthorClass(entity.AuthorClass(in.AuthorClass)).
			SetRevision(existing.Revision + 1).
			SetCardUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return rollback(err)
		}
		if n == 0 {
			return rollback(&EntityConflictError{Current: existing.Revision + 1})
		}
		if _, err := tx.EntityAlias.Delete().Where(entityalias.HasEntityWith(entity.ID(existing.ID))).Exec(ctx); err != nil {
			return rollback(err)
		}
		id = existing.ID
	}

	for _, n := range names {
		if _, err := tx.EntityAlias.Create().
			SetEntityID(id).
			SetOwnerID(userID).
			SetScopeKey(scope).
			SetAlias(n.display).
			SetAliasNorm(n.norm).
			Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				return rollback(&EntityAliasTakenError{Alias: n.display})
			}
			return rollback(err)
		}
	}

	row, err := tx.Entity.Query().Where(entity.ID(id)).WithAliases().Only(ctx)
	if err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return toEntityModel(row, userID), nil
}

// ArchiveEntity forgets an entity: it stops matching and its names are freed for reuse, while the
// row (and its last card) is kept. The caller must name the revision it read.
func (d *Datastore) ArchiveEntity(ctx context.Context, userID, id uuid.UUID, baseRevision int) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	n, err := tx.Entity.Update().
		Where(
			entity.ID(id),
			entity.HasOwnerWith(user.ID(userID)),
			entity.StateEQ(entity.StateActive),
			entity.Revision(baseRevision),
		).
		SetState(entity.StateArchived).
		SetRevision(baseRevision + 1).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n == 0 {
		_ = tx.Rollback()
		current, err := d.dbClient.Entity.Query().Where(entity.ID(id), entity.HasOwnerWith(user.ID(userID)), entity.StateEQ(entity.StateActive)).Only(ctx)
		if err != nil {
			return ErrEntityNotFound
		}
		return &EntityConflictError{Current: current.Revision}
	}
	if _, err := tx.EntityAlias.Delete().Where(entityalias.HasEntityWith(entity.ID(id))).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type entityName struct {
	display string
	norm    string
}

// entityNames returns the entity's name followed by its aliases, normalized and deduplicated.
func entityNames(in models.EntityInput) []entityName {
	var out []entityName
	seen := map[string]bool{}
	for _, raw := range append([]string{in.Name}, in.Aliases...) {
		display := strings.TrimSpace(raw)
		norm := models.NormalizeEntityName(display)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, entityName{display: display, norm: norm})
	}
	return out
}
