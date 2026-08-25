package agent_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestGetRitualIds(t *testing.T) {
	t.Run("no rituals", func(t *testing.T) {
		rituals := []*models.Ritual{}
		ids := agent.GetRitualIds(rituals)
		assert.Empty(t, ids)
	})

	t.Run("single ritual", func(t *testing.T) {
		exampleID := uuid.New()
		rituals := []*models.Ritual{{ID: exampleID}}
		ids := agent.GetRitualIds(rituals)
		assert.Len(t, ids, 1)
		assert.Equal(t, exampleID, ids[0])
	})

	t.Run("multiple rituals", func(t *testing.T) {
		id1 := uuid.New()
		id2 := uuid.New()
		rituals := []*models.Ritual{{ID: id1}, {ID: id2}}
		ids := agent.GetRitualIds(rituals)
		assert.Len(t, ids, 2)
		assert.Equal(t, []uuid.UUID{id1, id2}, ids)
	})
}

func TestFormatRituals(t *testing.T) {
	t.Run("no rituals", func(t *testing.T) {
		rituals := []*models.Ritual{}
		result := agent.FormatRituals(rituals)
		assert.Equal(t, "", result)
	})

	t.Run("single ritual", func(t *testing.T) {
		content := "First ritual"
		rituals := []*models.Ritual{{Content: content}}
		result := agent.FormatRituals(rituals)
		expected := fmt.Sprintf("\n\n%s", content)
		assert.Equal(t, expected, result)
	})

	t.Run("multiple rituals", func(t *testing.T) {
		contents := []string{"First ritual", "Second ritual"}
		rituals := []*models.Ritual{{Content: contents[0]}, {Content: contents[1]}}
		result := agent.FormatRituals(rituals)
		expected := fmt.Sprintf("\n\n%s\n\n%s", contents[0], contents[1])
		assert.Equal(t, expected, result)
	})
}
