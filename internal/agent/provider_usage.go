package agent

import (
	"github.com/theimaginaryfoundation/what-iff/internal/agent/embedding"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerpolicy"
)

// ProviderJob is one piece of work a provider key pays for, and the model that
// does it.
type ProviderJob struct {
	Job   string `json:"job"`
	Model string `json:"model"`
}

// ProviderUsage is everything one provider's key is spent on.
type ProviderUsage struct {
	Provider string        `json:"provider"`
	Required bool          `json:"required"`
	Jobs     []ProviderJob `json:"jobs"`
}

// ProviderUsageReport describes how provider keys work on this deployment.
//
// AccountsSupplyKeys travels with the job list because the two are read
// together: what a key is spent on only tells you what to expect if you also
// know whose key it is. A page that says "billed to your own account" where the
// operator pays is worse than one that says nothing.
type ProviderUsageReport struct {
	AccountsSupplyKeys bool            `json:"accounts_supply_keys"`
	Providers          []ProviderUsage `json:"providers"`
}

// chatModelPlaceholder stands in where the model is whatever the user picked,
// rather than something the app chose.
const chatModelPlaceholder = "the model you pick"

// ProviderUsageCatalog answers "what is my key being spent on, and by which
// model" for every provider.
//
// It reads the same constants the calls themselves use, deliberately. The
// alternative — writing the list out wherever it gets displayed — is how
// docs/ARCHITECTURE_SUMMARY.md came to claim archival runs on gpt-5-mini long
// after the code moved to gpt-5.6-luna. A second copy of this mapping is a
// second thing to forget.
func ProviderUsageCatalog() ProviderUsageReport {
	return ProviderUsageReport{
		AccountsSupplyKeys: providerpolicy.AccountsSupplyKeys(),
		Providers:          providerUsageProviders(),
	}
}

func providerUsageProviders() []ProviderUsage {
	return []ProviderUsage{
		{
			Provider: string(models.ModelProviderOpenAI),
			Required: true,
			Jobs: []ProviderJob{
				{Job: "The welcome message in your first chat", Model: FirstChatGreetingModelName},
				{Job: "Creating a personality", Model: generatePersonalityModel},
				{Job: "Memory extraction and scratchpad updates", Model: archivalOpenAIModel},
				{Job: "Conversation summaries at each checkpoint", Model: archivalOpenAIModel},
				{Job: "Naming chats and picking expressions", Model: chatNameModel},
				{Job: "Building memory search queries", Model: memoryQueryModel},
				{Job: "Reading a chat's mood", Model: moodAutoSelectModel},
				{Job: "Scheduling agent jobs", Model: models.UtilityModelName},
				{Job: "Searching your memories", Model: string(embedding.Model)},
				{Job: "Generating images, expressions and portraits", Model: provider.ImageEngine},
			},
		},
		{
			Provider: string(models.ModelProviderAnthropic),
			Jobs: []ProviderJob{
				{Job: "Chatting with any Claude model", Model: chatModelPlaceholder},
				// Only Claude chats archive on Claude; every other provider's
				// chats archive on OpenAI, so this line is not a hidden
				// Anthropic requirement for Gemini or GLM users.
				{Job: "Memory and scratchpad updates, for Claude chats only", Model: archivalClaudeModel},
			},
		},
		{
			Provider: string(models.ModelProviderGoogle),
			Jobs:     []ProviderJob{{Job: "Chatting with any Gemini model", Model: chatModelPlaceholder}},
		},
		{
			Provider: string(models.ModelProviderZAI),
			Jobs:     []ProviderJob{{Job: "Chatting with any GLM model", Model: chatModelPlaceholder}},
		},
	}
}
