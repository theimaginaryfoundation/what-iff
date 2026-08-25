package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func (a *Agent) EnrichUserMessageWithRituals(ctx context.Context, userID uuid.UUID, message *models.ChatMessage) (string, error) {
	ritualIds := GetRitualIds(message.Rituals)

	rituals, err := a.ds.GetRitualsByIDs(ctx, userID, ritualIds)
	if err != nil {
		return message.Message, err
	}
	message.Message += FormatRituals(rituals)
	return message.Message, nil
}

func FormatRituals(rituals []*models.Ritual) string {
	ritualText := ""
	for _, ritual := range rituals {
		ritualText += fmt.Sprintf("\n\n%s", ritual.Content)
	}
	return ritualText
}
func GetRitualIds(rituals []*models.Ritual) []uuid.UUID {
	ritualIds := make([]uuid.UUID, len(rituals))
	for i, ritual := range rituals {
		ritualIds[i] = ritual.ID
	}
	return ritualIds
}
