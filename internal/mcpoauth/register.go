package mcpoauth

// Client registration (issue #517): how a connector whose sign-in was discovered gets an OAuth
// client at its authorization server, in the order the MCP spec prefers:
//
//  1. A client ID the person registered and typed in.
//  2. A Client ID Metadata Document: the client ID is the https URL of a document this
//     deployment serves about itself (ClientMetadataDocument), which the authorization server
//     fetches. Nothing is registered and nothing is stored at the server.
//  3. RFC 7591 Dynamic Client Registration at the server's registration endpoint.
//
// Without any of them, the person registers the app with the provider and types the client ID in.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// ErrClientRegistrationUnavailable is an authorization server that takes neither a client
// metadata document from this deployment nor dynamic registration: a client ID has to be
// registered by hand.
var ErrClientRegistrationUnavailable = errors.New("this sign-in server needs a client ID registered by hand")

// Registration is an OAuth client a connector signs in as.
type Registration struct {
	// Method is one of models.MCPOAuthRegistration*.
	Method       string
	ClientID     string
	ClientSecret string
}

// clientMetadata is a client's description of itself: the Client ID Metadata Document this
// deployment serves, and the body of a dynamic registration.
type clientMetadata struct {
	ClientID                string   `json:"client_id,omitempty"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	// ApplicationType is "web": a server-side client with an https redirect (OpenID Connect
	// assumes web when it's missing, and the MCP spec asks clients to say).
	ApplicationType string `json:"application_type"`
	ClientURI       string `json:"client_uri,omitempty"`
}

func (s *Service) clientMetadata(authMethod string) clientMetadata {
	m := clientMetadata{
		ClientName:              s.clientName,
		RedirectURIs:            []string{s.redirectURL},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: authMethod,
		ApplicationType:         "web",
	}
	if u, err := url.Parse(s.redirectURL); err == nil && u.Host != "" {
		m.ClientURI = u.Scheme + "://" + u.Host
	}
	return m
}

// ClientMetadataURL is the https URL of this deployment's Client ID Metadata Document, which
// is also the client ID it signs in with where an authorization server takes one; empty when
// the deployment has no public https address to serve it from.
func (s *Service) ClientMetadataURL() string {
	return s.clientMetadataURL
}

// ClientMetadataDocument is the document served at ClientMetadataURL; false when there is none.
func (s *Service) ClientMetadataDocument() (any, bool) {
	if s.clientMetadataURL == "" {
		return nil, false
	}
	m := s.clientMetadata("none")
	m.ClientID = s.clientMetadataURL
	return m, true
}

// Register gets the connector an OAuth client at d's authorization server, by a metadata
// document or dynamic registration, whichever the server takes (metadata document first).
func (s *Service) Register(ctx context.Context, d *Discovery) (Registration, error) {
	if s.clientMetadataURL != "" && d.ClientIDMetadataDocumentSupported {
		return Registration{Method: models.MCPOAuthRegistrationMetadataDocument, ClientID: s.clientMetadataURL}, nil
	}
	if d.RegistrationEndpoint != "" {
		return s.registerDynamically(ctx, d)
	}
	return Registration{}, ErrClientRegistrationUnavailable
}

// RegistrationPlan says how Register would get a client for d, without registering:
// for the setup form to say what will happen.
func (s *Service) RegistrationPlan(d *Discovery) string {
	switch {
	case s.clientMetadataURL != "" && d.ClientIDMetadataDocumentSupported:
		return models.MCPOAuthRegistrationMetadataDocument
	case d.RegistrationEndpoint != "":
		return models.MCPOAuthRegistrationDynamic
	}
	return models.MCPOAuthRegistrationManual
}

type registrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// registerDynamically registers a client with RFC 7591. It asks for a public client (no
// secret, PKCE protects the code) where the server takes one, else a secret sent in the form.
func (s *Service) registerDynamically(ctx context.Context, d *Discovery) (Registration, error) {
	authMethod := "none"
	if len(d.TokenEndpointAuthMethods) > 0 && !slices.Contains(d.TokenEndpointAuthMethods, "none") {
		authMethod = "client_secret_post"
	}
	body, err := json.Marshal(s.clientMetadata(authMethod))
	if err != nil {
		return Registration{}, err
	}
	u, err := url.Parse(d.RegistrationEndpoint)
	if err != nil {
		return Registration{}, discoveryErr("invalid registration_endpoint")
	}
	if err := checkFetchURL(u, s.allowLocalhost); err != nil {
		return Registration{}, discoveryErr("registration_endpoint: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return Registration{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.discoveryClient.Do(req)
	if err != nil {
		return Registration{}, discoveryErr("couldn't register with the sign-in server: %v", unwrapURLError(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes))
	var out registrationResponse
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode < 200 || resp.StatusCode > 299 || strings.TrimSpace(out.ClientID) == "" {
		reason := sanitizeProviderError(strings.TrimSpace(out.Error + " " + out.ErrorDesc))
		if reason == "" {
			reason = http.StatusText(resp.StatusCode)
		}
		return Registration{}, discoveryErr("the sign-in server refused to register this app: %s", reason)
	}
	return Registration{
		Method:       models.MCPOAuthRegistrationDynamic,
		ClientID:     strings.TrimSpace(out.ClientID),
		ClientSecret: strings.TrimSpace(out.ClientSecret),
	}, nil
}

// AutoConfig is a connector's OAuth settings read from its server, with the client it signs
// in as.
type AutoConfig struct {
	Discovery    *Discovery
	Registration Registration
}

// AutoConfigure discovers how to sign in to the server at serverURL and gets a client for it:
// the one given (clientID, and clientSecret if any) when set, else by Register.
func (s *Service) AutoConfigure(ctx context.Context, serverURL, clientID, clientSecret string) (*AutoConfig, error) {
	d, err := s.Discover(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	reg := Registration{Method: models.MCPOAuthRegistrationManual, ClientID: strings.TrimSpace(clientID), ClientSecret: strings.TrimSpace(clientSecret)}
	if reg.ClientID == "" {
		if reg, err = s.Register(ctx, d); err != nil {
			return nil, err
		}
	}
	return &AutoConfig{Discovery: d, Registration: reg}, nil
}

// Apply writes the settings onto a connector. Its client secret is the caller's to store
// (it is encrypted on its own update path).
func (c *AutoConfig) Apply(server *models.MCPServer) {
	d := c.Discovery
	server.AuthMode = models.MCPServerAuthModeOAuth
	server.OAuthAuthURL = d.AuthorizationEndpoint
	server.OAuthTokenURL = d.TokenEndpoint
	server.OAuthScopes = slices.Clone(d.Scopes)
	server.OAuthPKCEPolicy = models.MCPServerPKCERequired
	server.OAuthResource = d.Resource
	server.OAuthIssuer = d.Issuer
	server.OAuthIssParamSupported = d.IssParameterSupported
	server.OAuthClientID = c.Registration.ClientID
	server.OAuthClientRegistration = c.Registration.Method
}
