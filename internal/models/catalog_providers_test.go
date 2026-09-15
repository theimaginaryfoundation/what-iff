package models

import "testing"

// The list drives the integrations screen, so it has to match the catalog
// rather than a second hand-maintained copy that can drift from it.
func TestCatalogProvidersMatchesSeededModels(t *testing.T) {
	got := CatalogProviders()
	if len(got) == 0 {
		t.Fatal("no providers derived from AvailableModels")
	}
	seen := map[ModelProvider]bool{}
	for _, p := range got {
		if seen[p] {
			t.Fatalf("duplicate provider %q", p)
		}
		seen[p] = true
	}
	// Every seeded model's provider must be represented.
	for _, m := range AvailableModels {
		p := ProviderForModel(string(m.Provider), m.Name)
		if !seen[p] {
			t.Fatalf("model %q has provider %q, missing from CatalogProviders()", m.Name, p)
		}
	}
	t.Logf("derived: %v", got)
}

func TestIsCatalogProviderRejectsUnknown(t *testing.T) {
	if !IsCatalogProvider("openai") {
		t.Fatal("openai must be a catalog provider")
	}
	if IsCatalogProvider("definitely-not-a-provider") {
		t.Fatal("unknown provider accepted; a key stored for it would never be read")
	}
}
