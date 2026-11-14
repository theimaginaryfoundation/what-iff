package sharelink

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ProxySecretHeader is the header a deployment's proxy sends its secret in, which proves a request
// came through it, so the address header it set can be trusted (Config.ProxySecret).
const ProxySecretHeader = "X-Client-IP-Proxy-Secret"

// visitorAddress is the address a request comes from (ADR 0x051 §4): the value of header, when
// the deployment names one its proxy sets and a visitor cannot (CloudFront-Viewer-Address) and the
// request proves it came through that proxy (secret, when set), else the connection's. An IPv6
// address counts as its /64, as one machine holds a whole /64. ok is false when there is none.
func visitorAddress(r *http.Request, header, secret string) (netip.Prefix, bool) {
	viaProxy := secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(ProxySecretHeader)), []byte(secret)) == 1
	if header != "" && viaProxy {
		if addr, ok := parseAddress(r.Header.Get(header)); ok {
			return prefixOf(addr), true
		}
	}
	if addr, ok := parseAddress(r.RemoteAddr); ok {
		return prefixOf(addr), true
	}
	return netip.Prefix{}, false
}

// parseAddress reads an address, with or without a port: "203.0.113.7", "203.0.113.7:443",
// "[2001:db8::1]:443", and CloudFront's "2001:db8::1:443" (an IPv6 address and a port, unbracketed).
func parseAddress(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if ap, err := netip.ParseAddrPort(raw); err == nil {
		return ap.Addr().Unmap(), true
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if a, err := netip.ParseAddr(host); err == nil {
			return a.Unmap(), true
		}
	}
	// CloudFront's IPv6 form has a port after the last colon, without brackets: try that first,
	// as an address that ends in a port-sized group parses as an address too.
	if i := strings.LastIndexByte(raw, ':'); i > 0 && strings.Count(raw, ":") > 1 {
		if a, err := netip.ParseAddr(raw[:i]); err == nil && a.Is6() {
			return a, true
		}
	}
	if a, err := netip.ParseAddr(raw); err == nil {
		return a.Unmap(), true
	}
	return netip.Addr{}, false
}

func prefixOf(addr netip.Addr) netip.Prefix {
	bits := 32
	if addr.Is6() {
		bits = 64
	}
	p, _ := addr.Prefix(bits)
	return p
}

// addressKey is what an address is stored as: a keyed hash, so the database never holds addresses.
func addressKey(secret []byte, p netip.Prefix) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(p.String()))
	return hex.EncodeToString(mac.Sum(nil))
}
