package chatimport

import (
	"context"
	"strings"
	"testing"
)

// One conversation in the OpenAI export mapping shape: a user turn and an
// assistant turn, plus a hidden empty system node that must be dropped.
const convJSON = `{
	"conversation_id": "conv-1",
	"title": "Chat A",
	"create_time": 1700000000,
	"current_node": "n3",
	"mapping": {
		"n1": {"id": "n1", "message": {"author": {"role": "system"}, "content": {"content_type": "text", "parts": [""]}, "metadata": {"is_visually_hidden_from_conversation": true}}, "parent": null, "children": ["n2"]},
		"n2": {"id": "n2", "message": {"author": {"role": "user"}, "create_time": 1700000001, "content": {"content_type": "text", "parts": ["hello"]}}, "parent": "n1", "children": ["n3"]},
		"n3": {"id": "n3", "message": {"author": {"role": "assistant"}, "create_time": 1700000002, "content": {"content_type": "text", "parts": ["hi there"]}}, "parent": "n2", "children": []}
	}
}`

func assertConvOne(t *testing.T, convs []SimplifiedConversation) {
	t.Helper()
	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}
	c := convs[0]
	if c.ConversationID != "conv-1" || c.Title != "Chat A" {
		t.Fatalf("unexpected conversation header: %+v", c)
	}
	if len(c.Messages) != 2 {
		t.Fatalf("expected hidden system node dropped and 2 messages kept, got %d: %+v", len(c.Messages), c.Messages)
	}
	if c.Messages[0].Role != "user" || c.Messages[0].Text != "hello" {
		t.Fatalf("messages not in chronological order: %+v", c.Messages)
	}
	if c.Messages[1].Role != "assistant" || c.Messages[1].Text != "hi there" {
		t.Fatalf("unexpected assistant message: %+v", c.Messages[1])
	}
}

func TestSplitFromReader_TopLevelArray(t *testing.T) {
	convs, err := SplitConversationArchiveFromReader(context.Background(), strings.NewReader("["+convJSON+"]"), ReaderSplitOptions{})
	if err != nil {
		t.Fatalf("SplitConversationArchiveFromReader: %v", err)
	}
	assertConvOne(t, convs)
}

func TestSplitFromReader_ObjectWrappedArray(t *testing.T) {
	in := `{"meta": {"exported_by": "test"}, "conversations": [` + convJSON + `]}`
	convs, err := SplitConversationArchiveFromReader(context.Background(), strings.NewReader(in), ReaderSplitOptions{})
	if err != nil {
		t.Fatalf("SplitConversationArchiveFromReader: %v", err)
	}
	assertConvOne(t, convs)
}

func TestSplitFromReader_ExplicitArrayField(t *testing.T) {
	// A decoy array first: auto-detection would pick it, ArrayField must win.
	in := `{"decoy": [], "threads": [` + convJSON + `]}`
	convs, err := SplitConversationArchiveFromReader(context.Background(), strings.NewReader(in), ReaderSplitOptions{ArrayField: "threads"})
	if err != nil {
		t.Fatalf("SplitConversationArchiveFromReader: %v", err)
	}
	assertConvOne(t, convs)
}

func TestSplitFromReader_DropsImageyEmptyToolMessage(t *testing.T) {
	in := `[{
		"conversation_id": "conv-img",
		"current_node": "n2",
		"mapping": {
			"n1": {"id": "n1", "message": {"author": {"role": "tool"}, "content": {"content_type": "image_asset_pointer", "parts": []}}, "parent": null, "children": ["n2"]},
			"n2": {"id": "n2", "message": {"author": {"role": "assistant"}, "content": {"content_type": "text", "parts": ["done"]}}, "parent": "n1", "children": []}
		}
	}]`
	convs, err := SplitConversationArchiveFromReader(context.Background(), strings.NewReader(in), ReaderSplitOptions{})
	if err != nil {
		t.Fatalf("SplitConversationArchiveFromReader: %v", err)
	}
	if len(convs) != 1 || len(convs[0].Messages) != 1 || convs[0].Messages[0].Role != "assistant" {
		t.Fatalf("expected imagey tool message dropped, got %+v", convs)
	}
}

func TestSplitFromReader_MissingID(t *testing.T) {
	if _, err := SplitConversationArchiveFromReader(context.Background(), strings.NewReader(`[{"title": "no id", "mapping": {}}]`), ReaderSplitOptions{}); err == nil {
		t.Fatal("expected error for conversation without conversation_id/id")
	}
}
