package provider

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrNetworkDenied is returned by the deny-network transport for every request.
// A dummy API key still lets a request (TCP/TLS handshake included) leave the
// machine; failing at the transport is the only real "no provider egress"
// guarantee for mock mode.
var ErrNetworkDenied = errors.New("provider network egress denied (MOCK_LLM mode)")

// denyNetworkTransport is an http.RoundTripper that rejects every request
// before any connection is attempted.
type denyNetworkTransport struct{}

func (denyNetworkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("%w: %s %s", ErrNetworkDenied, req.Method, req.URL.Redacted())
}

// DenyNetworkHTTPClient returns an *http.Client whose transport fails every
// request with ErrNetworkDenied. Under MOCK_LLM it is injected into every
// external LLM client (shared agent client, provider constructors, and the
// memory/admin handler clients) so no provider call can leave the process.
// Scope is deliberately narrow: it guards provider HTTP clients only — mock
// mode still talks to Postgres, so the guarantee is "no provider egress", not
// "no network at all".
func DenyNetworkHTTPClient() *http.Client {
	return &http.Client{Transport: denyNetworkTransport{}}
}
