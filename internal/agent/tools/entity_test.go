package tools

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeEntityStore keeps entities by normalized key, all user-scoped.
type fakeEntityStore struct {
	byKey     map[string]*models.AgentEntity
	lastWrite models.AgentEntityWriter
}

func (f *fakeEntityStore) GetAgentEntity(_ context.Context, _, _ uuid.UUID, key string) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	e, ok := f.byKey[norm]
	if !ok {
		return nil, datastore.ErrAgentEntityNotFound
	}
	c := *e
	return &c, nil
}
func (f *fakeEntityStore) CreateAgentEntity(_ context.Context, _ uuid.UUID, w models.AgentEntityWriter, key, body string) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	if err := models.CheckEntityBody(body); err != nil {
		return nil, err
	}
	if _, ok := f.byKey[norm]; ok {
		return nil, datastore.ErrAgentEntityExists
	}
	f.lastWrite = w
	e := &models.AgentEntity{ID: uuid.New(), Key: norm, Title: models.EntityTitle(key), Body: body}
	f.byKey[norm] = e
	return e, nil
}
func (f *fakeEntityStore) UpdateAgentEntity(_ context.Context, _ uuid.UUID, w models.AgentEntityWriter, key string, change func(string) (string, error)) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	e, ok := f.byKey[norm]
	if !ok {
		return nil, datastore.ErrAgentEntityNotFound
	}
	next, err := change(e.Body)
	if err != nil {
		return nil, err
	}
	if err := models.CheckEntityBody(next); err != nil {
		return nil, err
	}
	f.lastWrite = w
	e.Body = next
	return e, nil
}

func (f *fakeEntityStore) ArchiveAgentEntity(_ context.Context, _ uuid.UUID, w models.AgentEntityWriter, key string) (*models.AgentEntity, error) {
	norm, err := models.NormalizeEntityKey(key)
	if err != nil {
		return nil, err
	}
	e, ok := f.byKey[norm]
	if !ok {
		return nil, datastore.ErrAgentEntityNotFound
	}
	delete(f.byKey, norm)
	f.lastWrite = w
	now := time.Now()
	e.ArchivedAt = &now
	return e, nil
}
func (f *fakeEntityStore) ListVisibleAgentEntities(_ context.Context, _, _ uuid.UUID, prefix string, limit int) ([]*models.AgentEntity, int, error) {
	norm := strings.ToLower(strings.Join(strings.Fields(prefix), " "))
	var keys []string
	for k := range f.byKey {
		if strings.HasPrefix(k, norm) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []*models.AgentEntity
	for _, k := range keys[:min(len(keys), limit)] {
		out = append(out, f.byKey[k])
	}
	return out, len(keys), nil
}

func TestEntityTools(t *testing.T) {
	store := &fakeEntityStore{byKey: map[string]*models.AgentEntity{}}
	tool := &EntityTool{store: store, logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}

	res := callJSON(t, tool.Write, chat, map[string]any{"action": "write", "name": "Alex  Rivera", "text": "Works with [[Dana Lee]] on [[Project Orchard]] and [[dana lee]]."})
	requireOK(t, res)
	require.Equal(t, models.AgentEntityWriter{ActivePersonalityID: chat.PersonalityID, ChatID: chat.ID}, store.lastWrite)
	page := res["entity"].(map[string]any)
	require.Equal(t, "Alex Rivera", page["name"])
	require.Nil(t, page["body"], "a write does not echo the body back")
	require.Equal(t, []any{"Dana Lee", "Project Orchard"}, page["links"], "links are de-duplicated by normalized name")

	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "write", "name": "alex rivera", "text": "x"}), "already exists")
	requireOK(t, callJSON(t, tool.Write, chat, map[string]any{"action": "append", "name": "ALEX RIVERA", "text": "Likes tea."}))
	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "append", "name": "nobody", "text": "x"}), "no entity named")
	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "append", "name": "alex rivera", "text": strings.Repeat("x", models.MaxEntityBodyRunes)}), "condense")

	got := callJSON(t, tool.Get, chat, map[string]any{"names": []string{"alex rivera", "Dana Lee"}})
	requireOK(t, got)
	entities := got["entities"].([]any)
	alex := entities[0].(map[string]any)
	require.Equal(t, true, alex["found"])
	require.Equal(t, "Works with [[Dana Lee]] on [[Project Orchard]] and [[dana lee]].\nLikes tea.", alex["body"])
	require.Equal(t, "user", alex["scope"])
	dana := entities[1].(map[string]any)
	require.Equal(t, false, dana["found"])
	require.Equal(t, "No entity with this name.", dana["note"])

	requireOK(t, callJSON(t, tool.Write, chat, map[string]any{"action": "update", "name": "alex rivera", "text": "Moved to Oslo."}))
	require.Equal(t, "Moved to Oslo.", store.byKey["alex rivera"].Body)

	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "remove", "name": "alex rivera", "text": "x"}), "action must be")
	sandboxed := *chat
	sandboxed.ContextScope = models.ContextScopeSandbox
	requireRefused(t, callJSON(t, tool.Get, &sandboxed, map[string]any{"names": []string{"x"}}), "sandboxed")
}

func TestEntityTools_PrefixLookupAndDelete(t *testing.T) {
	store := &fakeEntityStore{byKey: map[string]*models.AgentEntity{}}
	tool := &EntityTool{store: store, logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}
	for _, name := range []string{"Project Alpha", "Project Beta", "Alex Rivera"} {
		requireOK(t, callJSON(t, tool.Write, chat, map[string]any{"action": "write", "name": name, "text": "x"}))
	}

	res := callJSON(t, tool.Get, chat, map[string]any{"prefix": "project"})
	requireOK(t, res)
	require.Equal(t, "prefix", res["match_type"])
	require.EqualValues(t, 2, res["total"])
	matches := res["matches"].([]any)
	first := matches[0].(map[string]any)
	require.Equal(t, "Project Alpha", first["name"])
	require.Equal(t, "user", first["scope"])
	require.Nil(t, first["body"], "a prefix lookup lists names, not pages")

	require.Contains(t, callJSON(t, tool.Get, chat, map[string]any{"prefix": "zed"})["note"], "No entity name starts with")
	requireRefused(t, callJSON(t, tool.Get, chat, map[string]any{"names": []string{"Alex Rivera"}, "prefix": "pro"}), "not both")
	requireRefused(t, callJSON(t, tool.Get, chat, map[string]any{}), "pass names")
	// An empty prefix sent next to names is ignored, since models often send every property.
	requireOK(t, callJSON(t, tool.Get, chat, map[string]any{"names": []string{"Alex Rivera"}, "prefix": ""}))

	// Delete is exact-name only; a prefix is not a name.
	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "delete", "name": "Project"}), "nothing was archived")
	del := callJSON(t, tool.Write, chat, map[string]any{"action": "delete", "name": "project beta"})
	requireOK(t, del)
	page := del["entity"].(map[string]any)
	require.Equal(t, "Project Beta", page["name"])
	require.NotEmpty(t, page["archived_at"])
	require.Contains(t, page["note"], "restore")
	require.Equal(t, false, page["found"])
	require.EqualValues(t, 1, callJSON(t, tool.Get, chat, map[string]any{"prefix": "project"})["total"])
}
