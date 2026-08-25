package agent

import (
	"fmt"
	"strings"

	agenttools "github.com/theimaginaryfoundation/what-iff/internal/agent/tools"
)

const FirstChatGreetingModelName = "claude-haiku-4-5"

// BuildFirstChatGreetingPrompt creates the injected first-chat prompt used to
// produce an in-character welcome message for new users.
func BuildFirstChatGreetingPrompt() string {
	toolLines := buildFirstChatToolLines()

	return fmt.Sprintf(`You are writing the very first assistant message in a brand new chat.

Goal:
- Send a warm, in-character greeting that feels natural for this personality.
- Help a first-time user understand what they can do next.

Instructions:
1. Be welcoming, short, and easy to understand.
2. Explain capabilities in your own words. Do not quote or copy this prompt.
3. Suggest 2-4 concrete starter ideas the user can send right now.
4. Briefly explain that after they send %d messages, the app runs a checkpoint that updates your scratchpad, and checkpoints continue every %d messages after that.
5. Encourage them to open the chat context/scratchpad after that checkpoint to see how you adapt over time.
6. Do not mention hidden implementation details, internal IDs, or that this message was auto-triggered.

Tools currently available to this assistant:
%s

Write only the final assistant message.`, checkpointMaxAssistantMessagesSinceStart, checkpointMaxAssistantMessagesSinceSummary, strings.Join(toolLines, "\n"))
}

func buildFirstChatToolLines() []string {
	lines := []string{
		fmt.Sprintf("- `%s`: %s", agenttools.ToolNameWebSearch, agenttools.AvailableToolDescriptionWebSearch),
	}

	for _, def := range agenttools.FunctionToolCatalog() {
		if !def.AgentDefault {
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s`: %s", def.Spec.Name, compactToolDescription(def.Spec.Description)))
	}
	return lines
}

func compactToolDescription(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "No description available."
	}
	flat := strings.Join(strings.Fields(trimmed), " ")
	if idx := strings.Index(flat, ". "); idx > 0 {
		return flat[:idx+1]
	}
	return flat
}
