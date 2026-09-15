package providerkeys

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// With no datastore the registry still answers from the deployment keys, which
// is the path an install that has never used the settings screen takes.
func TestRegistryFallsBackToDeploymentKeys(t *testing.T) {
	r := NewRegistry(nil, DeploymentKeys{OpenAI: "sk-openai", Anthropic: "sk-ant"})
	ctx := context.Background()

	for _, tc := range []struct {
		provider models.ModelProvider
		want     bool
	}{
		{models.ModelProviderOpenAI, true},
		{models.ModelProviderAnthropic, true},
		{models.ModelProviderGoogle, false},
		{models.ModelProviderZAI, false},
	} {
		if got := r.Configured(ctx, tc.provider); got != tc.want {
			t.Fatalf("%s: configured=%v, want %v", tc.provider, got, tc.want)
		}
	}
}

// The actor on the context is what a per-request answer keys off; with no
// datastore every account sees the deployment answer, which is correct.
func TestRegistryAnswersPerRequest(t *testing.T) {
	r := NewRegistry(nil, DeploymentKeys{OpenAI: "sk-openai"})
	ctx := apicontext.WithUserID(context.Background(), uuid.New())
	if !r.Configured(ctx, models.ModelProviderOpenAI) {
		t.Fatal("deployment key should cover an account with none of its own")
	}
	if r.Configured(ctx, models.ModelProviderMistral) {
		t.Fatal("no key anywhere should not report configured")
	}
}

// An unknown provider must answer false rather than panic: the catalog can
// name a provider the registry was not built with.
func TestRegistryUnknownProvider(t *testing.T) {
	r := NewRegistry(nil, DeploymentKeys{OpenAI: "sk-openai"})
	if r.Configured(context.Background(), models.ModelProvider("nonesuch")) {
		t.Fatal("unknown provider reported as configured")
	}
	if r.Key(context.Background(), models.ModelProvider("nonesuch")) != "" {
		t.Fatal("unknown provider returned a key")
	}
}

// A nil registry is safe: tests and the mock backend construct agents without
// one, and a panic there would be a worse failure than "not configured".
func TestNilRegistryIsSafe(t *testing.T) {
	var r *Registry
	if r.Configured(context.Background(), models.ModelProviderOpenAI) {
		t.Fatal("nil registry reported configured")
	}
	if r.Key(context.Background(), models.ModelProviderOpenAI) != "" {
		t.Fatal("nil registry returned a key")
	}
	r.Invalidate(uuid.New())
	if r.ResolverFor(models.ModelProviderOpenAI) != nil {
		t.Fatal("nil registry returned a resolver")
	}
}
