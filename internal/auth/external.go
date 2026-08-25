package auth

import (
	"net/http"

	"go.uber.org/zap"
)

// ExternalIdentity is a user identity resolved from an upstream identity provider.
type ExternalIdentity struct {
	// Subject is the provider's stable, unique identifier for the user.
	Subject string
	// Username is the provider's chosen or preferred username for the user.
	Username string
	// Email is the user's email address.
	Email string
	// FirstName and LastName are optional profile attributes.
	FirstName string
	LastName  string
}

// ExternalAuthenticator resolves an ExternalIdentity from an incoming request.
// The second return value is false when the request carries no credentials this
// authenticator handles, in which case the caller uses the built-in token path.
type ExternalAuthenticator interface {
	Authenticate(r *http.Request) (ExternalIdentity, bool)
}

// ExternalAuthenticatorProvider builds the request authenticator used for
// upstream identity providers. When nil, the built-in token path is the only
// authentication method.
var ExternalAuthenticatorProvider func(logger *zap.Logger) ExternalAuthenticator
