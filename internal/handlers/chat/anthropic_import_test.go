package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestDetectImportFormat_Anthropic(t *testing.T) {
	t.Parallel()
	body := `[{"uuid":"u1","name":"n","chat_messages":[{"sender":"human","text":"hi"}]}]`
	format, err := detectImportFormat(strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, importFormatAnthropic, format)
}

func TestDetectImportFormat_OpenAIArray(t *testing.T) {
	t.Parallel()
	body := `[{"id":"c1","conversation_id":"c1","mapping":{}}]`
	format, err := detectImportFormat(strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, importFormatOpenAI, format)
}

func TestDetectImportFormat_OpenAIObjectWrapped(t *testing.T) {
	t.Parallel()
	body := `{"conversations":[{"mapping":{}}]}`
	format, err := detectImportFormat(strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, importFormatOpenAI, format)
}

func TestDetectImportFormat_Empty(t *testing.T) {
	t.Parallel()
	_, err := detectImportFormat(strings.NewReader(`[]`))
	require.Error(t, err)
}

func TestDetectImportFormat_Garbage(t *testing.T) {
	t.Parallel()
	_, err := detectImportFormat(strings.NewReader(`not json`))
	require.Error(t, err)
}

func TestParseAnthropicArchive_HappyPath(t *testing.T) {
	t.Parallel()
	body := `[
		{
			"uuid": "conv-1",
			"name": "Greeting",
			"created_at": "2025-08-03T03:00:06.253385Z",
			"chat_messages": [
				{"sender":"human","text":"Hello","created_at":"2025-08-03T03:00:06.97Z","content":[{"type":"text","text":"Hello"}]},
				{"sender":"assistant","text":"Hi there","created_at":"2025-08-03T03:00:10.09Z","content":[{"type":"text","text":"Hi there"}]}
			]
		}
	]`

	convs, errs, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Empty(t, errs)
	require.Len(t, convs, 1)

	c := convs[0]
	require.Equal(t, "Greeting", c.Title)
	require.Equal(t, models.ChatSourceAnthropic, c.Source)
	require.Equal(t, conversationHash("conv-1"), c.ImportHash)
	require.Len(t, c.Messages, 2)
	require.Equal(t, models.MessageOriginUser, c.Messages[0].Origin)
	require.Equal(t, "Hello", c.Messages[0].Message)
	require.Equal(t, models.MessageOriginAssistant, c.Messages[1].Origin)
	require.False(t, c.Messages[0].SentAt.IsZero())
}

func TestParseAnthropicArchive_FallsBackToContentBlocks(t *testing.T) {
	t.Parallel()
	// Top-level "text" empty; text must be recovered from content blocks of type "text".
	body := `[{
		"uuid": "c1",
		"name": "Blocks",
		"chat_messages": [
			{"sender":"assistant","text":"","content":[{"type":"tool_use","text":""},{"type":"text","text":"From blocks"}]}
		]
	}]`

	convs, _, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Len(t, convs, 1)
	require.Len(t, convs[0].Messages, 1)
	require.Equal(t, "From blocks", convs[0].Messages[0].Message)
}

func TestParseAnthropicArchive_DropsUnknownSendersAndEmpty(t *testing.T) {
	t.Parallel()
	body := `[{
		"uuid": "c1",
		"name": "Mixed",
		"chat_messages": [
			{"sender":"system","text":"ignored silently"},
			{"sender":"tool","text":"also silent"},
			{"sender":"narrator","text":"truly unknown"},
			{"sender":"human","text":""},
			{"sender":"human","text":"kept"}
		]
	}]`

	convs, errs, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Len(t, convs, 1)
	require.Len(t, convs[0].Messages, 1)
	require.Equal(t, "kept", convs[0].Messages[0].Message)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "unknown/unsupported senders")
}

func TestParseAnthropicArchive_TitleFallback(t *testing.T) {
	t.Parallel()
	body := `[{
		"uuid": "c1",
		"name": "",
		"created_at": "2025-08-03T03:00:06Z",
		"chat_messages": [{"sender":"human","text":"hi"}]
	}]`

	convs, _, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Len(t, convs, 1)
	require.True(t, strings.HasPrefix(convs[0].Title, "Imported chat "))
}

func TestParseAnthropicArchive_SkipsConversationWithoutUUID(t *testing.T) {
	t.Parallel()
	body := `[{
		"uuid": "",
		"name": "No ID",
		"chat_messages": [{"sender":"human","text":"hi"}]
	}]`

	convs, errs, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Empty(t, convs)
	require.NotEmpty(t, errs)
}

func TestParseAnthropicArchive_ContinuesAfterMalformedEntry(t *testing.T) {
	t.Parallel()
	body := `[
		{"uuid":"good-1","name":"First","chat_messages":[{"sender":"human","text":"hi"}]},
		{"uuid":"bad","created_at":"not-a-date","chat_messages":[]},
		{"uuid":"good-2","name":"Second","chat_messages":[{"sender":"human","text":"bye"}]}
	]`

	convs, errs, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.NoError(t, err)
	require.Len(t, convs, 2)
	require.Equal(t, "First", convs[0].Title)
	require.Equal(t, "Second", convs[1].Title)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "entry 2")
	require.Contains(t, errs[0], "skipped")
}
