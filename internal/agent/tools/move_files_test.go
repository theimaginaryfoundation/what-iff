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
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
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

	renamed   bool
	renameID  uuid.UUID
	renameTo  string
	renameErr error
}

func (f *fakeMoveStore) RenameGalleryFile(_ context.Context, userID, id uuid.UUID, name string) (string, error) {
	f.called, f.renamed, f.user, f.renameID, f.renameTo = true, true, userID, id, name
	if f.renameErr != nil {
		return "", f.renameErr
	}
	return models.NormalizeFileName(name, "old.png")
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
	require.NotNil(t, res.Folder)
	assert.Equal(t, "charts/oura", *res.Folder)
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
	require.NotNil(t, res.Folder, "the top level is reported, not left out")
	assert.Equal(t, "", *res.Folder)
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
		"neither":         fmt.Sprintf(`{"ids":[%q]}`, uuid.New()),
		"rename many":     fmt.Sprintf(`{"ids":[%q,%q],"name":"x"}`, uuid.New(), uuid.New()),
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
	assert.ElementsMatch(t, []string{"ids"}, MoveFilesToolSpec.Required, "folder and name are each optional")
}

func TestMoveFilesRenamesOneFileWithoutMovingIt(t *testing.T) {
	store := &fakeMoveStore{}
	id := uuid.New()

	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q],"name":" vix-self-portrait "}`, id))

	assert.True(t, res.Success)
	assert.Equal(t, "vix-self-portrait.png", res.Name, "the result reports the stored name, extension kept")
	assert.Nil(t, res.Folder, "no folder was given, so nothing moved")
	assert.Zero(t, res.Moved)
	assert.Equal(t, id, store.renameID)
	assert.Empty(t, store.ids, "the move was not called")
}

func TestMoveFilesRenamesAndMovesInOneCall(t *testing.T) {
	store := &fakeMoveStore{}
	id := uuid.New()

	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q],"name":"portrait-2025-03.webp","folder":"Vix/Portraits"}`, id))

	assert.True(t, res.Success)
	assert.Equal(t, "portrait-2025-03.webp", res.Name)
	require.NotNil(t, res.Folder)
	assert.Equal(t, "vix/portraits", *res.Folder)
	assert.Equal(t, 1, res.Moved)
	assert.Equal(t, []uuid.UUID{id}, store.ids)
}

// The store is the one place a name is judged, since only it knows the extension the name keeps;
// its refusal reaches the model as is, and stops the call before anything moves.
func TestMoveFilesPassesOnTheStoresRefusalOfAName(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":    "  ",
		"slash":    "a/b.png",
		"too long": strings.Repeat("n", models.MaxFileNameRunes-2), // fits until ".png" is kept
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeMoveStore{}
			res := runMove(t, store, fmt.Sprintf(`{"ids":[%q],"name":%q,"folder":"x"}`, uuid.New(), raw))
			assert.False(t, res.Success)
			assert.Contains(t, res.Error, models.ErrInvalidFileName.Error())
			assert.Empty(t, store.ids, "nothing moved")
		})
	}
}

func TestMoveFilesRenameOfAnUnknownFileIsAResultNotAnError(t *testing.T) {
	store := &fakeMoveStore{renameErr: datastore.ErrFileAttachmentNotFound}
	res := runMove(t, store, fmt.Sprintf(`{"ids":[%q],"name":"x","folder":"y"}`, uuid.New()))
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "not one of your files")
	assert.Empty(t, store.ids, "a failed rename stops before the move")
}
