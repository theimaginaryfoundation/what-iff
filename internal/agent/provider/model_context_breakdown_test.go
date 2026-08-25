package provider

import "testing"

// TestSegmentBreakdown_AggregatesByKindInFirstAppearanceOrder verifies that segments of the
// same kind are rolled up (tokens summed, segment count incremented) and that the result
// preserves the order each kind first appears — i.e. it reads top-to-bottom like the
// context is actually laid out for the model.
func TestSegmentBreakdown_AggregatesByKindInFirstAppearanceOrder(t *testing.T) {
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "you are a helpful assistant", true)
	ctx.AppendHistoryTurn(RoleUser, "first user turn here", nil, false)
	ctx.AppendHistoryTurn(RoleAssistant, "first assistant turn here", nil, false)
	ctx.AppendUserMessage(RoleUser, "the current question", nil, false)

	counter := NewTokenCounter()
	stats := ctx.SegmentBreakdown(counter)

	if len(stats) != 3 {
		t.Fatalf("expected 3 kinds, got %d: %+v", len(stats), stats)
	}
	if stats[0].Kind != SegmentKindSystemPrompt {
		t.Errorf("expected system_prompt first, got %s", stats[0].Kind)
	}
	if stats[1].Kind != SegmentKindHistoryTurn {
		t.Errorf("expected history_turn second, got %s", stats[1].Kind)
	}
	if stats[1].Segments != 2 {
		t.Errorf("expected 2 history segments aggregated, got %d", stats[1].Segments)
	}
	if stats[2].Kind != SegmentKindUserMessage {
		t.Errorf("expected user_message last, got %s", stats[2].Kind)
	}
	for _, s := range stats {
		if s.Tokens <= 0 {
			t.Errorf("expected positive token estimate for %s, got %d", s.Kind, s.Tokens)
		}
	}
}

// TestSegmentBreakdown_CacheableAnyAndImageCount verifies the cacheable flag is OR-ed across
// a kind's segments and image payloads are counted even when a segment carries no text.
func TestSegmentBreakdown_CacheableAnyAndImageCount(t *testing.T) {
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "cached prefix", true)
	// Image-only user turn: empty text, one image payload.
	ctx.AppendUserMessage(RoleUser, "", []UserMessageImage{{FileID: "file_1", MediaType: "image/png"}}, false)

	stats := ctx.SegmentBreakdown(NewTokenCounter())

	var sys, user *SegmentKindStat
	for i := range stats {
		switch stats[i].Kind {
		case SegmentKindSystemPrompt:
			sys = &stats[i]
		case SegmentKindUserMessage:
			user = &stats[i]
		}
	}
	if sys == nil || !sys.Cacheable {
		t.Errorf("expected system prompt marked cacheable, got %+v", sys)
	}
	if user == nil {
		t.Fatalf("expected an image-only user_message segment to be present")
	}
	if user.Segments != 1 {
		t.Errorf("expected image-only user turn to count as 1 segment, got %d", user.Segments)
	}
	if user.Images != 1 {
		t.Errorf("expected 1 image counted on user turn, got %d", user.Images)
	}
	if user.Tokens != 0 {
		t.Errorf("expected 0 tokens for image-only turn, got %d", user.Tokens)
	}
}

// TestSegmentBreakdown_NilReceiverAndCounter guards the defensive nil paths.
func TestSegmentBreakdown_NilReceiverAndCounter(t *testing.T) {
	var nilCtx *ModelContext
	if got := nilCtx.SegmentBreakdown(NewTokenCounter()); got != nil {
		t.Errorf("expected nil breakdown for nil context, got %+v", got)
	}
	ctx := &ModelContext{}
	ctx.Append(SegmentKindSystemPrompt, RoleDeveloper, "text", true)
	// A nil counter should still return one row (tokens zero), not panic.
	stats := ctx.SegmentBreakdown(nil)
	if len(stats) != 1 || stats[0].Tokens != 0 {
		t.Errorf("expected 1 row with 0 tokens for nil counter, got %+v", stats)
	}
}
