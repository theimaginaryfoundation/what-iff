package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type fakeCodec struct{}

func (fakeCodec) GetName() string { return "fake" }
func (fakeCodec) Count(s string) (int, error) {
	// Deterministic for tests: 1 char == 1 token.
	return len(s), nil
}
func (fakeCodec) Encode(string) ([]uint, []string, error) { return nil, nil, nil }
func (fakeCodec) Decode([]uint) (string, error)           { return "", nil }

func TestTokenCounter_CountTokens_FallbackDoesNotPanic(t *testing.T) {
	t.Parallel()

	// Explicitly force the fallback path.
	c := &TokenCounter{enc: nil}
	n, err := c.CountTokens("hello world")
	require.NoError(t, err)
	require.Greater(t, n, 0)
}

func TestTokenCounter_CountTokens_Empty(t *testing.T) {
	t.Parallel()

	c := &TokenCounter{enc: nil}
	n, err := c.CountTokens("")
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
func TestSelectCarryOverTurns_FindsMostRecentCompleteTurn(t *testing.T) {
	user1 := &models.ChatMessage{Message: "u1", Origin: models.MessageOriginUser}
	assistant1 := &models.ChatMessage{Message: "a1", Origin: models.MessageOriginAssistant}
	user2 := &models.ChatMessage{Message: "u2", Origin: models.MessageOriginUser}
	assistant2 := &models.ChatMessage{Message: "a2", Origin: models.MessageOriginAssistant}

	// Ordered by descending SentAt (newest first): assistant2, user2, assistant1, user1.
	recent := []*models.ChatMessage{assistant2, user2, assistant1, user1}
	c := &TokenCounter{enc: fakeCodec{}}
	turns := c.SelectCarryOverTurns(recent, 3, 100)
	assert.Len(t, turns, 2)
	assert.Equal(t, user1, turns[0][0])
	assert.Equal(t, assistant1, turns[0][1])
	assert.Equal(t, user2, turns[1][0])
	assert.Equal(t, assistant2, turns[1][1])
}

func TestSelectCarryOverTurns_RespectsMaxTurns(t *testing.T) {
	user1 := &models.ChatMessage{Message: "u1", Origin: models.MessageOriginUser}
	assistant1 := &models.ChatMessage{Message: "a1", Origin: models.MessageOriginAssistant}
	user2 := &models.ChatMessage{Message: "u2", Origin: models.MessageOriginUser}
	assistant2 := &models.ChatMessage{Message: "a2", Origin: models.MessageOriginAssistant}
	user3 := &models.ChatMessage{Message: "u3", Origin: models.MessageOriginUser}
	assistant3 := &models.ChatMessage{Message: "a3", Origin: models.MessageOriginAssistant}

	recent := []*models.ChatMessage{assistant3, user3, assistant2, user2, assistant1, user1}
	c := &TokenCounter{enc: fakeCodec{}}
	turns := c.SelectCarryOverTurns(recent, 2, 100)
	assert.Len(t, turns, 2)
	assert.Equal(t, user2, turns[0][0])
	assert.Equal(t, assistant2, turns[0][1])
	assert.Equal(t, user3, turns[1][0])
	assert.Equal(t, assistant3, turns[1][1])
}

func TestSelectCarryOverTurns_RespectsTokenBudget(t *testing.T) {
	// Each message is 10 tokens, each turn is 20 tokens.
	user1 := &models.ChatMessage{Message: "uuuuuuuuuu", Origin: models.MessageOriginUser}
	assistant1 := &models.ChatMessage{Message: "aaaaaaaaaa", Origin: models.MessageOriginAssistant}
	user2 := &models.ChatMessage{Message: "UUUUUUUUUU", Origin: models.MessageOriginUser}
	assistant2 := &models.ChatMessage{Message: "AAAAAAAAAA", Origin: models.MessageOriginAssistant}
	user3 := &models.ChatMessage{Message: "xxxxxxxxxx", Origin: models.MessageOriginUser}
	assistant3 := &models.ChatMessage{Message: "yyyyyyyyyy", Origin: models.MessageOriginAssistant}

	recent := []*models.ChatMessage{assistant3, user3, assistant2, user2, assistant1, user1}
	c := &TokenCounter{enc: fakeCodec{}}
	turns := c.SelectCarryOverTurns(recent, 3, 40)
	assert.Len(t, turns, 2, "Should include only 2 turns within token budget")
	assert.Equal(t, user2, turns[0][0])
	assert.Equal(t, assistant2, turns[0][1])
	assert.Equal(t, user3, turns[1][0])
	assert.Equal(t, assistant3, turns[1][1])
}

func TestSelectCarryOverTurns_TruncatesOversizedSingleTurn(t *testing.T) {
	// Newest turn: user(400) + assistant(400) = 800, budget is 100 -> should truncate and include only 1 turn.
	user := &models.ChatMessage{Message: makeString("u", 400), Origin: models.MessageOriginUser}
	assistant := &models.ChatMessage{Message: makeString("a", 400), Origin: models.MessageOriginAssistant}
	recent := []*models.ChatMessage{assistant, user}
	c := &TokenCounter{enc: fakeCodec{}}
	turns := c.SelectCarryOverTurns(recent, 3, 100)
	assert.Len(t, turns, 1)
	assert.LessOrEqual(t, len(turns[0][0].Message)+len(turns[0][1].Message), 120, "Truncated turn should be small-ish")
	assert.Contains(t, turns[0][0].Message, "…")
	assert.Contains(t, turns[0][1].Message, "…")
}

func makeString(ch string, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = ch[0]
	}
	return string(out)
}
