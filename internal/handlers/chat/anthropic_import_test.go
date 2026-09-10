package chat

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestParseAnthropicArchive_TerminatesOnMalformedStream pins the fix for a decode loop that
// never returned. json.Decoder keeps a stream-level syntax error permanently while More()
// goes on reporting data, so the parser's "skip the entry and continue" branch spun forever
// on any corruption after the first element, growing the error slice without bound and
// leaving the import job stuck in "processing" with its temp file still on disk.
//
// The parse runs in a goroutine behind a deadline so a regression fails this test in seconds
// instead of hanging the package until the go test timeout.
func TestParseAnthropicArchive_TerminatesOnMalformedStream(t *testing.T) {
	t.Parallel()

	const good = `{"uuid":"good-1","name":"First","chat_messages":[{"sender":"human","text":"hi"}]}`

	tests := []struct {
		name string
		body string
	}{
		{"truncated after first entry", `[` + good + `,{"uuid":"u2"`},
		{"invalid token after first entry", `[` + good + `,@]`},
		{"trailing comma", `[` + good + `,]`},
		{"syntax error nested in a later entry", `[` + good + `,{"chat_messages":[{,}]}]`},
		{"truncated inside the first entry", `[{"uuid":"u1","chat_messages":`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			type result struct {
				convs []models.ImportConversation
				errs  []string
				err   error
			}

			// The context is only an escape hatch. parseAnthropicArchive checks
			// ctx.Err() once per iteration, so cancelling releases a spinning
			// goroutine instead of leaving it to burn a core until the package
			// timeout. A correct parser returns long before this matters.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan result, 1)
			go func() {
				convs, errs, err := parseAnthropicArchive(ctx, strings.NewReader(tc.body), fixedNow)
				done <- result{convs, errs, err}
			}()

			select {
			case got := <-done:
				require.Error(t, got.err, "a malformed stream must report an error, not be skipped")
				require.NotErrorIs(t, got.err, context.Canceled,
					"the parser must reject the malformed stream on its own, not wait to be cancelled")
				require.Less(t, len(got.errs), 10, "per-entry errors must not accumulate in a loop")
			case <-time.After(10 * time.Second):
				cancel()
				<-done
				t.Fatal("parseAnthropicArchive did not return: the decode loop is spinning on a sticky decoder error")
			}
		})
	}
}

// TestParseAnthropicArchive_MalformedStreamKeepsEarlierConversations documents what a caller
// receives alongside the error: conversations decoded before the corruption. runChatImport
// discards them and fails the job, but the parser reports how far it got.
func TestParseAnthropicArchive_MalformedStreamKeepsEarlierConversations(t *testing.T) {
	t.Parallel()

	body := `[
		{"uuid":"good-1","name":"First","chat_messages":[{"sender":"human","text":"hi"}]},
		{"uuid":"good-2","name":"Second","chat_messages":[{"sender":"human","text":"bye"}]},
		{"uuid":"trunc"`

	convs, _, err := parseAnthropicArchive(context.Background(), strings.NewReader(body), fixedNow)
	require.Error(t, err)
	require.Contains(t, err.Error(), "conversation entry 3")
	require.Len(t, convs, 2)
	require.Equal(t, "First", convs[0].Title)
	require.Equal(t, "Second", convs[1].Title)
}
