package relaykit

import (
	"fmt"
	"net"
	"syscall"

	"github.com/theimaginaryfoundation/what-iff/internal/netguard"
)

// IsPublicIP reports whether ip is an address on the public internet (netguard.IsPublicIP).
func IsPublicIP(ip net.IP) bool { return netguard.IsPublicIP(ip) }

// PublicOnlyControl is netguard.PublicOnlyControl with the caller named in the error ("discord
// attachment"). It runs with the resolved address just before each connection is made, so a name
// that resolves to an internal address (or is rebound to one) still cannot reach the server's own
// network.
func PublicOnlyControl(what string) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		if err := netguard.PublicOnlyControl(network, address, c); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	}
}
