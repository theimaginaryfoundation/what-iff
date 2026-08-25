package personality

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestCompleteDefaultExpressionGrid(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	rows := make([]models.PersonalityExpression, 0, len(agent.ExpressionGridKeys))
	for _, key := range agent.ExpressionGridKeys {
		k := key
		img := uuid.New()
		rows = append(rows, models.PersonalityExpression{
			ExpressionKey: k,
			ImageID:       &img,
		})
	}
	require.True(t, completeDefaultExpressionGrid(rows))

	require.False(t, completeDefaultExpressionGrid(nil))
	require.False(t, completeDefaultExpressionGrid([]models.PersonalityExpression{
		{ExpressionKey: "happy", ImageID: &id},
	}))
}
