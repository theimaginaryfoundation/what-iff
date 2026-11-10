package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

func TestContextWorkspacePath(t *testing.T) {
	for name, want := range map[string]string{
		"report.csv":        "report.csv",
		"  notes.md ":       "notes.md",
		"sub\\dir\\a.txt":   "sub/dir/a.txt",
		"./tidy/../a.txt":   "a.txt",
		"/etc/passwd":       "",
		"../escape.txt":     "",
		"":                  "",
		".":                 "",
		"a/../../escape.md": "",
	} {
		got, err := ContextWorkspacePath(&models.FileAttachment{Name: name})
		if want == "" {
			require.Error(t, err, name)
		} else {
			require.NoError(t, err, name)
			require.Equal(t, want, got, name)
		}
	}
	_, err := ContextWorkspacePath(nil)
	require.Error(t, err)

	p, err := SkillBundleWorkspacePath("tidy", "scripts/run.sh")
	require.NoError(t, err)
	require.Equal(t, "skills/tidy/scripts/run.sh", p)
	_, err = SkillBundleWorkspacePath("tidy", "../../other/x.sh")
	require.Error(t, err, "a bundle file never leaves its skill's folder")
}

func TestContextFileCollector_ReasonsShadowingAndOrder(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	older := &models.FileAttachment{ID: uuid.New(), Name: "Report.csv", CreatedAt: t0}
	newer := &models.FileAttachment{ID: uuid.New(), Name: "report.csv", CreatedAt: t0.Add(time.Hour)}
	other := &models.FileAttachment{ID: uuid.New(), Name: "notes.md", CreatedAt: t0.Add(30 * time.Minute)}
	bad := &models.FileAttachment{ID: uuid.New(), Name: "../escape.md", CreatedAt: t0}
	generatedA := &models.FileAttachment{ID: uuid.New(), Name: "out.png"} // zero time: this turn
	generatedB := &models.FileAttachment{ID: uuid.New(), Name: "OUT.png"}

	c := newContextFileCollector()
	c.add(older, "", ContextReasonAttachedSinceCheckpoint)
	c.add(older, "", ContextReasonAttachedRecent)
	c.add(older, "", ContextReasonAttachedRecent) // repeated: no duplicate reason
	c.add(newer, "", ContextReasonAttachedRecent)
	c.add(other, "", ContextReasonAgentWritten)
	c.add(bad, "", ContextReasonAttachedRecent)
	c.add(nil, "", ContextReasonAttachedRecent)
	c.add(generatedA, "", ContextReasonProducedThisTurn)
	c.add(generatedB, "", ContextReasonProducedThisTurn)
	c.add(&models.FileAttachment{ID: uuid.New(), Name: "run.sh", CreatedAt: t0}, "skills/tidy/scripts/run.sh", ContextReasonSkillBundle)

	got := c.result()
	require.Len(t, got, 6, "the invalid path and the nil are left out")
	byID := map[uuid.UUID]ContextFile{}
	for _, f := range got {
		byID[f.Attachment.ID] = f
	}
	require.Equal(t, []ContextReason{ContextReasonAttachedSinceCheckpoint, ContextReasonAttachedRecent}, byID[older.ID].Reasons)
	require.NotNil(t, byID[older.ID].ShadowedBy)
	require.Equal(t, newer.ID, *byID[older.ID].ShadowedBy, "the newest file takes the path, case-insensitively")
	require.Nil(t, byID[newer.ID].ShadowedBy)
	require.Nil(t, byID[other.ID].ShadowedBy)
	var skillPaths []string
	for _, f := range got {
		if f.Reasons[0] == ContextReasonSkillBundle {
			skillPaths = append(skillPaths, f.WorkspacePath)
		}
	}
	require.Equal(t, []string{"skills/tidy/scripts/run.sh"}, skillPaths, "a bundle file keeps the path it was given")
	require.NotNil(t, byID[generatedA.ID].ShadowedBy, "with equal times the one seen later wins")
	require.Equal(t, generatedB.ID, *byID[generatedA.ID].ShadowedBy)

	// Newest first; files without a time (this turn's) sort last, later-seen first among them.
	require.Equal(t, newer.ID, got[0].Attachment.ID)
	require.Equal(t, other.ID, got[1].Attachment.ID)
	require.Equal(t, generatedB.ID, got[4].Attachment.ID)
	require.Equal(t, generatedA.ID, got[5].Attachment.ID)
}

func TestContextFilesForChat_NilAgentOrChat(t *testing.T) {
	var a *Agent
	files, err := a.ContextFilesForChat(t.Context(), &models.Chat{})
	require.NoError(t, err)
	require.Nil(t, files)
	files, err = newTestAgent(nil).ContextFilesForChat(t.Context(), nil)
	require.NoError(t, err)
	require.Nil(t, files)
}

func TestContextFilesForChat_SeamsWithoutADatastore(t *testing.T) {
	prevExtra, prevStatus := extraContextFilesForChat, containerStatusForChat
	t.Cleanup(func() { extraContextFilesForChat, containerStatusForChat = prevExtra, prevStatus })
	produced := &models.FileAttachment{ID: uuid.New(), Name: "chart.png"}
	extraContextFilesForChat = func(_ context.Context, _ *models.Chat) []*models.FileAttachment {
		return []*models.FileAttachment{produced}
	}
	containerStatusForChat = func(_ context.Context, _ *models.Chat, files []ContextFile) *ContainerStatus {
		return &ContainerStatus{State: "running", Files: map[string]string{files[0].WorkspacePath: "current"}}
	}
	a := &Agent{logger: zap.NewNop()}
	chat := &models.Chat{ID: uuid.New(), UserID: uuid.New()}
	files, err := a.ContextFilesForChat(t.Context(), chat)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "chart.png", files[0].WorkspacePath)
	require.Equal(t, []ContextReason{ContextReasonProducedThisTurn}, files[0].Reasons)
	status := a.ContainerStatusForChat(t.Context(), chat, files)
	require.Equal(t, "running", status.State)
	require.Equal(t, "current", status.Files["chart.png"])

	containerStatusForChat = nil
	require.Nil(t, a.ContainerStatusForChat(t.Context(), chat, files))
}
