// Package netguard keeps server-side fetches of user-chosen addresses on the public internet.
// The check runs on the resolved address just before each connection, so a name that resolves
// to an internal address, or is rebound to one after a first check, still cannot reach the
// server's own network or the cloud metadata service.
//
// IsPublicIP is the boundary for every caller at once, so it should only ever get stricter. A
// caller that needs an internal address gets an explicit opt-in at its own call site
// (PublicOrLoopbackControl, NoAddressCheck), never a wider IsPublicIP. Callers tell a refusal
// from a network failure with errors.Is(err, ErrNonPublicAddress).
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrNonPublicAddress is wrapped by every refusal, so callers can tell a refused address
// (permanent until the address changes) from a network failure (worth retrying).
var ErrNonPublicAddress = errors.New("refusing to connect to non-public address")

// nonPublicNets are special-use ranges that net.IP's own predicates do not cover.
// The NAT64 and 6to4 prefixes embed an IPv4 address, which could be an internal one.
var nonPublicNets = mustCIDRs(
	"0.0.0.0/8",       // "this network"
	"100.64.0.0/10",   // shared address space (CGNAT)
	"192.0.0.0/24",    // IETF protocol assignments
	"198.18.0.0/15",   // benchmarking
	"64:ff9b::/96",    // NAT64, well-known prefix
	"64:ff9b:1::/48",  // NAT64, local use
	"2002::/16",       // 6to4
	"fec0::/10",       // deprecated site-local
	"2001:db8::/32",   // documentation
	"192.0.2.0/24",    // documentation (TEST-NET-1)
	"198.51.100.0/24", // documentation (TEST-NET-2)
	"203.0.113.0/24",  // documentation (TEST-NET-3)
)

// mustCIDRs is for the compile-time literals above only; it panics at init on a typo, which
// TestSpecialUseRangesAreNotPublic would also catch.
func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// IsPublicIP reports whether ip is an address on the public internet: not loopback, private,
// link-local (including the cloud metadata address), multicast, unspecified, or one of the
// special-use ranges in nonPublicNets (shared, NAT64, 6to4, documentation, and others).
//
// This is the SSRF boundary for every user-chosen fetch. It leans on net.IP's predicates for
// RFC 1918, ULA, loopback and link-local; TestIsPublicIPRangeBoundaries pins each range it must
// refuse, at both ends, so a change here or in those predicates fails a test rather than opening
// a hole. A range added to or removed from this function belongs in that table too.
func IsPublicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, n := range nonPublicNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// PublicOnlyControl is a net.Dialer Control hook that refuses any address IsPublicIP rejects.
//
// A Control hook runs after DNS resolution, once per address the dialer tries, so address is
// always an IP literal and port, never the hostname that was dialed: a name is checked by what it
// resolved to. Something that is not an IP literal therefore never reaches here from a dialer,
// and is refused rather than trusted if it does (TestHostnamesAreCheckedByWhatTheyResolveTo).
func PublicOnlyControl(_, address string, _ syscall.RawConn) error {
	return check(address, false)
}

// PublicOrLoopbackControl is PublicOnlyControl that also allows loopback, for a local
// development opt-in that should reach a server on this machine and nothing else internal.
func PublicOrLoopbackControl(_, address string, _ syscall.RawConn) error {
	return check(address, true)
}

// Control picks PublicOrLoopbackControl when allowLoopback, else PublicOnlyControl.
func Control(allowLoopback bool) func(network, address string, c syscall.RawConn) error {
	if allowLoopback {
		return PublicOrLoopbackControl
	}
	return PublicOnlyControl
}

func check(address string, allowLoopback bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip != nil && (IsPublicIP(ip) || (allowLoopback && ip.IsLoopback())) {
		return nil
	}
	return fmt.Errorf("%w %s", ErrNonPublicAddress, host)
}

// PublicOnlyDialer is a dialer that connects to public addresses only.
func PublicOnlyDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: PublicOnlyControl}
}

// NoAddressCheck is a Control hook that allows every address. It exists so that turning the
// guard off is a named, greppable choice (a local-network opt-in) rather than a nil that might be
// an oversight.
func NoAddressCheck(_, _ string, _ syscall.RawConn) error { return nil }

// NewHTTPClient is an HTTP client whose every connection passes control: PublicOnlyControl,
// PublicOrLoopbackControl, or NoAddressCheck for an explicit opt-in. A nil control means
// PublicOnlyControl, so a caller that leaves it out fails closed.
//
// Proxy is nil, so HTTPS_PROXY is ignored: through a proxy the checked address would be the
// proxy's, not the server's. Redirects are followed only within the same origin
// (SameOriginRedirects), so credentials and request bodies are never replayed to another host
// or downgraded to http. Everything else (pool limits, TLS and keep-alive timeouts) is
// http.DefaultTransport's, cloned so it stays in step with the standard library.
func NewHTTPClient(control func(network, address string, c syscall.RawConn) error, timeout time.Duration) *http.Client {
	if control == nil {
		control = PublicOnlyControl
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: control}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: SameOriginRedirects,
		Transport:     transport,
	}
}

// SameOriginRedirects is an http.Client CheckRedirect that follows a redirect only to the same
// scheme and host as the original request, at most 10 times. It compares against via[0], the
// first request, on purpose: every hop must stay on the origin the caller chose, so a chain
// cannot walk away from it one hop at a time.
func SameOriginRedirects(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return errors.New("redirect without an original request")
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		return fmt.Errorf("refusing redirect from %s://%s to %s://%s", first.Scheme, first.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}
