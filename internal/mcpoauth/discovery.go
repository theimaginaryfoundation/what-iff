package mcpoauth

// Discovery (issue #517): reading how to sign in to an MCP server from the server itself,
// as the MCP authorization spec lays out, so a connector needs only its URL:
//
//  1. Ask the server for something without a token. It answers 401 with a WWW-Authenticate
//     challenge that may name its Protected Resource Metadata (RFC 9728) and the scopes it wants.
//  2. Read that metadata, from the challenge's URL or the well-known ones, for the authorization
//     servers that issue its tokens.
//  3. Read the authorization server's metadata (RFC 8414, or OpenID Connect discovery) for its
//     endpoints, and check that it is the issuer it was asked for and that it does PKCE with S256.
//
// A server with no Protected Resource Metadata, written to the spec's first version, is read
// as its own authorization server (its origin's RFC 8414 metadata), still with the issuer and
// PKCE checks.
//
// Every fetch here goes to an address a person typed or a remote document named, so it goes
// through the discovery client: public addresses only (netguard), https, short timeouts, small
// bodies, and no credentials of the deployment's own.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
	"github.com/theimaginaryfoundation/what-iff/internal/netguard"
)

// Discovery is what an MCP server says about signing in to it.
type Discovery struct {
	// Resource is the canonical URL of the MCP server, sent as the RFC 8707 resource
	// parameter so the token is issued for it.
	Resource string `json:"resource"`
	// ResourceName is the server's own name for itself, when its metadata gives one.
	ResourceName string `json:"resource_name,omitempty"`
	// Issuer is the authorization server, as its metadata names itself.
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	// RegistrationEndpoint is where RFC 7591 Dynamic Client Registration is done, if offered.
	RegistrationEndpoint string `json:"registration_endpoint,omitempty"`
	// Scopes are the scopes to ask for: the challenge's, else the resource's scopes_supported.
	Scopes []string `json:"scopes,omitempty"`
	// ClientIDMetadataDocumentSupported is whether the authorization server takes a URL
	// as a client ID (Client ID Metadata Documents).
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
	// IssParameterSupported is whether the authorization server always returns iss on its
	// callback (RFC 9207), which the callback then requires.
	IssParameterSupported bool `json:"iss_parameter_supported"`
	// TokenEndpointAuthMethods are how the token endpoint takes a client's credentials.
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods,omitempty"`
	// Legacy is set when the server has no Protected Resource Metadata and was read as its
	// own authorization server.
	Legacy bool `json:"legacy,omitempty"`
}

var (
	// ErrNoAuthRequired is a server that answered without asking for credentials and
	// publishes no Protected Resource Metadata: it needs no sign-in.
	ErrNoAuthRequired = errors.New("this server didn't ask for sign-in")
	// ErrDiscovery is a server whose sign-in settings couldn't be read; the message says why.
	ErrDiscovery = errors.New("couldn't read this server's sign-in settings")
)

func discoveryErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDiscovery, fmt.Sprintf(format, args...))
}

const (
	discoveryTimeout = 10 * time.Second
	maxMetadataBytes = 256 << 10
	maxRedirects     = 3
)

// newDiscoveryClient is the client every discovery and registration request goes through:
// public addresses only, unless the deployment allows localhost MCP servers (local
// development), when loopback is allowed too. Never the credential transport.
func newDiscoveryClient(allowLocalhost bool) *http.Client {
	dialer := netguard.PublicOnlyDialer(discoveryTimeout)
	if allowLocalhost {
		dialer.Control = func(network, address string, c syscall.RawConn) error {
			if host, _, err := net.SplitHostPort(address); err == nil {
				if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
					return nil
				}
			}
			return netguard.PublicOnlyControl(network, address, c)
		}
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   discoveryTimeout,
		ResponseHeaderTimeout: discoveryTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   2 * discoveryTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return checkFetchURL(req.URL, allowLocalhost)
		},
	}
}

// checkFetchURL refuses an address discovery won't fetch: anything but https, except
// plain http to localhost where the deployment allows localhost MCP servers.
func checkFetchURL(u *url.URL, allowLocalhost bool) error {
	if u == nil || u.Host == "" {
		return errors.New("not an absolute URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowLocalhost && isLoopbackHost(u.Hostname()) {
			return nil
		}
		return errors.New("only https addresses are used for sign-in")
	}
	return fmt.Errorf("unsupported scheme %q", u.Scheme)
}

func isLoopbackHost(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// CanonicalResource is the canonical form of an MCP server URL for the resource
// parameter: lowercase scheme and host, no fragment or query, no trailing slash.
func CanonicalResource(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("not an absolute http(s) URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment, u.RawFragment, u.RawQuery, u.ForceQuery = "", "", "", false
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

// Discover reads how to sign in to the MCP server at serverURL.
func (s *Service) Discover(ctx context.Context, serverURL string) (*Discovery, error) {
	server, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return nil, discoveryErr("the server URL isn't a valid URL")
	}
	if err := checkFetchURL(server, s.allowLocalhost); err != nil {
		return nil, discoveryErr("%v", err)
	}
	resource, err := CanonicalResource(serverURL)
	if err != nil {
		return nil, discoveryErr("%v", err)
	}

	challenge, err := s.probe(ctx, server)
	if err != nil {
		return nil, err
	}

	prmURLs := protectedResourceMetadataURLs(server)
	if challenge.resourceMetadata != "" {
		u, err := url.Parse(challenge.resourceMetadata)
		if err != nil || !u.IsAbs() {
			return nil, discoveryErr("the server named an invalid resource_metadata address")
		}
		prmURLs = []string{u.String()}
	}
	var prm *protectedResourceMetadata
	for _, u := range prmURLs {
		prm, err = s.fetchProtectedResourceMetadata(ctx, u)
		if err == nil {
			break
		}
		if !errors.Is(err, errMetadataNotFound) {
			return nil, err
		}
	}

	d := &Discovery{Resource: resource}
	var issuers []string
	switch {
	case prm != nil:
		if prm.Resource != "" {
			if !sameResource(resource, prm.Resource) {
				return nil, discoveryErr("the server's metadata describes another resource (%s)", prm.Resource)
			}
			if r, err := CanonicalResource(prm.Resource); err == nil {
				d.Resource = r
			}
		}
		d.ResourceName = strings.TrimSpace(prm.ResourceName)
		issuers = prm.AuthorizationServers
		d.Scopes = prm.ScopesSupported
	case challenge.status == http.StatusUnauthorized || challenge.status == http.StatusForbidden:
		// Asked for credentials, but publishes no Protected Resource Metadata: the spec's first
		// version, where the server is its own authorization server.
		issuers = []string{server.Scheme + "://" + server.Host}
		d.Legacy = true
	case challenge.status >= 200 && challenge.status <= 299:
		return nil, ErrNoAuthRequired
	default:
		return nil, discoveryErr("the server answered HTTP %d and publishes no sign-in metadata", challenge.status)
	}
	if len(challenge.scopes) > 0 {
		// The challenge's scopes are what this server wants now (MCP authorization, scope selection).
		d.Scopes = challenge.scopes
	}

	as, err := s.firstAuthorizationServer(ctx, issuers)
	if err != nil {
		if d.Legacy && errors.Is(err, errMetadataNotFound) {
			return nil, discoveryErr("the server asks for sign-in but publishes no OAuth metadata; enter its OAuth settings by hand")
		}
		return nil, err
	}
	d.Issuer = as.Issuer
	d.AuthorizationEndpoint = as.AuthorizationEndpoint
	d.TokenEndpoint = as.TokenEndpoint
	d.RegistrationEndpoint = as.RegistrationEndpoint
	d.ClientIDMetadataDocumentSupported = as.ClientIDMetadataDocumentSupported
	d.IssParameterSupported = as.AuthorizationResponseIssParameterSupported
	d.TokenEndpointAuthMethods = as.TokenEndpointAuthMethodsSupported
	if slices.Contains(as.ScopesSupported, "offline_access") && !slices.Contains(d.Scopes, "offline_access") {
		// Ask for a refresh token where the server offers one, so the connector stays signed in.
		d.Scopes = append(slices.Clone(d.Scopes), "offline_access")
	}
	return d, nil
}

// challenge is what the server said to a request without a token.
type challenge struct {
	status           int
	resourceMetadata string
	scopes           []string
}

// probe sends an MCP initialize with no token and reads the answer's Bearer challenge.
func (s *Service) probe(ctx context.Context, server *url.URL) (challenge, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcpclient.ProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "what-iff", "version": "1"},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.String(), bytes.NewReader(body))
	if err != nil {
		return challenge{}, discoveryErr("%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := s.discoveryClient.Do(req)
	if err != nil {
		return challenge{}, discoveryErr("couldn't reach the server: %v", unwrapURLError(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxMetadataBytes))
	c := challenge{status: resp.StatusCode}
	for _, h := range resp.Header.Values("WWW-Authenticate") {
		params, ok := bearerChallengeParams(h)
		if !ok {
			continue
		}
		c.resourceMetadata = strings.TrimSpace(params["resource_metadata"])
		c.scopes = strings.Fields(params["scope"])
		break
	}
	return c, nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// bearerChallengeParams finds the Bearer challenge in a WWW-Authenticate header value and
// returns its auth-params (RFC 9110 §11.6.1), keys lowercased.
func bearerChallengeParams(header string) (map[string]string, bool) {
	lower := strings.ToLower(header)
	i := 0
	for {
		j := strings.Index(lower[i:], "bearer")
		if j < 0 {
			return nil, false
		}
		start := i + j
		end := start + len("bearer")
		before := start == 0 || lower[start-1] == ' ' || lower[start-1] == ','
		after := end == len(lower) || lower[end] == ' '
		if before && after {
			return parseAuthParams(header[end:]), true
		}
		i = end
	}
}

// parseAuthParams reads name=value pairs, values quoted or not, separated by commas, up to
// the next challenge (a token that is not followed by '=').
func parseAuthParams(s string) map[string]string {
	out := map[string]string{}
	for {
		s = strings.TrimLeft(s, " ,")
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return out
		}
		name := strings.TrimSpace(s[:eq])
		if strings.ContainsAny(name, " ,\"") {
			return out // the next challenge's scheme
		}
		s = strings.TrimLeft(s[eq+1:], " ")
		var value string
		if strings.HasPrefix(s, `"`) {
			var b strings.Builder
			k := 1
			for ; k < len(s); k++ {
				if s[k] == '\\' && k+1 < len(s) {
					k++
					b.WriteByte(s[k])
					continue
				}
				if s[k] == '"' {
					break
				}
				b.WriteByte(s[k])
			}
			value = b.String()
			if k < len(s) {
				k++
			}
			s = s[k:]
		} else {
			k := strings.IndexByte(s, ',')
			if k < 0 {
				k = len(s)
			}
			value = strings.TrimSpace(s[:k])
			s = s[k:]
		}
		out[strings.ToLower(name)] = value
	}
}

// protectedResourceMetadataURLs are the well-known addresses of a server's Protected
// Resource Metadata, in the order the spec tries them: the path-inserted one, then the root.
func protectedResourceMetadataURLs(server *url.URL) []string {
	origin := server.Scheme + "://" + server.Host
	path := strings.TrimRight(server.EscapedPath(), "/")
	root := origin + "/.well-known/oauth-protected-resource"
	if path == "" {
		return []string{root}
	}
	return []string{root + path, root}
}

// authorizationServerMetadataURLs are where an issuer's metadata may be, in the order the
// spec tries them: RFC 8414 and OpenID Connect with the path inserted after the well-known
// segment, then OpenID Connect with it appended.
func authorizationServerMetadataURLs(issuer *url.URL) []string {
	origin := issuer.Scheme + "://" + issuer.Host
	path := strings.TrimRight(issuer.EscapedPath(), "/")
	if path == "" {
		return []string{
			origin + "/.well-known/oauth-authorization-server",
			origin + "/.well-known/openid-configuration",
		}
	}
	return []string{
		origin + "/.well-known/oauth-authorization-server" + path,
		origin + "/.well-known/openid-configuration" + path,
		origin + path + "/.well-known/openid-configuration",
	}
}

// sameResource reports whether the resource a server's metadata names is the server the
// person gave: the same origin, and a path that is the server's or a parent of it.
func sameResource(serverResource, metadataResource string) bool {
	a, err1 := url.Parse(serverResource)
	b, err2 := url.Parse(strings.TrimSpace(metadataResource))
	if err1 != nil || err2 != nil {
		return false
	}
	if !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Host, b.Host) {
		return false
	}
	ap, bp := strings.TrimRight(a.Path, "/"), strings.TrimRight(b.Path, "/")
	return ap == bp || bp == "" || strings.HasPrefix(ap, bp+"/")
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
	ResourceName         string   `json:"resource_name"`
}

type authorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	ScopesSupported                            []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	ClientIDMetadataDocumentSupported          bool     `json:"client_id_metadata_document_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

// errMetadataNotFound is a metadata address with nothing there; the next one is tried.
var errMetadataNotFound = errors.New("no metadata at this address")

func (s *Service) fetchProtectedResourceMetadata(ctx context.Context, rawURL string) (*protectedResourceMetadata, error) {
	var prm protectedResourceMetadata
	if err := s.fetchJSON(ctx, rawURL, &prm); err != nil {
		return nil, err
	}
	var servers []string
	for _, a := range prm.AuthorizationServers {
		if a = strings.TrimSpace(a); a != "" {
			servers = append(servers, a)
		}
	}
	if len(servers) == 0 {
		return nil, discoveryErr("the server's metadata names no authorization server")
	}
	prm.AuthorizationServers = servers
	return &prm, nil
}

// firstAuthorizationServer reads the metadata of the first issuer that has usable metadata.
// Another failure (metadata that names another issuer, no PKCE) is reported over a mere absence.
func (s *Service) firstAuthorizationServer(ctx context.Context, issuers []string) (*authorizationServerMetadata, error) {
	var firstErr error
	for _, issuer := range issuers {
		as, err := s.fetchAuthorizationServerMetadata(ctx, issuer)
		if err == nil {
			return as, nil
		}
		if firstErr == nil && !errors.Is(err, errMetadataNotFound) {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("%w (%w)", discoveryErr("the authorization server %s publishes no OAuth metadata", issuers[0]), errMetadataNotFound)
}

func (s *Service) fetchAuthorizationServerMetadata(ctx context.Context, issuer string) (*authorizationServerMetadata, error) {
	issuerURL, err := url.Parse(issuer)
	if err != nil || !issuerURL.IsAbs() || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, discoveryErr("the authorization server %q isn't a valid issuer", issuer)
	}
	if err := checkFetchURL(issuerURL, s.allowLocalhost); err != nil {
		return nil, discoveryErr("authorization server %s: %v", issuer, err)
	}
	for _, u := range authorizationServerMetadataURLs(issuerURL) {
		var as authorizationServerMetadata
		err := s.fetchJSON(ctx, u, &as)
		if errors.Is(err, errMetadataNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// The metadata must be the issuer's own: one claiming another issuer is not used
		// (RFC 8414 §3.3, MCP authorization server discovery).
		if as.Issuer != issuer {
			return nil, discoveryErr("the authorization server's metadata names another issuer (%q, not %q)", as.Issuer, issuer)
		}
		if err := s.checkEndpoint("authorization_endpoint", as.AuthorizationEndpoint); err != nil {
			return nil, err
		}
		if err := s.checkEndpoint("token_endpoint", as.TokenEndpoint); err != nil {
			return nil, err
		}
		if as.RegistrationEndpoint != "" {
			if err := s.checkEndpoint("registration_endpoint", as.RegistrationEndpoint); err != nil {
				as.RegistrationEndpoint = "" // unusable; another way to register may still work
			}
		}
		// PKCE with S256 is required; a server whose metadata doesn't say it supports it is
		// not signed in to (MCP authorization, PKCE).
		if !slices.Contains(as.CodeChallengeMethodsSupported, "S256") {
			return nil, discoveryErr("the authorization server %s doesn't say it supports PKCE (S256), which sign-in requires", issuer)
		}
		return &as, nil
	}
	return nil, errMetadataNotFound
}

func (s *Service) checkEndpoint(name, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return discoveryErr("the authorization server's %s is missing or invalid", name)
	}
	if err := checkFetchURL(u, s.allowLocalhost); err != nil {
		return discoveryErr("the authorization server's %s: %v", name, err)
	}
	return nil
}

// fetchJSON GETs a metadata document into out: errMetadataNotFound when there is none there.
func (s *Service) fetchJSON(ctx context.Context, rawURL string, out any) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return discoveryErr("invalid metadata address")
	}
	if err := checkFetchURL(u, s.allowLocalhost); err != nil {
		return discoveryErr("%s: %v", rawURL, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return discoveryErr("%v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.discoveryClient.Do(req)
	if err != nil {
		return discoveryErr("couldn't fetch %s: %v", rawURL, unwrapURLError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return discoveryErr("couldn't read %s: %v", rawURL, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone,
		resp.StatusCode == http.StatusMethodNotAllowed, resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return errMetadataNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return discoveryErr("%s answered HTTP %d", rawURL, resp.StatusCode)
	case len(body) > maxMetadataBytes:
		return discoveryErr("%s is too large", rawURL)
	}
	if err := json.Unmarshal(body, out); err != nil {
		// An HTML page (a single-page app answering every path) is not metadata.
		return errMetadataNotFound
	}
	return nil
}
