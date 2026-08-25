package personality

import (
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// GetPromptDefaults returns default agent prompt text: scratchpad update (for optional persona override)
// and memory query/extraction strings (reference only; persona memory prompt overrides are disabled).
func (h *Handler) GetPromptDefaults(w http.ResponseWriter, r *http.Request) {
	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, models.PromptDefaults{
		ScratchpadUpdatePrompt: agent.ScratchpadUpdatePrompt,
		MemoryQueryPrompt:      agent.MemoryQueryPrompt,
		MemoryExtractionPrompt: agent.MemoryExtractionPrompt,
	})
}
