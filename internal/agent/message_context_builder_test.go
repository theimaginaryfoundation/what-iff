package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func TestMergeAdditionalContextItems_DedupesByTypeAndContent(t *testing.T) {
	t.Parallel()

	u1 := uuid.New()
	history := []*models.ChatMessage{
		{
			ID:      u1,
			Origin:  models.MessageOriginUser,
			Message: "hi",
			AdditionalContext: []models.AdditionalContextItem{
				{Type: models.AdditionalContextTypeMemory, Content: "from-db"},
				{Type: models.AdditionalContextTypeMemory, Content: "dup"},
			},
		},
	}
	current := &models.ChatMessage{
		ID:     uuid.New(),
		Origin: models.MessageOriginUser,
		AdditionalContext: []models.AdditionalContextItem{
			{Type: models.AdditionalContextTypeMemory, Content: "dup"},
		},
	}
	got := mergeAdditionalContextItems(nil, history, current, []string{"from-fetch", "from-db"}, nil)
	require.Len(t, got, 3)
	keys := make(map[string]int)
	for _, it := range got {
		keys[it.Type+"\x00"+it.Content]++
	}
	require.Equal(t, 1, keys[models.AdditionalContextTypeMemory+"\x00"+"from-db"])
	require.Equal(t, 1, keys[models.AdditionalContextTypeMemory+"\x00"+"dup"])
	require.Equal(t, 1, keys[models.AdditionalContextTypeMemory+"\x00"+"from-fetch"])
}

// Memories persisted on earlier turns must survive rehydration as memory refs. They only do so
// when the stored context item carries its scope, so this pins the whole-segment accumulation the
// merger and compaction audit depend on.
func TestAppendMergedAdditionalContext_KeepsMemoryRefsFromPriorTurns(t *testing.T) {
	t.Parallel()

	priorID := uuid.New()
	currentID := uuid.New()
	items := []models.AdditionalContextItem{
		{Type: models.AdditionalContextTypeMemory, Content: "User prefers dark mode", Scope: "User", MemoryID: &priorID},
		{Type: models.AdditionalContextTypeMemory, Content: "User uses Neovim", Scope: "User", MemoryID: &currentID},
	}

	modelCtx := &provider.ModelContext{}
	appendMergedAdditionalContext(modelCtx, items)

	require.Len(t, modelCtx.MemoryRefs, 2)
	require.Equal(t, priorID.String(), modelCtx.MemoryRefs[0].MemoryID)
	require.Equal(t, "User prefers dark mode", modelCtx.MemoryRefs[0].Content)
	require.Equal(t, currentID.String(), modelCtx.MemoryRefs[1].MemoryID)
}

func TestNewMessageContextBuilder_RequiresTelemetryLogger(t *testing.T) {
	t.Parallel()
	_, err := newMessageContextBuilder(nil, nil, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "telemetry")

	_, err = newMessageContextBuilder(nil, &telemetry.Telemetry{}, nil, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "telemetry")
}

func TestMessageContextBuilder_Build_MemoriesAttachmentsAndUserOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	userID := uuid.New()
	chat := &models.Chat{
		ID:           uuid.New(),
		SystemPrompt: "be helpful",
	}
	tel := &telemetry.Telemetry{Logger: zap.NewNop()}
	b, err := newMessageContextBuilder(nil, tel, nil, func(_ context.Context, _, _ uuid.UUID, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
		return nil
	}, nil)
	require.NoError(t, err)

	fid := "file-1"
	atts := []*models.FileAttachment{
		{Name: "a.pdf", FileID: &fid},
	}

	mc, err := b.build(ctx, messageContextBuildRequest{
		UserID:                   userID,
		Chat:                     chat,
		UserPrompt:               "final user text",
		Memories:                 []string{"mem1", "mem2"},
		Attachments:              atts,
		ExpressionsEnabled:       true,
		IncludeAttachmentContext: true,
	})
	require.NoError(t, err)
	require.NotNil(t, mc)

	var kinds []provider.ModelContextSegmentKind
	for _, s := range mc.Segments {
		kinds = append(kinds, s.Kind)
	}
	require.Contains(t, kinds, provider.SegmentKindSystemPrompt)
	require.Contains(t, kinds, provider.SegmentKindMemoryContext)
	require.Contains(t, kinds, provider.SegmentKindAttachmentContext)
	require.Contains(t, kinds, provider.SegmentKindUserMessage)

	last := mc.Segments[len(mc.Segments)-1]
	require.Equal(t, provider.SegmentKindUserMessage, last.Kind)
	require.Equal(t, "final user text", last.Content)

	imgID := "file-img-1"
	attsWithImage := []*models.FileAttachment{
		{Name: "a.pdf", FileID: &fid},
		{Name: "shot.png", FileID: &imgID, FileType: "image/png"},
	}
	mc2, err := b.build(ctx, messageContextBuildRequest{
		UserID:                   userID,
		Chat:                     chat,
		UserPrompt:               "final user text",
		Attachments:              attsWithImage,
		ExpressionsEnabled:       true,
		IncludeAttachmentContext: true,
	})
	require.NoError(t, err)
	last2 := mc2.Segments[len(mc2.Segments)-1]
	require.Len(t, last2.UserImages, 1)
	require.Equal(t, imgID, last2.UserImages[0].FileID)
	require.Equal(t, "image/png", last2.UserImages[0].MediaType)

	var memSeg *provider.ModelContextSegment
	for i := range mc.Segments {
		if mc.Segments[i].Kind == provider.SegmentKindMemoryContext {
			memSeg = &mc.Segments[i]
			break
		}
	}
	require.NotNil(t, memSeg)
	require.Contains(t, memSeg.Content, "mem1")
	require.Contains(t, memSeg.Content, "mem2")
}

func TestMessageContextBuilder_Build_InjectsPersistedToolResults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	userID := uuid.New()
	chat := &models.Chat{ID: uuid.New(), SystemPrompt: "be helpful"}
	tel := &telemetry.Telemetry{Logger: zap.NewNop()}

	history := []*models.ChatMessage{
		{Origin: models.MessageOriginUser, Message: "find my notes"},
		{
			ID:      uuid.New(),
			Origin:  models.MessageOriginAssistant,
			Message: "here they are",
			ToolCalls: []*models.ToolCall{
				{ToolName: tools.RecallToolSpec.Name, ToolOutput: "chunk: the breathprint doc"},
			},
		},
	}
	b, err := newMessageContextBuilder(nil, tel, nil, func(_ context.Context, _, _ uuid.UUID, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
		return history
	}, nil)
	require.NoError(t, err)

	mc, err := b.build(ctx, messageContextBuildRequest{
		UserID:     userID,
		Chat:       chat,
		UserPrompt: "and summarize them",
	})
	require.NoError(t, err)

	var toolResultSeg *provider.ModelContextSegment
	for i := range mc.Segments {
		if mc.Segments[i].Kind == provider.SegmentKindToolResult {
			toolResultSeg = &mc.Segments[i]
			break
		}
	}
	require.NotNil(t, toolResultSeg, "expected a persisted tool-result segment")

	// The tool-result block must land after the (cacheable) history turns and before
	// the final user message, and must not be marked cacheable.
	var lastHistoryIdx, firstToolResultIdx, userIdx int
	lastHistoryIdx, firstToolResultIdx, userIdx = -1, -1, -1
	for i, s := range mc.Segments {
		switch s.Kind {
		case provider.SegmentKindHistoryTurn:
			lastHistoryIdx = i
		case provider.SegmentKindToolResult:
			if firstToolResultIdx == -1 {
				firstToolResultIdx = i
			}
			require.False(t, s.Cacheable, "tool-result segments must be non-cacheable")
		case provider.SegmentKindUserMessage:
			userIdx = i
		}
	}
	require.Greater(t, firstToolResultIdx, lastHistoryIdx)
	require.Greater(t, userIdx, firstToolResultIdx)

	joined := ""
	for _, s := range mc.Segments {
		if s.Kind == provider.SegmentKindToolResult {
			joined += s.Content + "\n"
		}
	}
	require.Contains(t, joined, "chunk: the breathprint doc")
	require.Contains(t, joined, tools.RecallToolSpec.Name)
}

func TestMessageContextBuilder_SelectCarryOverTurns_InvalidMax(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, err := newMessageContextBuilder(nil, &telemetry.Telemetry{Logger: zap.NewNop()}, nil, nil, nil)
	require.NoError(t, err)

	uid, cid := uuid.New(), uuid.New()
	_, err = b.selectCarryOverTurns(ctx, uid, cid, nil, 0, 100, nil, "test")
	require.Error(t, err)
	_, err = b.selectCarryOverTurns(ctx, uid, cid, nil, 1, 0, nil, "test")
	require.Error(t, err)
}

func TestMessageContextBuilder_LoadHistoryOverride_UsedAndNotReversed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uid, cid := uuid.New(), uuid.New()
	excl := uuid.New()
	tel := &telemetry.Telemetry{Logger: zap.NewNop()}

	var gotExcl uuid.UUID
	b, err := newMessageContextBuilder(nil, tel, nil, func(_ context.Context, _, _ uuid.UUID, excludeMessageID uuid.UUID, _ int, minDate *time.Time, _ string) []*models.ChatMessage {
		gotExcl = excludeMessageID
		return []*models.ChatMessage{
			{Origin: models.MessageOriginUser, Message: "first"},
			{Origin: models.MessageOriginAssistant, Message: "second"},
		}
	}, nil)
	require.NoError(t, err)

	msgs := b.loadHistoryMessages(ctx, uid, cid, &excl, 50, nil, "log")
	require.Equal(t, excl, gotExcl)
	require.Len(t, msgs, 2)
	require.Equal(t, "first", msgs[0].Message)
	require.Equal(t, "second", msgs[1].Message)
}

// fetchRecentMessages is best-effort: a datastore error must not propagate,
// it must surface as an empty slice so callers can keep composing context.
func TestFetchRecentMessages_DatastoreErrorReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	ds, _, cleanup := newTestDatastore(t)
	defer cleanup()

	b := &messageContextBuilder{ds: ds, telemetry: &telemetry.Telemetry{Logger: zap.NewNop()}}

	// No sqlmock expectations are configured, so the underlying query fails
	// immediately, exercising the error/best-effort-empty-slice branch.
	msgs := b.fetchRecentMessages(context.Background(), uuid.New(), uuid.New(), nil, 10, models.ChatMessageFilters{}, "test")
	require.NotNil(t, msgs)
	require.Empty(t, msgs)
}
