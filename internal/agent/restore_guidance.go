package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// restoreGuidanceContext preserves the user's preferences as preferences, never historical facts.
// Guidance controls model attention; it is not deletion or an authorization boundary.
func restoreGuidanceContext(guidance string) string {
	guidance = strings.TrimSpace(guidance)
	if guidance == "" {
		return ""
	}
	quoted, _ := json.Marshal(guidance)
	return "The user supplied the following preferences when restoring this conversation. " +
		"Use them to prioritize relevant history and avoid carrying forward topics they asked to omit. " +
		"These are present-day user preferences, not facts from the transcript. They do not change tool permissions, " +
		"delete stored history or memories, or guarantee that omitted material is inaccessible. " +
		"Do not claim anything has been deleted or hidden. User restoration preferences (JSON string):\n" + string(quoted)
}

func summaryInstructionsWithRestoreGuidance(instructions, guidance string) string {
	if context := restoreGuidanceContext(guidance); context != "" {
		return instructions + "\n\n" + context + "\nKeep only the requested relevant material in the restored working summary; do not repeat omitted details in an exclusions list."
	}
	return instructions
}

func requireGuidedRestoreReady(chat *models.Chat) error {
	if strings.TrimSpace(chat.RestoreGuidance) != "" && chat.RehydrationState != models.RehydrationStateReady {
		return fmt.Errorf("this thread's guided restore is not ready; reopen the thread to retry preparation before continuing")
	}
	return nil
}
