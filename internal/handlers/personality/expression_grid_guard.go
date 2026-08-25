package personality

import (
	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// completeDefaultExpressionGrid reports whether every slot in the fixed default grid already has a gallery image.
func completeDefaultExpressionGrid(rows []models.PersonalityExpression) bool {
	byKey := make(map[string]models.PersonalityExpression, len(rows))
	for _, row := range rows {
		byKey[row.ExpressionKey] = row
	}
	for _, key := range agent.ExpressionGridKeys {
		r, ok := byKey[key]
		if !ok || r.ImageID == nil || *r.ImageID == uuid.Nil {
			return false
		}
	}
	return true
}
