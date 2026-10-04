package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// memEntityStore mirrors the datastore's entity rules in memory: names are unique per (owner,
// scope), updates need the current revision, archiving frees names.
type memEntityStore struct {
	mu        sync.Mutex
	entities  map[uuid.UUID]*models.Entity
	aliasLoad int
}

func newMemEntityStore() *memEntityStore {
	return &memEntityStore{entities: map[uuid.UUID]*models.Entity{}}
}

func scopeOf(e *models.Entity) uuid.UUID {
	if e.PinnedPersonalityID == nil {
		return uuid.Nil
	}
	return *e.PinnedPersonalityID
}

func visible(e *models.Entity, userID, personalityID uuid.UUID, limit models.MemorySensitivity) bool {
	return e.UserID == userID && e.State == models.EntityStateActive &&
		e.Sensitivity.AllowedUnder(limit) &&
		(e.PinnedPersonalityID == nil || *e.PinnedPersonalityID == personalityID)
}

func namesOf(e *models.Entity) []string {
	out := []string{models.NormalizeEntityName(e.Name)}
	for _, a := range e.Aliases {
		out = append(out, models.NormalizeEntityName(a))
	}
	return out
}

func (s *memEntityStore) ListEntityAliasesForScope(_ context.Context, userID, personalityID uuid.UUID, _ int, limit models.MemorySensitivity) ([]models.EntityAliasMatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aliasLoad++
	var out []models.EntityAliasMatch
	for _, e := range s.entities {
		if !visible(e, userID, personalityID, limit) {
			continue
		}
		for _, n := range namesOf(e) {
			out = append(out, models.EntityAliasMatch{EntityID: e.ID, AliasNorm: n, Pinned: e.PinnedPersonalityID != nil})
		}
	}
	return out, nil
}

func (s *memEntityStore) GetEntitiesByIDs(_ context.Context, userID uuid.UUID, ids []uuid.UUID, limit models.MemorySensitivity) ([]*models.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.Entity
	for _, id := range ids {
		if e, ok := s.entities[id]; ok && e.UserID == userID && e.State == models.EntityStateActive && e.Sensitivity.AllowedUnder(limit) {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *memEntityStore) FindEntityByName(_ context.Context, userID, personalityID uuid.UUID, name string, limit models.MemorySensitivity) (*models.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	norm := models.NormalizeEntityName(name)
	var found *models.Entity
	for _, e := range s.entities {
		if !visible(e, userID, personalityID, limit) {
			continue
		}
		for _, n := range namesOf(e) {
			if n == norm && (found == nil || e.PinnedPersonalityID != nil) {
				found = e
			}
		}
	}
	if found == nil {
		return nil, datastore.ErrEntityNotFound
	}
	cp := *found
	return &cp, nil
}

func (s *memEntityStore) ListEntities(_ context.Context, userID, personalityID uuid.UUID, filter string, limit int, maxSensitivity models.MemorySensitivity) ([]*models.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.Entity
	for _, e := range s.entities {
		if visible(e, userID, personalityID, maxSensitivity) && strings.Contains(strings.ToLower(e.Name), strings.ToLower(filter)) {
			cp := *e
			out = append(out, &cp)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *memEntityStore) CountActiveEntities(_ context.Context, userID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entities {
		if e.UserID == userID && e.State == models.EntityStateActive {
			n++
		}
	}
	return n, nil
}

func (s *memEntityStore) SaveEntity(_ context.Context, userID uuid.UUID, existingID *uuid.UUID, baseRevision int, in models.EntityInput) (*models.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := &models.Entity{Name: in.Name, Aliases: in.Aliases, PinnedPersonalityID: in.PinnedPersonalityID}
	for _, other := range s.entities {
		if other.UserID != userID || other.State != models.EntityStateActive || scopeOf(other) != scopeOf(candidate) {
			continue
		}
		if existingID != nil && other.ID == *existingID {
			continue
		}
		for _, mine := range namesOf(candidate) {
			for _, theirs := range namesOf(other) {
				if mine == theirs {
					return nil, &datastore.EntityAliasTakenError{Alias: mine, Owner: other.Name}
				}
			}
		}
	}
	if existingID == nil {
		e := &models.Entity{ID: uuid.New(), UserID: userID, Name: in.Name, Type: in.Type, Card: in.Card, Aliases: in.Aliases,
			Revision: 1, State: models.EntityStateActive, AuthorClass: in.AuthorClass, PinnedPersonalityID: in.PinnedPersonalityID, Sensitivity: in.Sensitivity.OrDefault(), CardUpdatedAt: time.Now()}
		s.entities[e.ID] = e
		cp := *e
		return &cp, nil
	}
	e, ok := s.entities[*existingID]
	if !ok {
		return nil, datastore.ErrEntityNotFound
	}
	if e.Revision != baseRevision {
		return nil, &datastore.EntityConflictError{Current: e.Revision}
	}
	e.Name, e.Type, e.Card, e.Aliases, e.AuthorClass = in.Name, in.Type, in.Card, in.Aliases, in.AuthorClass
	if in.Sensitivity.Valid() {
		e.Sensitivity = in.Sensitivity
	}
	e.Revision++
	cp := *e
	return &cp, nil
}

func (s *memEntityStore) ArchiveEntity(_ context.Context, _ uuid.UUID, id uuid.UUID, baseRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entities[id]
	if !ok {
		return datastore.ErrEntityNotFound
	}
	if e.Revision != baseRevision {
		return &datastore.EntityConflictError{Current: e.Revision}
	}
	e.State = models.EntityStateArchived
	e.Revision++
	return nil
}

type entityFixture struct {
	tool  *EntityTool
	store *memEntityStore
	chat  *models.Chat
}

func newEntityFixture() *entityFixture {
	store := newMemEntityStore()
	return &entityFixture{
		tool:  NewEntityTool(store, zap.NewNop()),
		store: store,
		chat:  &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()},
	}
}

func (f *entityFixture) remember(t *testing.T, args map[string]interface{}) entityResult {
	t.Helper()
	in, err := json.Marshal(args)
	require.NoError(t, err)
	out, err := f.tool.RememberEntity(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res entityResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func (f *entityFixture) recall(t *testing.T, name string) entityResult {
	t.Helper()
	in, _ := json.Marshal(map[string]string{"name": name})
	out, err := f.tool.RecallEntity(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res entityResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func (f *entityFixture) spot(t *testing.T, text string) []string {
	t.Helper()
	found, err := f.tool.Spot(context.Background(), f.chat.UserID, f.chat.PersonalityID, text, entitySpotMaxCards, f.chat.MemoryLimit())
	require.NoError(t, err)
	var names []string
	for _, e := range found {
		names = append(names, e.Name)
	}
	return names
}

func TestNormalizeEntityName(t *testing.T) {
	assert.Equal(t, "john o neil", models.NormalizeEntityName("  John   O'Neil! "))
	assert.Equal(t, "italy trip 2026", models.NormalizeEntityName("Italy-Trip (2026)"))
	assert.Equal(t, "josé", models.NormalizeEntityName("JOSÉ"))
	assert.Equal(t, "", models.NormalizeEntityName("!!! ---"))
}

func TestRememberEntity_CreateRecallUpdateForget(t *testing.T) {
	f := newEntityFixture()

	created := f.remember(t, map[string]interface{}{"name": "John Park", "type": "person", "card": "the user's manager at Acme.", "aliases": []string{"John", "my boss"}})
	require.True(t, created.Success, created.Error)
	assert.Equal(t, "create", created.Op)
	assert.Equal(t, 1, created.Entity.Revision)

	byAlias := f.recall(t, "my boss")
	require.True(t, byAlias.Success, byAlias.Error)
	assert.Equal(t, "John Park", byAlias.Entity.Name)

	noBase := f.remember(t, map[string]interface{}{"name": "John", "card": "On leave until Oct 14."})
	assert.False(t, noBase.Success)
	assert.Contains(t, noBase.Error, "base_revision")
	require.NotNil(t, noBase.Entity, "the refusal shows the current card so the agent can merge")

	stale := f.remember(t, map[string]interface{}{"name": "John", "card": "x", "base_revision": 7})
	assert.True(t, stale.Conflict)
	assert.Equal(t, 1, stale.CurrentRevision)

	updated := f.remember(t, map[string]interface{}{"name": "John", "card": "Manager at Acme; on leave until Oct 14.", "base_revision": 1})
	require.True(t, updated.Success, updated.Error)
	assert.Equal(t, "update", updated.Op)
	assert.Equal(t, "John Park", updated.Entity.Name, "updating through an alias keeps the canonical name")
	assert.Equal(t, "person", updated.Entity.Type, "fields not given keep their values")
	assert.ElementsMatch(t, []string{"John", "my boss"}, updated.Entity.Aliases)

	forgot := f.remember(t, map[string]interface{}{"name": "John Park", "forget": true, "base_revision": 2})
	require.True(t, forgot.Success, forgot.Error)
	assert.False(t, f.recall(t, "John").Success)
}

func TestRememberEntity_Validation(t *testing.T) {
	f := newEntityFixture()
	assert.Contains(t, f.remember(t, map[string]interface{}{"name": "Vex"}).Error, "card is required")
	assert.Contains(t, f.remember(t, map[string]interface{}{"name": "???", "card": "x"}).Error, "letters or digits")
	assert.Contains(t, f.remember(t, map[string]interface{}{"name": "Vex", "card": strings.Repeat("a", entityMaxCardChars+1)}).Error, "too long")
	tooMany := make([]string, entityMaxAliases+1)
	for i := range tooMany {
		tooMany[i] = "alias" + string(rune('a'+i))
	}
	assert.Contains(t, f.remember(t, map[string]interface{}{"name": "Vex", "card": "x", "aliases": tooMany}).Error, "too many aliases")
	assert.Contains(t, f.remember(t, map[string]interface{}{"name": "Nobody", "forget": true, "base_revision": 1}).Error, "no entity")

	require.True(t, f.remember(t, map[string]interface{}{"name": "Vex", "card": "ranger"}).Success)
	assert.Contains(t, f.remember(t, map[string]interface{}{"name": "Vex", "forget": true, "base_revision": 0}).Error, "base_revision")
}

func TestRememberEntity_NameClashAndPersonalityScope(t *testing.T) {
	f := newEntityFixture()
	require.True(t, f.remember(t, map[string]interface{}{"name": "Alex", "card": "Sam's spouse", "aliases": []string{"Ari"}}).Success)

	clash := f.remember(t, map[string]interface{}{"name": "Ariadne", "card": "x", "aliases": []string{"Ari"}})
	assert.False(t, clash.Success)
	assert.Contains(t, clash.Error, "already belongs to Alex")

	// A personality-only entity may reuse a shared name, and wins for that personality.
	pinned := f.remember(t, map[string]interface{}{"name": "Ari", "card": "the campaign's rogue", "personality_only": true})
	require.False(t, pinned.Success, "Ari resolves to the shared Alex first, so this is an update without base_revision")

	otherName := f.remember(t, map[string]interface{}{"name": "Grog", "card": "the campaign's barbarian", "personality_only": true})
	require.True(t, otherName.Success, otherName.Error)
	assert.True(t, otherName.Entity.PersonalityOnly)

	otherPersona := *f.chat
	otherPersona.PersonalityID = uuid.New()
	f.chat = &otherPersona
	assert.False(t, f.recall(t, "Grog").Success, "a personality-only entity is invisible to other personalities")
	assert.True(t, f.recall(t, "Alex").Success, "shared entities are visible to every personality")
}

func TestSpot_MatchesNamesAndAliasesInOrder(t *testing.T) {
	f := newEntityFixture()
	require.True(t, f.remember(t, map[string]interface{}{"name": "John Park", "card": "manager", "aliases": []string{"John", "my boss"}}).Success)
	require.True(t, f.remember(t, map[string]interface{}{"name": "Italy Trip 2026", "card": "May 12-26", "aliases": []string{"the Italy trip"}}).Success)
	require.True(t, f.remember(t, map[string]interface{}{"name": "Will", "card": "neighbour"}).Success)

	assert.Equal(t, []string{"Italy Trip 2026", "John Park"}, f.spot(t, "Did the Italy trip come up with my boss? John said..."),
		"order of first mention; each entity once; multi-word aliases match")
	assert.Empty(t, f.spot(t, "I will ask about it tomorrow"), "a common word only matches when capitalized")
	assert.Equal(t, []string{"Will"}, f.spot(t, "Ask Will about it"))
	assert.Empty(t, f.spot(t, "Johnny and Parker went out"), "names match whole words only")
	assert.Equal(t, []string{"John Park"}, f.spot(t, "john park, call me"), "case-insensitive for names that aren't common words")
}

func TestSpot_CachesAndInvalidatesOnWrite(t *testing.T) {
	f := newEntityFixture()
	require.True(t, f.remember(t, map[string]interface{}{"name": "Vex", "card": "ranger"}).Success)
	f.spot(t, "Vex")
	f.spot(t, "Vex again")
	assert.Equal(t, 1, f.store.aliasLoad, "names are loaded once and cached")

	require.True(t, f.remember(t, map[string]interface{}{"name": "Grog", "card": "barbarian"}).Success)
	assert.Equal(t, []string{"Grog"}, f.spot(t, "Grog smash"), "a write invalidates this process's cache immediately")
}

func TestSpot_CapsCards(t *testing.T) {
	f := newEntityFixture()
	var text []string
	for i := 0; i < entitySpotMaxCards+3; i++ {
		name := "Person" + string(rune('A'+i))
		require.True(t, f.remember(t, map[string]interface{}{"name": name, "card": "x"}).Success)
		text = append(text, name)
	}
	assert.Len(t, f.spot(t, strings.Join(text, " ")), entitySpotMaxCards)
}

func TestRenderEntityCards(t *testing.T) {
	out := RenderEntityCards([]*models.Entity{
		{Name: "John Park", Type: "person", Aliases: []string{"John", "my boss"}, Card: "Manager at Acme.\n  On leave."},
		{Name: "Vex", Card: ""},
	})
	assert.Contains(t, out, "- John Park (person; aka John, my boss): Manager at Acme. On leave.")
	assert.Contains(t, out, "- Vex")
	assert.Empty(t, RenderEntityCards(nil))
}

func TestListEntities(t *testing.T) {
	f := newEntityFixture()
	require.True(t, f.remember(t, map[string]interface{}{"name": "Vex", "type": "npc", "card": "ranger", "aliases": []string{"Vex'ahlia"}}).Success)
	list := &ListTool{logger: zap.NewNop()}
	list.SetEntities(f.tool)

	in, _ := json.Marshal(map[string]interface{}{"kind": "entities"})
	out, err := list.List(context.Background(), f.chat, in)
	require.NoError(t, err)
	var res listResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Items, 1)
	assert.Equal(t, "Vex", res.Items[0].Name)
	assert.Equal(t, "npc; aka Vex'ahlia", res.Items[0].Description)
	assert.Equal(t, 1, res.Items[0].Revision)
}
