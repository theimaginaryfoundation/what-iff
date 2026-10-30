package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeEntityStore keeps entities by normalized key, all global.
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

func TestEntityTools(t *testing.T) {
	store := &fakeEntityStore{byKey: map[string]*models.AgentEntity{}}
	tool := &EntityTool{store: store, logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: uuid.New()}

	res := callJSON(t, tool.Write, chat, map[string]any{"action": "write", "name": "Chris  Smith", "text": "Works with [[Dana Lee]] on [[Project Y]] and [[dana lee]]."})
	requireOK(t, res)
	require.Equal(t, models.AgentEntityWriter{ActivePersonalityID: chat.PersonalityID, ChatID: chat.ID}, store.lastWrite)
	page := res["entity"].(map[string]any)
	require.Equal(t, "Chris Smith", page["name"])
	require.Nil(t, page["body"], "a write does not echo the body back")
	require.Equal(t, []any{"Dana Lee", "Project Y"}, page["links"], "links are de-duplicated by normalized name")

	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "write", "name": "chris smith", "text": "x"}), "already exists")
	requireOK(t, callJSON(t, tool.Write, chat, map[string]any{"action": "append", "name": "CHRIS SMITH", "text": "Likes tea."}))
	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "append", "name": "nobody", "text": "x"}), "no entity named")
	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "append", "name": "chris smith", "text": strings.Repeat("x", models.MaxEntityBodyRunes)}), "condense")

	got := callJSON(t, tool.Get, chat, map[string]any{"names": []string{"chris smith", "Dana Lee"}})
	requireOK(t, got)
	entities := got["entities"].([]any)
	chris := entities[0].(map[string]any)
	require.Equal(t, true, chris["found"])
	require.Equal(t, "Works with [[Dana Lee]] on [[Project Y]] and [[dana lee]].\nLikes tea.", chris["body"])
	require.Equal(t, "global", chris["scope"])
	dana := entities[1].(map[string]any)
	require.Equal(t, false, dana["found"])
	require.Equal(t, "No entity with this name.", dana["note"])

	requireOK(t, callJSON(t, tool.Write, chat, map[string]any{"action": "update", "name": "chris smith", "text": "Moved to Oslo."}))
	require.Equal(t, "Moved to Oslo.", store.byKey["chris smith"].Body)

	requireRefused(t, callJSON(t, tool.Write, chat, map[string]any{"action": "delete", "name": "chris smith", "text": "x"}), "action must be")
	sandboxed := *chat
	sandboxed.ContextScope = models.ContextScopeSandbox
	requireRefused(t, callJSON(t, tool.Get, &sandboxed, map[string]any{"names": []string{"x"}}), "sandboxed")
}
