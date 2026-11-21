package sharelink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strings"
	"time"
)

// sessionCookie is the cookie a visitor holds once they unlocked a share (ADR 0x051 §3), scoped to
// that share's API path, so each share has its own.
const sessionCookie = "wi_share"

// sessionTTL is how long an unlock lasts.
const sessionTTL = 12 * time.Hour

// sessionPath is the API path of a share, which its cookie is scoped to.
func sessionPath(key string) string { return "/api/shared/" + key }

// sessionValue is an unlock of share key at passcode generation gen until exp: the expiry and
// generation, then an HMAC of the key, generation and expiry.
func sessionValue(secret []byte, key string, gen int, exp time.Time) string {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[:8], uint64(exp.Unix()))
	binary.BigEndian.PutUint32(buf[8:], uint32(gen))
	return base64.RawURLEncoding.EncodeToString(append(buf, sessionMAC(secret, key, buf)...))
}

func sessionMAC(secret []byte, key string, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(key))
	mac.Write([]byte{0})
	mac.Write(payload)
	return mac.Sum(nil)
}

// unlocked reports whether the request holds a live unlock of share key at generation gen.
func unlocked(r *http.Request, secret []byte, key string, gen int, now time.Time) bool {
	for _, c := range r.Cookies() {
		if c.Name != sessionCookie {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(c.Value))
		if err != nil || len(raw) != 12+sha256.Size {
			continue
		}
		payload, mac := raw[:12], raw[12:]
		if !hmac.Equal(mac, sessionMAC(secret, key, payload)) {
			continue
		}
		exp := time.Unix(int64(binary.BigEndian.Uint64(payload[:8])), 0)
		if int(binary.BigEndian.Uint32(payload[8:])) == gen && now.Before(exp) {
			return true
		}
	}
	return false
}

// setSession gives the visitor an unlock of share key: HttpOnly (no script reads it), Secure
// (browsers accept that on http://localhost too), SameSite=Strict, and only for that share's path.
func setSession(w http.ResponseWriter, secret []byte, key string, gen int, now time.Time) {
	exp := now.Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionValue(secret, key, gen, exp),
		Path:     sessionPath(key),
		Expires:  exp,
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}
