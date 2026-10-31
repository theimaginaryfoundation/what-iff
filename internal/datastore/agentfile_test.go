package datastore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestAgentFiles_PathLookupSwapCountAndOrigin(t *testing.T) {
	ds, cleanup := newFileAttachmentTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	userID := createFATestUser(t, ds)
	other := createFATestUser(t, ds)
	persona := createFATestPersonality(t, ds, userID, "Scribe")

	agentFile, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{
		Name: "Journal.md", FileType: "text/markdown", S3Key: "users/u/agent/v1_journal.md", Folder: "notes",
		Origin: models.FileOriginAgent, CreatedByPersonalityID: &persona,
	})
	require.NoError(t, err)
	require.True(t, agentFile.AgentAuthored())
	require.Equal(t, &persona, agentFile.CreatedByPersonalityID)
	require.Equal(t, models.FileAttachmentSourceGenerated, agentFile.Source)

	upload, err := ds.CreateFileAttachment(ctx, userID, models.FileAttachment{Name: "brief.md", FileType: "text/markdown", S3Key: "users/u/brief.md", Folder: "notes"})
	require.NoError(t, err)
	require.False(t, upload.AgentAuthored())
	_, err = ds.CreateFileAttachment(ctx, other, models.FileAttachment{Name: "journal.md", FileType: "text/markdown", Folder: "notes"})
	require.NoError(t, err)

	found, err := ds.FindFileAttachmentsAtPath(ctx, userID, "notes", " journal.MD ")
	require.NoError(t, err)
	require.Len(t, found, 1, "case-insensitive, owner-scoped")
	require.Equal(t, agentFile.ID, found[0].ID)
	found, err = ds.FindFileAttachmentsAtPath(ctx, userID, "", "journal.md")
	require.NoError(t, err)
	require.Empty(t, found, "the folder must match exactly")

	ok, err := ds.SwapAgentFileObject(ctx, userID, agentFile.ID, "users/u/agent/stale.md", "users/u/agent/v2_journal.md")
	require.NoError(t, err)
	require.False(t, ok, "a stale old key does not swap")
	ok, err = ds.SwapAgentFileObject(ctx, userID, upload.ID, upload.S3Key, "users/u/agent/hijack.md")
	require.NoError(t, err)
	require.False(t, ok, "a user upload is never swapped")
	ok, err = ds.SwapAgentFileObject(ctx, other, agentFile.ID, agentFile.S3Key, "users/u/agent/hijack.md")
	require.NoError(t, err)
	require.False(t, ok, "another user cannot swap it")
	ok, err = ds.SwapAgentFileObject(ctx, userID, agentFile.ID, agentFile.S3Key, "users/u/agent/v2_journal.md")
	require.NoError(t, err)
	require.True(t, ok)
	got, err := ds.GetFileAttachment(ctx, userID, agentFile.ID)
	require.NoError(t, err)
	require.Equal(t, "users/u/agent/v2_journal.md", got.S3Key)
	require.NotNil(t, got.UpdatedAt)

	n, err := ds.CountAgentFiles(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	page, err := ds.ListFileAttachments(ctx, userID, 1, 50, models.FileAttachmentFilters{Origin: models.FileOriginAgent})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	page, err = ds.ListFileAttachments(ctx, userID, 1, 50, models.FileAttachmentFilters{Origin: models.FileOriginUser})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	require.Equal(t, upload.ID, page.Results[0].(*models.FileAttachment).ID)
}
