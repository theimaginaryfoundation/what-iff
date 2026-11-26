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
		parsed := net.ParseIP(ip)
		require.NotNil(t, parsed, ip) // IsPublicIP(nil) is false, so a typo would pass silently
		assert.False(t, IsPublicIP(parsed), ip)
	}
}

// Each range IsPublicIP must refuse, at its first and last address, with the nearest public
// address on either side where there is one. This pins the boundary independently of how
// IsPublicIP computes it (net.IP predicates or nonPublicNets), so a regression in either fails.
func TestIsPublicIPRangeBoundaries(t *testing.T) {
	// A nil IP (a resolver entry without an address) is refused, not a panic.
	require.False(t, IsPublicIP(nil))

	refused := map[string][]string{
		"RFC 1918 10/8":              {"10.0.0.0", "10.255.255.255"},
		"RFC 1918 172.16/12":         {"172.16.0.0", "172.31.255.255"},
		"RFC 1918 192.168/16":        {"192.168.0.0", "192.168.255.255"},
		"loopback 127/8":             {"127.0.0.0", "127.255.255.255", "::1"},
		"link-local and metadata":    {"169.254.0.0", "169.254.169.254", "169.254.255.255", "fe80::", "febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		"ULA fc00::/7":               {"fc00::", "fd00:ec2::254", "fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		"CGNAT 100.64/10":            {"100.64.0.0", "100.127.255.255"},
		"this network 0/8":           {"0.0.0.0", "0.255.255.255"},
		"multicast":                  {"224.0.0.0", "239.255.255.255", "ff02::1"},
		"broadcast":                  {"255.255.255.255"},
		"IPv4-mapped internal":       {"::ffff:10.0.0.1", "::ffff:127.0.0.1", "::ffff:169.254.169.254"},
		"NAT64 embedding metadata":   {"64:ff9b::a9fe:a9fe", "64:ff9b:1::a9fe:a9fe"},
		"6to4 embedding private":     {"2002:0a00:0001::1"},
		"IETF protocol 192.0.0/24":   {"192.0.0.0", "192.0.0.255"},
		"benchmarking 198.18/15":     {"198.18.0.0", "198.19.255.255"},
		"documentation":              {"192.0.2.1", "198.51.100.1", "203.0.113.1", "2001:db8::1"},
		"deprecated site-local fec0": {"fec0::", "feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
	}
	for name, ips := range refused {
		for _, ip := range ips {
			parsed := net.ParseIP(ip)
			require.NotNil(t, parsed, ip)
			assert.False(t, IsPublicIP(parsed), "%s: %s must be refused", name, ip)
		}
	}
	// Neighbours just outside the refused ranges stay public, so the ranges are not too wide.
	for _, ip := range []string{"9.255.255.255", "11.0.0.0", "172.15.255.255", "172.32.0.0", "192.167.255.255", "192.169.0.0", "100.63.255.255", "100.128.0.0", "1.0.0.0", "198.17.255.255", "198.20.0.0", "8.8.8.8", "::ffff:8.8.8.8", "2606:4700::6810:85e5"} {
		parsed := net.ParseIP(ip)
		require.NotNil(t, parsed, ip)
		assert.True(t, IsPublicIP(parsed), "%s must stay public", ip)
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

	guarded := NewHTTPClient(PublicOnlyControl, 5*time.Second)
	// No proxy: through one, the checked address would be the proxy's, not the server's.
	require.Nil(t, guarded.Transport.(*http.Transport).Proxy)
	require.NotNil(t, guarded.CheckRedirect)
	_, err := guarded.Get(srv.URL)
	require.True(t, errors.Is(err, ErrNonPublicAddress), "got %v", err)

	// A nil control is the public-only default, not "no check".
	_, err = NewHTTPClient(nil, 5*time.Second).Get(srv.URL)
	require.True(t, errors.Is(err, ErrNonPublicAddress), "nil control must fail closed, got %v", err)

	for _, control := range []func(string, string, syscall.RawConn) error{PublicOrLoopbackControl, NoAddressCheck} {
		resp, err := NewHTTPClient(control, 5*time.Second).Get(srv.URL)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
}

// Redirects stay on the original origin: a different port is another origin, so neither a
// credential nor a replayed body can follow it there.
func TestNewHTTPClientFollowsOnlySameOriginRedirects(t *testing.T) {
	// Called directly with no history, it refuses rather than panicking on via[0].
	assert.Error(t, SameOriginRedirects(httptest.NewRequest(http.MethodGet, "https://a.example/", nil), nil))

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
