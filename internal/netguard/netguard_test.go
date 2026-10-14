package netguard

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
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
