package datastore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// createWorkspaceTestSchema adds the workspace tables. Requires users (createMemoryImportTestSchema).
func createWorkspaceTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE workspace_files (
			id uuid PRIMARY KEY,
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			root text NOT NULL,
			root_ref uuid NOT NULL,
			path text NOT NULL,
			content_type text NOT NULL,
			current_revision integer NOT NULL DEFAULT 0,
			size integer NOT NULL DEFAULT 0,
			sha256 text NOT NULL DEFAULT '',
			storage_key text NOT NULL DEFAULT '',
			state text NOT NULL DEFAULT 'live',
			author_class text NOT NULL DEFAULT 'agent',
			content_updated_at datetime NOT NULL,
			last_read_at datetime,
			read_count integer NOT NULL DEFAULT 0,
			user_workspace_files uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX workspacefile_root_root_ref_path_user_workspace_files
			ON workspace_files (root, root_ref, path, user_workspace_files)`,
		`CREATE TABLE workspace_file_revisions (
			id uuid PRIMARY KEY,
			revision integer NOT NULL,
			op text NOT NULL,
			base_revision integer NOT NULL DEFAULT 0,
			storage_key text NOT NULL DEFAULT '',
			size integer NOT NULL DEFAULT 0,
			sha256 text NOT NULL DEFAULT '',
			author_class text NOT NULL,
			chat_id uuid,
			created_at datetime NOT NULL,
			workspace_file_revisions uuid NOT NULL REFERENCES workspace_files(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX workspacefilerevision_revision_workspace_file_revisions
			ON workspace_file_revisions (revision, workspace_file_revisions)`,
	}
	for _, stmt := range statements {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
}

func newWorkspaceTestDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	return newTestDatastore(t, createMemoryImportTestSchema, createWorkspaceTestSchema)
}

func wsInput(rootRef uuid.UUID, path, op string, base int, key string, size int64) models.WorkspaceRevisionInput {
	return models.WorkspaceRevisionInput{
		Root: models.WorkspaceRootAgent, RootRef: rootRef, Path: path, ContentType: "text/markdown",
		Op: op, BaseRevision: base, StorageKey: key, Size: size, SHA256: "sha-" + key,
		AuthorClass: models.WorkspaceAuthorAgent,
	}
}

func TestCommitWorkspaceRevision_CreateAdvanceAndConflict(t *testing.T) {
	ds, cleanup := newWorkspaceTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	persona := uuid.New()
	chatID := uuid.New()

	in := wsInput(persona, "notes.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k1", 10)
	in.ChatID = &chatID
	created, err := ds.CommitWorkspaceRevision(ctx, userID, in)
	require.NoError(t, err)
	assert.Equal(t, 1, created.CurrentRevision)
	assert.Equal(t, "k1", created.StorageKey)
	assert.Equal(t, models.WorkspaceFileLive, created.State)

	// A second create of a live path (two writers who both saw "no such file") is a conflict, so
	// the later writer can never silently replace the earlier one.
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "notes.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k-dup", 1))
	var conflict *WorkspaceConflictError
	require.True(t, errors.As(err, &conflict), "got %v", err)
	assert.Equal(t, 1, conflict.Current)

	got, err := ds.GetWorkspaceFile(ctx, userID, models.WorkspaceRootAgent, persona, "notes.md")
	require.NoError(t, err)
	assert.Equal(t, "k1", got.StorageKey, "the first writer's content is intact")

	advanced, err := ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "notes.md", models.WorkspaceOpWrite, 1, "k2", 20))
	require.NoError(t, err)
	assert.Equal(t, 2, advanced.CurrentRevision)

	// A stale base revision is refused with the current revision.
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "notes.md", models.WorkspaceOpWrite, 1, "k-stale", 1))
	require.True(t, errors.As(err, &conflict), "got %v", err)
	assert.Equal(t, 2, conflict.Current)

	advanced, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "notes.md", models.WorkspaceOpWrite, 2, "k3", 30))
	require.NoError(t, err)
	assert.Equal(t, 3, advanced.CurrentRevision)
	assert.Equal(t, int64(30), advanced.Size)

	// A base revision on a file that doesn't exist is a conflict at revision 0.
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "missing.md", models.WorkspaceOpWrite, 4, "k4", 1))
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, 0, conflict.Current)

	// Deleting a file that never existed is not found.
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "missing.md", models.WorkspaceOpDelete, models.WorkspaceAnyRevision, "", 0))
	assert.ErrorIs(t, err, ErrWorkspaceFileNotFound)

	revs, err := ds.dbClient.WorkspaceFileRevision.Query().All(ctx)
	require.NoError(t, err)
	assert.Len(t, revs, 3, "refused writes leave no revision rows")
}

func TestCommitWorkspaceRevision_DeleteReviveAndListing(t *testing.T) {
	ds, cleanup := newWorkspaceTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	persona := uuid.New()

	empty, err := ds.GetWorkspaceUsage(ctx, userID)
	require.NoError(t, err, "a user with no files has zero usage, not a NULL-scan error")
	assert.Equal(t, models.WorkspaceUsage{}, empty)

	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "a/one.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k1", 5))
	require.NoError(t, err)
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "a/two.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k2", 7))
	require.NoError(t, err)
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "b/three.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k3", 11))
	require.NoError(t, err)

	usage, err := ds.GetWorkspaceUsage(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, models.WorkspaceUsage{Files: 3, Bytes: 23}, usage)

	deleted, err := ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "a/two.md", models.WorkspaceOpDelete, 1, "", 0))
	require.NoError(t, err)
	assert.Equal(t, models.WorkspaceFileDeleted, deleted.State)
	assert.Equal(t, 2, deleted.CurrentRevision)

	listed, err := ds.ListWorkspaceFiles(ctx, userID, models.WorkspaceRootAgent, persona, "a/", 10)
	require.NoError(t, err)
	require.Len(t, listed, 1, "deleted files are not listed")
	assert.Equal(t, "a/one.md", listed[0].Path)

	all, err := ds.ListWorkspaceFiles(ctx, userID, models.WorkspaceRootAgent, persona, "", 10)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	usage, err = ds.GetWorkspaceUsage(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, models.WorkspaceUsage{Files: 2, Bytes: 16}, usage)

	revived, err := ds.CommitWorkspaceRevision(ctx, userID, wsInput(persona, "a/two.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k5", 3))
	require.NoError(t, err)
	assert.Equal(t, models.WorkspaceFileLive, revived.State)
	assert.Equal(t, 3, revived.CurrentRevision)
}

func TestWorkspace_OwnerAndRootIsolation(t *testing.T) {
	ds, cleanup := newWorkspaceTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	owner := createFATestUser(t, ds)
	stranger := createFATestUser(t, ds)
	persona := uuid.New()

	_, err := ds.CommitWorkspaceRevision(ctx, owner, wsInput(persona, "secret.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k1", 1))
	require.NoError(t, err)

	_, err = ds.GetWorkspaceFile(ctx, stranger, models.WorkspaceRootAgent, persona, "secret.md")
	assert.ErrorIs(t, err, ErrWorkspaceFileNotFound)
	_, err = ds.GetWorkspaceFile(ctx, owner, models.WorkspaceRootChat, persona, "secret.md")
	assert.ErrorIs(t, err, ErrWorkspaceFileNotFound, "roots are separate namespaces")

	listed, err := ds.ListWorkspaceFiles(ctx, stranger, models.WorkspaceRootAgent, persona, "", 10)
	require.NoError(t, err)
	assert.Empty(t, listed)

	// The same path for a different owner is a different file.
	_, err = ds.CommitWorkspaceRevision(ctx, stranger, wsInput(persona, "secret.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k2", 1))
	require.NoError(t, err)

	f, err := ds.GetWorkspaceFile(ctx, owner, models.WorkspaceRootAgent, persona, "secret.md")
	require.NoError(t, err)
	require.NoError(t, ds.TouchWorkspaceFileRead(ctx, owner, f.ID))
	require.NoError(t, ds.TouchWorkspaceFileRead(ctx, stranger, f.ID), "a stranger's touch is a no-op, not an error")
	f, err = ds.GetWorkspaceFile(ctx, owner, models.WorkspaceRootAgent, persona, "secret.md")
	require.NoError(t, err)
	assert.Equal(t, 1, f.ReadCount)
	assert.NotNil(t, f.LastReadAt)
}

func TestPurgeWorkspaceRoot_RemovesOnlyThatRoot(t *testing.T) {
	ds, cleanup := newWorkspaceTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	doomed, kept := uuid.New(), uuid.New()

	_, err := ds.CommitWorkspaceRevision(ctx, userID, wsInput(doomed, "a.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k1", 1))
	require.NoError(t, err)
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(doomed, "a.md", models.WorkspaceOpWrite, 1, "k2", 1))
	require.NoError(t, err)
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(doomed, "a.md", models.WorkspaceOpDelete, 2, "", 0))
	require.NoError(t, err)
	_, err = ds.CommitWorkspaceRevision(ctx, userID, wsInput(kept, "b.md", models.WorkspaceOpCreate, models.WorkspaceAnyRevision, "k3", 1))
	require.NoError(t, err)

	keys, err := ds.PurgeWorkspaceRoot(ctx, userID, models.WorkspaceRootAgent, doomed)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2"}, keys, "every stored revision object is returned, including a deleted file's history")

	_, err = ds.GetWorkspaceFile(ctx, userID, models.WorkspaceRootAgent, doomed, "a.md")
	assert.ErrorIs(t, err, ErrWorkspaceFileNotFound)
	_, err = ds.GetWorkspaceFile(ctx, userID, models.WorkspaceRootAgent, kept, "b.md")
	assert.NoError(t, err)
	revs, err := ds.dbClient.WorkspaceFileRevision.Query().All(ctx)
	require.NoError(t, err)
	assert.Len(t, revs, 1)

	none, err := ds.PurgeWorkspaceRoot(ctx, userID, models.WorkspaceRootAgent, doomed)
	require.NoError(t, err)
	assert.Empty(t, none, "purging again is a no-op")
}
