package schema

import "testing"

func TestChatMCPToolStateSchemaMethods(t *testing.T) {
	var s ChatMCPToolState
	if got := s.Fields(); len(got) == 0 {
		t.Fatal("expected fields")
	}
	if got := s.Indexes(); len(got) == 0 {
		t.Fatal("expected indexes")
	}
	if got := s.Edges(); len(got) != 0 {
		t.Fatalf("expected no edges, got %d", len(got))
	}
	if got := s.Mixin(); len(got) == 0 {
		t.Fatal("expected mixins")
	}
}
