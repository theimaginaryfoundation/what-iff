package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Special-use ranges that embed an internal IPv4 address (NAT64, 6to4) or are otherwise not
// routable are refused, though Go's own predicates call them global unicast.
func TestSpecialUseRangesAreNotPublic(t *testing.T) {
	for _, ip := range []string{"64:ff9b::a9fe:a9fe", "64:ff9b::a00:1", "64:ff9b:1::1", "2002:a9fe:a9fe::1", "0.1.2.3", "192.0.0.8", "198.18.0.1", "fec0::1", "192.0.2.1", "2001:db8::1"} {
		assert.False(t, IsPublicIP(net.ParseIP(ip)), ip)
	}
}

func TestLoopbackOptInAllowsOnlyLoopback(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1"} {
		assert.NoError(t, PublicOrLoopbackControl("tcp", net.JoinHostPort(ip, "80"), nil), ip)
		assert.ErrorIs(t, PublicOnlyControl("tcp", net.JoinHostPort(ip, "80"), nil), ErrNonPublicAddress, ip)
	}
	for _, ip := range []string{"10.0.0.5", "169.254.169.254", "100.64.0.1", "fc00::1"} {
		assert.ErrorIs(t, PublicOrLoopbackControl("tcp", net.JoinHostPort(ip, "80"), nil), ErrNonPublicAddress, ip)
	}
}

// The client refuses a loopback server (standing in for any non-public address) at connect time,
// and the error stays recognisable through the HTTP client's wrapping.
func TestNewHTTPClientChecksEveryConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	_, err := NewHTTPClient(PublicOnlyControl, 5*time.Second).Get(srv.URL)
	require.True(t, errors.Is(err, ErrNonPublicAddress), "got %v", err)

	for _, control := range []func(string, string, syscall.RawConn) error{PublicOrLoopbackControl, nil} {
		resp, err := NewHTTPClient(control, 5*time.Second).Get(srv.URL)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
}

// Redirects stay on the original origin: a different port is another origin, so neither a
// credential nor a replayed body can follow it there.
func TestNewHTTPClientFollowsOnlySameOriginRedirects(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, other.URL+"/", http.StatusTemporaryRedirect)
		case "/here":
			http.Redirect(w, r, "/done", http.StatusTemporaryRedirect)
		}
	}))
	defer srv.Close()
	c := NewHTTPClient(PublicOrLoopbackControl, 5*time.Second)

	resp, err := c.Get(srv.URL + "/here")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, "/done", resp.Request.URL.Path)

	_, err = c.Get(srv.URL + "/away")
	assert.ErrorContains(t, err, "refusing redirect")
}
