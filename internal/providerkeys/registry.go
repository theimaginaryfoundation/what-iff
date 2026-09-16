package providerkeys

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Registry answers, for one request, which model providers the account making
// it can actually use — and with which credential.
//
// This replaces asking whether a provider client is nil. That test conflated
// two different questions: whether the process built a client (decided once, at
// startup, from environment variables) and whether the caller has a credential
// (a per-request question once keys belong to accounts). Keeping them
// conflated is what made OpenAI the only provider whose key could be supplied
// per account.
type Registry struct {
	resolvers map[models.ModelProvider]*Resolver
}

// DeploymentKeys are the process-wide fallbacks, read from the environment.
// They remain the lowest tier of the lookup so an existing install keeps
// working and an operator can still configure a single-user instance from a
// file, but an account's own key takes precedence.
type DeploymentKeys struct {
	OpenAI    string
	Anthropic string
	ZAI       string
	Gemini    string
	Mistral   string
	DeepSeek  string
	Qwen      string
	Xiaomi    string
}

// NewRegistry builds a resolver per provider.
func NewRegistry(ds *datastore.Datastore, fallbacks DeploymentKeys) *Registry {
	byProvider := map[models.ModelProvider]string{
		models.ModelProviderOpenAI:    fallbacks.OpenAI,
		models.ModelProviderAnthropic: fallbacks.Anthropic,
		models.ModelProviderZAI:       fallbacks.ZAI,
		models.ModelProviderGoogle:    fallbacks.Gemini,
		models.ModelProviderMistral:   fallbacks.Mistral,
		models.ModelProviderDeepSeek:  fallbacks.DeepSeek,
		models.ModelProviderQwen:      fallbacks.Qwen,
		models.ModelProviderXiaomi:    fallbacks.Xiaomi,
	}
	r := &Registry{resolvers: make(map[models.ModelProvider]*Resolver, len(byProvider))}
	for p, fallback := range byProvider {
		r.resolvers[p] = NewResolver(ds, string(p), fallback)
	}
	return r
}

// Key returns the credential the caller should use for provider p, or "" when
// they have none.
func (r *Registry) Key(ctx context.Context, p models.ModelProvider) string {
	if r == nil {
		return ""
	}
	res, ok := r.resolvers[p]
	if !ok {
		return ""
	}
	return res.Resolve(ctx)
}

// Configured reports whether the caller can use provider p at all. This is the
// replacement for a nil-provider check, and unlike one it is specific to the
// account making the request.
func (r *Registry) Configured(ctx context.Context, p models.ModelProvider) bool {
	return r.Key(ctx, p) != ""
}

// Invalidate drops an account's cached credentials across every provider,
// called after a write so the next request sees the change immediately.
func (r *Registry) Invalidate(userID uuid.UUID) {
	if r == nil {
		return
	}
	for _, res := range r.resolvers {
		res.Invalidate(userID)
	}
}

// ResolverFor exposes one provider's resolver, for callers that need to pass a
// resolution function rather than ask a question (the HTTP transport).
func (r *Registry) ResolverFor(p models.ModelProvider) *Resolver {
	if r == nil {
		return nil
	}
	return r.resolvers[p]
}
