package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	migration "github.com/theimaginaryfoundation/what-iff/internal/chatimport"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func requireStrictlyIncreasingImportTimes(t *testing.T, messages []models.ChatMessage) {
	t.Helper()
	for i := 1; i < len(messages); i++ {
		require.Truef(t, messages[i].SentAt.After(messages[i-1].SentAt),
			"message %d (%s) must sort after message %d (%s): %s <= %s",
			i, messages[i].Origin, i-1, messages[i-1].Origin,
			messages[i].SentAt.Format(time.RFC3339Nano), messages[i-1].SentAt.Format(time.RFC3339Nano))
	}
}

func TestPrepareConversations_MakesMissingAndRegressingTimesStableInTranscriptOrder(t *testing.T) {
	t.Parallel()
	first := float64(1700000002)
	regressing := float64(1700000001)
	raw := []migration.SimplifiedConversation{{
		ConversationID: "order-openai",
		Title:          "Ordering",
		CreateTime:     &first,
		Messages: []migration.SimplifiedMessage{
			{Role: "user", Text: "first", CreateTime: &first},
			{Role: "assistant", Text: "second"},
			{Role: "user", Text: "third", CreateTime: &regressing},
		},
	}}

	convs, errs := prepareConversations(raw, fixedNow)
	require.Empty(t, errs)
	require.Len(t, convs, 1)
	require.Equal(t, []models.MessageOrigin{
		models.MessageOriginUser,
		models.MessageOriginAssistant,
		models.MessageOriginUser,
	}, []models.MessageOrigin{
		convs[0].Messages[0].Origin,
		convs[0].Messages[1].Origin,
		convs[0].Messages[2].Origin,
	})
	requireStrictlyIncreasingImportTimes(t, convs[0].Messages)
}

func TestParseAnthropicArchive_MakesEqualAndRegressingTimesStableInTranscriptOrder(t *testing.T) {
	t.Parallel()
	body := `[{ 
		"uuid":"order-anthropic",
		"name":"Ordering",
		"created_at":"2025-08-03T03:00:00Z",
		"chat_messages":[
			{"sender":"human","text":"first","created_at":"2025-08-03T03:00:02Z"},
			{"sender":"assistant","text":"second","created_at":"2025-08-03T03:00:02Z"},
			{"sender":"human","text":"third","created_at":"2025-08-03T03:00:01Z"}
		]
	}]`

	convs, errs, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Empty(t, errs)
	require.Len(t, convs, 1)
	require.Equal(t, []models.MessageOrigin{
		models.MessageOriginUser,
		models.MessageOriginAssistant,
		models.MessageOriginUser,
	}, []models.MessageOrigin{
		convs[0].Messages[0].Origin,
		convs[0].Messages[1].Origin,
		convs[0].Messages[2].Origin,
	})
	requireStrictlyIncreasingImportTimes(t, convs[0].Messages)
}
