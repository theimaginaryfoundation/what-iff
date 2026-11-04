package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// Models from provider connections (ADR 0x027). A model from a built-in
// provider's connection goes through that provider's own path with the
// account's key, like any built-in model. A model from OpenRouter, a server on
// this computer or a Custom server is reached through a client built for its
// connection, at the connection's address with the connection's key.

// ConnectionFlavor is the API a connection speaks.
type ConnectionFlavor string

const (
	ConnectionFlavorOpenAI    ConnectionFlavor = "openai"
	ConnectionFlavorAnthropic ConnectionFlavor = "anthropic"
)

// ConnectionEndpoint is where a connection is reached, and with what key.
type ConnectionEndpoint struct {
	// Preset is the preset the connection was made from (openrouter, ollama,
	// lmstudio, custom).
	Preset  string
	Flavor  ConnectionFlavor
	BaseURL string
	// Key is the connection's key, decrypted, or empty for a server that has none.
	Key string
}

// ConnectionResolver finds an account's connection. It refuses a connection the
// account does not own.
type ConnectionResolver interface {
	Endpoint(ctx context.Context, userID, connectionID uuid.UUID) (ConnectionEndpoint, error)
}

// ConnectionResolverFunc adapts a function to ConnectionResolver.
type ConnectionResolverFunc func(ctx context.Context, userID, connectionID uuid.UUID) (ConnectionEndpoint, error)

// Endpoint calls f.
func (f ConnectionResolverFunc) Endpoint(ctx context.Context, userID, connectionID uuid.UUID) (ConnectionEndpoint, error) {
	return f(ctx, userID, connectionID)
}

// ErrConnectionUnavailable is returned for a model whose connection can't be
// used: removed, not the account's, or not reachable on this deployment.
var ErrConnectionUnavailable = errors.New("this model's provider connection is not available")

// ErrNoModelSelected is returned for a turn with no model to run on: its chat
// names none and the account has no default, having enabled no model yet.
var ErrNoModelSelected = errors.New("no model is selected for this account")

// ErrModelDisabled is returned for a connection model the account turned off.
var ErrModelDisabled = errors.New("this model is turned off")

// connectionFor returns the connection that serves a model, or nil for a model
// that goes through a built-in provider's own path. A model needs its own
// connection's client when it belongs to a connection whose preset is not a
// built-in provider.
func (a *Agent) connectionFor(ctx context.Context, userID uuid.UUID, m *models.Model) (*ConnectionEndpoint, error) {
	if m == nil || m.ConnectionID == nil {
		return nil, nil
	}
	if m.Deleted {
		return nil, ErrModelDisabled
	}
	if models.ModelProvider(m.Provider).Known() {
		return nil, nil
	}
	if a.connections == nil {
		return nil, ErrConnectionUnavailable
	}
	ep, err := a.connections.Endpoint(ctx, userID, *m.ConnectionID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectionUnavailable, err)
	}
	return &ep, nil
}

// connectionLabels is how a connection's calls are labelled: the telemetry
// dependency, and the provider named on a recorded safety refusal.
func connectionLabels(preset string) (string, models.SafetyViolationProvider) {
	switch preset {
	case "ollama", "lmstudio":
		return telemetry.DependencyLocalLLM, models.SafetyViolationProviderLocal
	case "openrouter":
		return telemetry.DependencyOpenRouter, models.SafetyViolationProviderOpenRouter
	default:
		return telemetry.DependencyOther, models.SafetyViolationProviderCustom
	}
}

// connectionHTTPClient is the client a connection's calls go through: the
// network block, timeouts and instrumentation, without the credential
// transport (which would put the account's built-in keys on requests to the
// built-in providers' hosts).
func (a *Agent) connectionHTTPClient() *http.Client {
	return a.connectionClient
}

// connectionChatProvider builds the Chat Completions client for one turn on
// an OpenAI-compatible connection.
func (a *Agent) connectionChatProvider(ep *ConnectionEndpoint) *provider.ConnectionChatProvider {
	dependency, safety := connectionLabels(ep.Preset)
	return provider.NewConnectionChatProvider(ep.Key, ep.BaseURL, dependency, safety, a.telemetry, a.connectionHTTPClient())
}

// connectionClaudeProvider builds the Messages client for one turn on an
// Anthropic-compatible connection.
func (a *Agent) connectionClaudeProvider(ep *ConnectionEndpoint) *provider.ClaudeProvider {
	dependency, _ := connectionLabels(ep.Preset)
	return provider.NewConnectionClaudeProvider(ep.Key, ep.BaseURL, dependency, a.telemetry, a.connectionHTTPClient())
}
