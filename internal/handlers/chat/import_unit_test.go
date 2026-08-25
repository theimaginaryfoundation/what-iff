package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	migration "github.com/theimaginaryfoundation/what-iff/internal/chatimport"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

var fixedNow = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

// --- conversationHash ---

func TestConversationHash_Stable(t *testing.T) {
	h1 := conversationHash("conv-abc")
	h2 := conversationHash("conv-abc")
	require.Equal(t, h1, h2)
}

func TestConversationHash_DifferentIDs(t *testing.T) {
	require.NotEqual(t, conversationHash("conv-a"), conversationHash("conv-b"))
}

func TestConversationHash_NonEmpty(t *testing.T) {
	require.NotEmpty(t, conversationHash("x"))
}

// --- floatToTime ---

func TestFloatToTime_WholeSeconds(t *testing.T) {
	got := floatToTime(1700000000.0)
	require.Equal(t, int64(1700000000), got.Unix())
	require.Equal(t, 0, got.Nanosecond())
}

func TestFloatToTime_FractionalSeconds(t *testing.T) {
	got := floatToTime(1700000000.5)
	require.Equal(t, int64(1700000000), got.Unix())
	require.InDelta(t, 5e8, float64(got.Nanosecond()), 1e6) // 0.5s ± 1ms tolerance for float64 precision
}

func TestFloatToTime_NsecNeverNegative(t *testing.T) {
	// Ensure float64 rounding never produces negative nanoseconds.
	for _, ts := range []float64{0.0, 0.1, 0.9999999999, 1.0, 1700000000.999999} {
		got := floatToTime(ts)
		require.GreaterOrEqual(t, got.Nanosecond(), 0, "nanoseconds must be >= 0 for ts=%v", ts)
		require.Less(t, got.Nanosecond(), int(1e9), "nanoseconds must be < 1e9 for ts=%v", ts)
	}
}

func TestFloatToTime_UTC(t *testing.T) {
	got := floatToTime(0)
	require.Equal(t, time.UTC, got.Location())
}

// --- models.TruncateImportTitle ---

func TestTruncateImportTitle_ShortString(t *testing.T) {
	require.Equal(t, "hello", models.TruncateImportTitle("hello"))
}

func TestTruncateImportTitle_ExactLength(t *testing.T) {
	exact := strings.Repeat("a", models.MaxImportTitleLen)
	require.Equal(t, exact, models.TruncateImportTitle(exact))
}

func TestTruncateImportTitle_LongString(t *testing.T) {
	long := strings.Repeat("a", models.MaxImportTitleLen+1)
	got := models.TruncateImportTitle(long)
	gotRunes := []rune(got)
	require.Equal(t, models.MaxImportTitleLen+1, len(gotRunes), "truncated string should be MaxImportTitleLen runes + ellipsis")
	require.Equal(t, '…', gotRunes[len(gotRunes)-1])
}

func TestTruncateImportTitle_MultiByte(t *testing.T) {
	// Build a string of MaxImportTitleLen+1 multi-byte runes; should truncate to MaxImportTitleLen + "…"
	long := strings.Repeat("日", models.MaxImportTitleLen+1)
	got := models.TruncateImportTitle(long)
	gotRunes := []rune(got)
	require.Equal(t, '…', gotRunes[len(gotRunes)-1])
	require.Equal(t, models.MaxImportTitleLen+1, len(gotRunes))
}

// --- prepareConversations ---

func conv(id, title string, createTime *float64, msgs []migration.SimplifiedMessage) migration.SimplifiedConversation {
	return migration.SimplifiedConversation{
		ConversationID: id,
		Title:          title,
		CreateTime:     createTime,
		Messages:       msgs,
	}
}

func floatPtr(f float64) *float64 { return &f }

func TestPrepareConversations_HappyPath(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "Chat A", floatPtr(1700000000), []migration.SimplifiedMessage{
			{Role: "user", Text: "Hello", CreateTime: floatPtr(1700000001)},
			{Role: "assistant", Text: "Hi", CreateTime: floatPtr(1700000002)},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Empty(t, errs)
	require.Len(t, convs, 1)
	require.Equal(t, "Chat A", convs[0].Title)
	require.Equal(t, conversationHash("id-1"), convs[0].ImportHash)
	require.Len(t, convs[0].Messages, 2)
}

func TestPrepareConversations_SystemAndToolRolesSilentlyDropped(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "Chat", nil, []migration.SimplifiedMessage{
			{Role: "system", Text: "Prompt"},
			{Role: "tool", Text: "browser payload", Name: "browser"},
			{Role: "user", Text: "Hello"},
			{Role: "assistant", Text: "Hi"},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Len(t, convs, 1)
	require.Len(t, convs[0].Messages, 2)
	require.Empty(t, errs, "system/tool are expected ChatGPT roles and must not surface as import errors")
}

func TestPrepareConversations_UnknownRoleWarned(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "Chat", nil, []migration.SimplifiedMessage{
			{Role: "developer", Text: "Weird"},
			{Role: "user", Text: "Hello"},
			{Role: "assistant", Text: "Hi"},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Len(t, convs, 1)
	require.Len(t, convs[0].Messages, 2)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "unknown/unsupported roles")
}

func TestPrepareConversations_EmptyTextDropped(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "Chat", nil, []migration.SimplifiedMessage{
			{Role: "user", Text: ""},
			{Role: "user", Text: "Hello"},
			{Role: "assistant", Text: "Hi"},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Empty(t, errs)
	require.Len(t, convs[0].Messages, 2)
}

func TestPrepareConversations_EmptyConversationIDSkipped(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("", "Chat", nil, []migration.SimplifiedMessage{
			{Role: "user", Text: "Hello"},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Empty(t, convs)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "missing conversation ID")
}

func TestPrepareConversations_AllFilteredSkipsConversation(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "System Only", nil, []migration.SimplifiedMessage{
			{Role: "system", Text: "Prompt"},
			{Role: "tool", Text: "noise"},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Empty(t, convs)
	require.Len(t, errs, 1) // skipped-conversation only; system/tool are silent
	require.Contains(t, errs[0], "no user/assistant messages after filtering")
}

func TestPrepareConversations_SynthesisedTitle(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "", floatPtr(1700000000), []migration.SimplifiedMessage{
			{Role: "user", Text: "Hello"},
		}),
	}
	convs, errs := prepareConversations(raw, fixedNow)
	require.Empty(t, errs)
	require.Contains(t, convs[0].Title, "Imported chat")
}

func TestPrepareConversations_SynthesisedTitleFallsBackToNow(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "", nil, []migration.SimplifiedMessage{
			{Role: "user", Text: "Hello"},
		}),
	}
	convs, _ := prepareConversations(raw, fixedNow)
	require.Contains(t, convs[0].Title, "2024-01-15 10:30")
}

func TestPrepareConversations_MissingTimestampFallsBackToNow(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "Chat", nil, []migration.SimplifiedMessage{
			{Role: "user", Text: "Hello"},
		}),
	}
	convs, _ := prepareConversations(raw, fixedNow)
	require.Equal(t, fixedNow, convs[0].CreatedAt)
	require.Equal(t, fixedNow, convs[0].Messages[0].SentAt)
}

func TestPrepareConversations_HashIsStableAcrossCalls(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-stable", "Chat", floatPtr(1700000000), []migration.SimplifiedMessage{
			{Role: "user", Text: "Hello"},
		}),
	}
	c1, _ := prepareConversations(raw, fixedNow)
	c2, _ := prepareConversations(raw, fixedNow.Add(24*time.Hour)) // different now
	require.Equal(t, c1[0].ImportHash, c2[0].ImportHash, "hash must not depend on import time")
}

func TestPrepareConversations_OriginMapping(t *testing.T) {
	raw := []migration.SimplifiedConversation{
		conv("id-1", "Chat", nil, []migration.SimplifiedMessage{
			{Role: "user", Text: "Q"},
			{Role: "assistant", Text: "A"},
		}),
	}
	convs, _ := prepareConversations(raw, fixedNow)
	require.Equal(t, models.MessageOriginUser, convs[0].Messages[0].Origin)
	require.Equal(t, models.MessageOriginAssistant, convs[0].Messages[1].Origin)
}
