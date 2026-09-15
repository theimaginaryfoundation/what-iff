package accountexport

import (
	"testing"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/exporter"
)

func TestFilterSelectedConversations(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	parsed := []exporter.ParsedConversation{
		{UUID: a.String()},
		{UUID: b.String()},
		{UUID: c.String()},
		{UUID: "not-a-uuid"}, // unparseable ids are dropped when a selection is active
	}

	got := filterSelectedConversations(parsed, []uuid.UUID{a, c})
	if len(got) != 2 {
		t.Fatalf("expected 2 selected conversations, got %d", len(got))
	}
	if got[0].UUID != a.String() || got[1].UUID != c.String() {
		t.Errorf("selection did not preserve the chosen conversations in order: %+v", got)
	}

	if n := len(filterSelectedConversations(parsed, nil)); n != 0 {
		t.Errorf("empty selection should restore nothing, got %d", n)
	}
}
