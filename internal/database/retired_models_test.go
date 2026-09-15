package database

import (
	"testing"

	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A name in both lists makes the two seeding steps fight on every fresh
// install: ensureAllModels creates the row, then migrateLegacyModels
// immediately retires it. Nothing breaks, but the model is offered for the
// length of one boot and the intent is unreadable from either list alone.
func TestRetiredModelsAreNotAlsoSeeded(t *testing.T) {
	t.Parallel()

	seeded := make(map[string]struct{}, len(appmodels.AvailableModels))
	for _, m := range appmodels.AvailableModels {
		seeded[m.Name] = struct{}{}
	}

	for _, name := range retiredModelNames {
		if _, clash := seeded[name]; clash {
			t.Errorf("model %q is both seeded in AvailableModels and listed for retirement; remove it from the seed catalog", name)
		}
	}
}

// Retirement is the only thing that removes a model from an install that
// already seeded it, so dropping a name from AvailableModels is not enough on
// its own. This pins the one we know never existed.
func TestNonexistentNanoIsRetired(t *testing.T) {
	t.Parallel()

	const fictional = "gpt-5.1-nano"

	found := false
	for _, name := range retiredModelNames {
		if name == fictional {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%q returns 404 model_not_found from OpenAI and must stay retired; "+
			"existing installs seeded it and only migrateLegacyModels clears it", fictional)
	}
}
