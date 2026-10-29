// Package providerkeys answers one question: which model-provider credential
// does this request use?
//
// Before this existed the answer was decided in several places — a key read
// from configuration at startup, handed to a provider client's constructor,
// and separately restated wherever something needed to know whether a provider
// was usable at all. Routing every answer through one interface means there is
// one place to change when the answer stops being the same for everybody.
package providerkeys

import (
	"context"
	"errors"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Resolver answers which credential to use for a provider.
//
// Implementations must be safe for concurrent use, and must tolerate a context
// carrying no actor: background work that belongs to nobody still asks.
type Resolver interface {
	// Key returns the credential for provider, or "" when there is none.
	//
	// An empty result is not an error. It means this caller cannot reach that
	// provider, which callers report as such rather than by substituting a
	// credential belonging to somebody else.
	Key(ctx context.Context, provider models.ModelProvider) string

	// Configured reports whether a client for provider is worth constructing.
	//
	// It answers a startup question, not a per-request one. Where credentials
	// come from configuration it is known then; where they belong to accounts
	// it cannot be, and the honest answer is yes — the per-request Key result
	// decides whether the call can actually be made.
	Configured(provider models.ModelProvider) bool

	// AccountsSupplyKeys reports whether keys come from accounts, which then
	// manage them through the key routes. It is part of the interface, not
	// inferred from the concrete type, so a resolver wrapping another has to
	// answer it.
	AccountsSupplyKeys() bool
}

// DeploymentResolver answers the same for every caller, from the credentials
// the deployment was configured with.
type DeploymentResolver struct {
	keys map[models.ModelProvider]string
}

// NewDeploymentResolver builds a resolver over configured credentials. Blank
// entries are dropped, so a provider left unset is indistinguishable from one
// never mentioned.
func NewDeploymentResolver(keys map[models.ModelProvider]string) DeploymentResolver {
	copied := make(map[models.ModelProvider]string, len(keys))
	for provider, key := range keys {
		if k := strings.TrimSpace(key); k != "" {
			copied[provider] = k
		}
	}
	return DeploymentResolver{keys: copied}
}

// Key returns the configured credential for provider. The context is ignored:
// every caller gets the same answer.
func (r DeploymentResolver) Key(_ context.Context, provider models.ModelProvider) string {
	return r.keys[provider]
}

// Configured reports whether provider has a configured credential.
func (r DeploymentResolver) Configured(provider models.ModelProvider) bool {
	return r.keys[provider] != ""
}

// AccountsSupplyKeys is false: the deployment supplies every key.
func (DeploymentResolver) AccountsSupplyKeys() bool { return false }

// New chooses the resolver the server uses, given the deployment's configured
// keys and the keys accounts store themselves. By default it is AccountKeys:
// each account brings its own keys, added in the app. A build that always
// supplies keys itself sets New in an init().
var New = AccountKeys

// AccountKeys is New's default: requests carry the requesting account's own
// key, and the deployment's configured keys are not used.
func AccountKeys(_ DeploymentResolver, accounts Resolver) Resolver {
	return accounts
}

// NotConfiguredError reports that a request to a provider was not sent because
// the caller has no key for it. It is returned in place of the provider's own
// authentication failure, so callers can say which key is missing.
type NotConfiguredError struct {
	Provider models.ModelProvider
}

func (e *NotConfiguredError) Error() string {
	return "no API key configured for " + string(e.Provider)
}

// HTTPStatusCode reports the failure as 401, the status the provider would
// have answered with. Error classification reads it before the transport-error
// check, so a missing key counts as an authentication failure and is not
// retried as if it were a dropped connection.
func (e *NotConfiguredError) HTTPStatusCode() int { return 401 }

// vendorNames are the names a person would know a provider's key by.
var vendorNames = map[models.ModelProvider]string{
	models.ModelProviderOpenAI:    "OpenAI",
	models.ModelProviderAnthropic: "Anthropic",
	models.ModelProviderZAI:       "z.ai",
	models.ModelProviderGoogle:    "Google Gemini",
	models.ModelProviderMistral:   "Mistral",
	models.ModelProviderDeepSeek:  "DeepSeek",
	models.ModelProviderQwen:      "Qwen",
	models.ModelProviderXiaomi:    "Xiaomi MiMo",
}

// Message states the missing key in words a person can act on. It states the
// fact only; what to do about it is for the client to say.
func (e *NotConfiguredError) Message() string {
	name, ok := vendorNames[e.Provider]
	if !ok {
		name = string(e.Provider)
	}
	return "No " + name + " API key is set."
}

// FailureText is what to record when err ends a job or a chat turn: Message
// when the cause is a missing key, and err.Error() otherwise. A missing key
// would otherwise be recorded as the transport error wrapping it, request URL
// included.
func FailureText(err error) string {
	var notConfigured *NotConfiguredError
	if errors.As(err, &notConfigured) {
		return notConfigured.Message()
	}
	return err.Error()
}
