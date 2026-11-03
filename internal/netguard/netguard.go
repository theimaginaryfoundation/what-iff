// Package netguard keeps server-side fetches of user-chosen addresses on the public internet.
// The check runs on the resolved address just before each connection, so a name that resolves
// to an internal address, or is rebound to one after a first check, still cannot reach the
// server's own network or the cloud metadata service.
package netguard

import (
	"fmt"
	"net"
	"syscall"
	"time"
)

// cgnat is the shared address space (100.64.0.0/10), which net.IP.IsPrivate does not cover.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPublicIP reports whether ip is an address on the public internet: not loopback,
// private, link-local (including the cloud metadata address), shared, multicast or unspecified.
func IsPublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !cgnat.Contains(ip)
}

// PublicOnlyControl is a net.Dialer Control hook that refuses any address IsPublicIP rejects.
//
// A Control hook runs after DNS resolution, once per address the dialer tries, so address is
// always an IP literal and port, never the hostname that was dialed: a name is checked by what it
// resolved to. Something that is not an IP literal therefore never reaches here from a dialer,
// and is refused rather than trusted if it does (TestHostnamesAreCheckedByWhatTheyResolveTo).
func PublicOnlyControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !IsPublicIP(ip) {
		return fmt.Errorf("refusing to connect to non-public address %s", host)
	}
	return nil
}

// PublicOnlyDialer is a dialer that connects to public addresses only.
func PublicOnlyDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: PublicOnlyControl}
}
