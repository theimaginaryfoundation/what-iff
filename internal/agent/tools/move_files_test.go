package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

type fakeMoveStore struct {
	called bool
	user   uuid.UUID
	ids    []uuid.UUID
	folder string
	moved  int
	err    error
}

func (f *fakeMoveStore) MoveFileAttachmentsToFolder(_ context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error) {
	f.called, f.user, f.ids, f.folder = true, userID, ids, folder
	if f.moved == 0 && f.err == nil {
		return len(ids), nil
	}
	return f.moved, f.err
}

func runMove(t *testing.T, store *fakeMoveStore, args string) moveFilesResult {
	t.Helper()
	tool := &MoveFilesTool{store: store, logger: zap.NewNop()}
	chat := &models.Chat{UserID: uuid.New()}
	out, err := tool.Move(context.Background(), chat, []byte(args))
	require.NoError(t, err, "a bad argument is a result for the model, not a Go error")
	var res moveFilesResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	if store.called {
		assert.Equal(t, chat.UserID, store.user, "moves are scoped to the chat's owner")
	}
	return res
}

func TestMoveFilesMovesTheNamedImagesToANormalizedFolder(t *testing.T) {
	store := &fakeMoveStore{}
	a, b := uuid.New(), uuid.New()

	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q," %s "],"folder":" Charts / Oura "}`, a, b))

	assert.True(t, res.Success)
	assert.Equal(t, "charts/oura", res.Folder)
	assert.Equal(t, 2, res.Moved)
	assert.Equal(t, 2, res.Requested)
	assert.Empty(t, res.Note)
	assert.Equal(t, []uuid.UUID{a, b}, store.ids)
	assert.Equal(t, "charts/oura", store.folder)
}

func TestMoveFilesAnEmptyFolderMeansTheTopLevel(t *testing.T) {
	store := &fakeMoveStore{}
	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q],"folder":""}`, uuid.New()))
	assert.True(t, res.Success)
	assert.Equal(t, "", store.folder)
}

func TestMoveFilesSaysWhenSomeIDsWereSkipped(t *testing.T) {
	store := &fakeMoveStore{moved: 1}
	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q,%q],"folder":"x"}`, uuid.New(), uuid.New()))
	assert.True(t, res.Success)
	assert.Equal(t, 1, res.Moved)
	assert.Equal(t, 2, res.Requested)
	assert.Contains(t, res.Note, "skipped")
}

func TestMoveFilesRefusesBadArgumentsWithoutTouchingTheStore(t *testing.T) {
	var tooMany []string
	for i := 0; i < moveFilesMaxIDs+1; i++ {
		tooMany = append(tooMany, fmt.Sprintf("%q", uuid.NewString()))
	}
	for name, args := range map[string]string{
		"not json":        `nope`,
		"no ids":          `{"ids":[],"folder":"x"}`,
		"missing ids":     `{"folder":"x"}`,
		"not a uuid":      `{"ids":["cat.png"],"folder":"x"}`,
		"too many":        `{"ids":[` + strings.Join(tooMany, ",") + `],"folder":"x"}`,
		"bad folder":      fmt.Sprintf(`{"ids":[%q],"folder":"../up"}`, uuid.New()),
		"folder too deep": fmt.Sprintf(`{"ids":[%q],"folder":"%s"}`, uuid.New(), strings.Repeat("a/", 9)),
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeMoveStore{}
			res := runMove(t, store, args)
			assert.False(t, res.Success)
			assert.NotEmpty(t, res.Error)
			assert.False(t, store.called)
		})
	}
}

func TestMoveFilesReportsAStoreFailureWithoutLeakingIt(t *testing.T) {
	store := &fakeMoveStore{err: errors.New("pq: connection refused at 10.0.0.5")}
	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q],"folder":"x"}`, uuid.New()))
	assert.False(t, res.Success)
	assert.NotContains(t, res.Error, "10.0.0.5")
}

func TestMoveFilesSpecIsAnAgentDefaultUserToggleableTool(t *testing.T) {
	var found bool
	for _, def := range functionToolCatalog {
		if def.Spec.Name == MoveFilesToolSpec.Name {
			found = true
			assert.True(t, def.AgentDefault)
			assert.True(t, def.UserToggleable, "people can switch it off per thread like the other tools")
		}
	}
	assert.True(t, found, "move_files is in the catalog")
	assert.ElementsMatch(t, []string{"ids", "folder"}, MoveFilesToolSpec.Required)
}
