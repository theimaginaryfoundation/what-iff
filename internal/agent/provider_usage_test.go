package agent

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestProviderUsageCatalog_OpenAIIsTheOnlyRequiredProvider(t *testing.T) {
	t.Parallel()

	catalog := ProviderUsageCatalog()
	required := []string{}
	for _, p := range catalog {
		if p.Required {
			required = append(required, p.Provider)
		}
	}
	require.Equal(t, []string{string(models.ModelProviderOpenAI)}, required,
		"OpenAI is the one key the app itself spends; everything else only buys its own chat models")
}

// The catalog exists to be displayed, so an entry with no model named is worse
// than no entry — it reads as though nothing is being spent.
func TestProviderUsageCatalog_EveryJobNamesAModel(t *testing.T) {
	t.Parallel()

	for _, p := range ProviderUsageCatalog() {
		require.NotEmptyf(t, p.Jobs, "provider %q lists no jobs", p.Provider)
		for _, j := range p.Jobs {
			require.NotEmptyf(t, j.Job, "provider %q has a job with no description", p.Provider)
			require.NotEmptyf(t, j.Model, "job %q on provider %q names no model", j.Job, p.Provider)
		}
	}
}

// Pins the mapping to the constants the calls actually use. If someone changes
// the model a job runs on, this catalog follows automatically — that is the
// whole point of building it from the constants rather than restating them.
func TestProviderUsageCatalog_ReadsTheLiveConstants(t *testing.T) {
	t.Parallel()

	jobs := map[string]string{}
	for _, p := range ProviderUsageCatalog() {
		for _, j := range p.Jobs {
			jobs[j.Job] = j.Model
		}
	}

	require.Equal(t, FirstChatGreetingModelName, jobs["The welcome message in your first chat"])
	require.Equal(t, archivalOpenAIModel, jobs["Memory extraction and scratchpad updates"])
	require.Equal(t, archivalClaudeModel, jobs["Memory and scratchpad updates, for Claude chats only"])
	require.Equal(t, models.UtilityModelName, jobs["Naming chats and picking expressions"])
}
