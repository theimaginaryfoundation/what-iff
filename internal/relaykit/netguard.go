package relaykit

import (
	"fmt"
	"net"
	"syscall"
)

// cgnat is the shared address space (100.64.0.0/10), which net.IP.IsPrivate does not cover.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPublicIP reports whether ip is an address on the public internet: not loopback,
// private, link-local (including the cloud metadata address), shared, multicast or unspecified.
func IsPublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !cgnat.Contains(ip)
}

// PublicOnlyControl is a net.Dialer Control hook that refuses any connection to an address
// that is not public. It runs with the resolved address just before each connection is made,
// so a name that resolves to an internal address (or is rebound to one) still cannot reach
// the server's own network. what names the caller in the error ("discord attachment").
func PublicOnlyControl(what string) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip == nil || !IsPublicIP(ip) {
			return fmt.Errorf("%s: refusing to connect to non-public address %s", what, host)
		}
		return nil
	}
}
