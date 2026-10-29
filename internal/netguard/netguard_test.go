package netguard

import (
	"context"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlyPublicAddressesAreAllowed(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.5", "172.16.3.4", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "fe80::1", "0.0.0.0", "224.0.0.1"} {
		assert.False(t, IsPublicIP(net.ParseIP(ip)), ip)
		assert.Error(t, PublicOnlyControl("tcp", net.JoinHostPort(ip, "443"), nil), ip)
	}
	for _, ip := range []string{"162.159.128.233", "8.8.8.8", "2606:4700::6810:85e5"} {
		assert.True(t, IsPublicIP(net.ParseIP(ip)), ip)
		assert.NoError(t, PublicOnlyControl("tcp", net.JoinHostPort(ip, "443"), nil), ip)
	}
}

// The dialer resolves a hostname before the hook runs, so the hook sees IP literals: a public
// name is allowed by its public addresses, and a name for an internal one is refused by them.
func TestHostnamesAreCheckedByWhatTheyResolveTo(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	var seen []string
	d := PublicOnlyDialer(time.Second)
	d.Control = func(network, address string, c syscall.RawConn) error {
		seen = append(seen, address)
		return PublicOnlyControl(network, address, c)
	}
	_, err = d.DialContext(context.Background(), "tcp", net.JoinHostPort("localhost", port))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to connect to non-public address")

	require.NotEmpty(t, seen)
	for _, address := range seen {
		host, _, err := net.SplitHostPort(address)
		require.NoError(t, err)
		assert.NotNil(t, net.ParseIP(host), "the hook got %q, not an IP literal", address)
	}

	assert.Error(t, PublicOnlyControl("tcp", "api.whatiff.chat:443", nil), "an unresolved name is refused, never trusted")
}
