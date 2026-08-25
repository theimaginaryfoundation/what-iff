package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

func TestIsFirstUserMessageResult_OnlyCurrentUserMessage(t *testing.T) {
	t.Parallel()

	currentID := uuid.New()
	results := []any{
		&models.ChatMessage{ID: currentID, Origin: models.MessageOriginUser, Message: "hello"},
	}

	require.True(t, isFirstUserMessageResult(results, currentID))
}

func TestIsFirstUserMessageResult_WithPreviousUserMessage(t *testing.T) {
	t.Parallel()

	currentID := uuid.New()
	results := []any{
		&models.ChatMessage{ID: currentID, Origin: models.MessageOriginUser, Message: "second"},
		&models.ChatMessage{ID: uuid.New(), Origin: models.MessageOriginUser, Message: "first"},
	}

	require.False(t, isFirstUserMessageResult(results, currentID))
}

func TestParseMoodAutoSelectResult_ValidJSON(t *testing.T) {
	t.Parallel()

	moodID := uuid.New()
	got, err := parseMoodAutoSelectResult(`{"mood_id":"` + moodID.String() + `"}`)
	require.NoError(t, err)
	require.Equal(t, moodID, got)
}

func TestParseMoodAutoSelectResult_MalformedJSON(t *testing.T) {
	t.Parallel()

	_, err := parseMoodAutoSelectResult(`{"mood_id":`)
	require.Error(t, err)
}

func TestParseMoodAutoSelectResult_MultipleJSONObjectPayloadFails(t *testing.T) {
	t.Parallel()

	moodID := uuid.New()
	_, err := parseMoodAutoSelectResult(`{"mood_id":"` + moodID.String() + `"}{"mood_id":"` + uuid.New().String() + `"}`)
	require.Error(t, err)
}

func TestParseMoodAutoSelectResult_ExtraProseFails(t *testing.T) {
	t.Parallel()

	moodID := uuid.New()
	_, err := parseMoodAutoSelectResult(`Here is the result: {"mood_id":"` + moodID.String() + `"}`)
	require.Error(t, err)
}

func TestFormatMoodContextBlock_AutoWithTools(t *testing.T) {
	t.Parallel()

	mood := &models.Mood{
		Name:          "Writing",
		Description:   "Creative drafting mode.",
		PromptSnippet: "Write in a warm tone.",
	}
	rituals := []*models.Ritual{{Name: "Style Guide", Content: "Use Oxford commas."}}
	got := formatMoodContextBlock(mood, rituals, moodContextOptions{IsAutoMood: true, MoodToolsAvailable: true})

	require.Contains(t, got, `You are currently in "Writing" mode (auto-selected for this conversation).`)
	require.Contains(t, got, "Creative drafting mode.")
	require.Contains(t, got, "list_modes")
	require.Contains(t, got, "change_mode")
	require.Contains(t, got, "This mode adds the following context:")
	require.Contains(t, got, "Write in a warm tone.")
	require.Contains(t, got, "Attached skills for this mode:\n")
	require.Contains(t, got, "- Style Guide: Use Oxford commas.")
	idxTools := strings.Index(got, "list_modes")
	idxContext := strings.Index(got, "This mode adds the following context:")
	require.Greater(t, idxContext, idxTools, "tool guidance should precede mode context header")
}

func TestFormatMoodContextBlock_LockedWithoutTools(t *testing.T) {
	t.Parallel()

	mood := &models.Mood{
		Name:          "Code Review",
		Description:   "Strict review mode.",
		PromptSnippet: "Focus on correctness.",
	}
	got := formatMoodContextBlock(mood, nil, moodContextOptions{IsAutoMood: false, MoodToolsAvailable: false})

	require.Contains(t, got, `You are currently in "Code Review" mode (locked for this conversation).`)
	require.Contains(t, got, "Focus on correctness.")
	require.NotContains(t, got, "list_modes")
	require.NotContains(t, got, "change_mode")
}

func TestFormatMoodContextBlock_RitualsOnly(t *testing.T) {
	t.Parallel()

	mood := &models.Mood{Name: "Skills Only"}
	rituals := []*models.Ritual{{Name: "Checklist", Content: "Step one."}}
	got := formatMoodContextBlock(mood, rituals, moodContextOptions{IsAutoMood: true})

	require.Contains(t, got, `You are currently in "Skills Only" mode`)
	require.Contains(t, got, "Attached skills for this mode:\n")
	require.Contains(t, got, "- Checklist: Step one.")
	require.NotContains(t, got, "This mode adds the following context:")
}

func TestFormatMoodContextBlock_EmptyReturnsBlank(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", formatMoodContextBlock(nil, nil, moodContextOptions{}))
	require.Equal(t, "", formatMoodContextBlock(&models.Mood{Name: "Empty"}, nil, moodContextOptions{}))
}

func TestFormatMoodRitualsForContext_BulletList(t *testing.T) {
	t.Parallel()

	got := formatMoodRitualsForContext([]*models.Ritual{
		{Name: "Test Skill", Content: "This is a test skill instruction."},
		{Name: "Second", Content: "Another line."},
	})
	require.Equal(t, "- Test Skill: This is a test skill instruction.\n- Second: Another line.", got)
}

func TestMessageContextBuilder_Build_ModeRitualsNotInUserMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	userID := uuid.New()
	chat := &models.Chat{
		ID:           uuid.New(),
		SystemPrompt: "be helpful",
		IsAutoMood:   true,
	}
	tel := &telemetry.Telemetry{Logger: zap.NewNop()}
	b, err := newMessageContextBuilder(nil, tel, nil, func(_ context.Context, _, _ uuid.UUID, _ uuid.UUID, _ int, _ *time.Time, _ string) []*models.ChatMessage {
		return nil
	}, nil)
	require.NoError(t, err)

	mood := &models.Mood{
		Name:          "Review",
		PromptSnippet: "Be concise.",
	}
	moodRituals := []*models.Ritual{{Name: "Rubric", Content: "Grade on clarity."}}

	mc, err := b.build(ctx, messageContextBuildRequest{
		UserID:             userID,
		Chat:               chat,
		UserPrompt:         "hello there",
		ActiveMood:         mood,
		ActiveMoodRituals:  moodRituals,
		IsAutoMood:         true,
		MoodToolsAvailable: true,
	})
	require.NoError(t, err)

	var moodSeg, userSeg *provider.ModelContextSegment
	for i := range mc.Segments {
		switch mc.Segments[i].Kind {
		case provider.SegmentKindMood:
			moodSeg = &mc.Segments[i]
		case provider.SegmentKindUserMessage:
			userSeg = &mc.Segments[i]
		}
	}
	require.NotNil(t, moodSeg)
	require.Contains(t, moodSeg.Content, "Be concise.")
	require.Contains(t, moodSeg.Content, "- Rubric: Grade on clarity.")
	require.NotNil(t, userSeg)
	require.Equal(t, "hello there", userSeg.Content)
}
