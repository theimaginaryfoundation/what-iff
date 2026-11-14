package sharelink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestProjectPlace_StaysInsideTheRoot(t *testing.T) {
	ok := []struct{ path, folder, name string }{
		{"index.html", "site", "index.html"},
		{"CSS/App.css", "site/css", "App.css"},
		{"a/b/c/logo.PNG", "site/a/b/c", "logo.PNG"},
		{"..hidden/x.js", "site/..hidden", "x.js"}, // a name that starts with dots is a name
	}
	for _, c := range ok {
		folder, name, good := projectPlace("site", c.path)
		require.True(t, good, c.path)
		require.Equal(t, c.folder, folder, c.path)
		require.Equal(t, c.name, name, c.path)
	}
	refused := []string{
		"", "/etc/passwd", "../other/secret.txt", "a/../../other.txt", "a/./b.txt", ".", "..",
		"a//b.txt", "a/", `a\..\b.txt`, "a\x00b.txt", "a/\nb.txt",
	}
	for _, p := range refused {
		_, _, good := projectPlace("site", p)
		require.False(t, good, "%q must be refused", p)
	}
	_, _, good := projectPlace("", "index.html")
	require.False(t, good, "the top level is no project")
}

func TestProjectPath(t *testing.T) {
	p, ok := projectPath("site", models.FileAttachment{Folder: "site/css", Name: "app.css"})
	require.True(t, ok)
	require.Equal(t, "css/app.css", p)
	_, ok = projectPath("site", models.FileAttachment{Folder: "sites", Name: "x"})
	require.False(t, ok, "a folder that only starts with the root's name is outside it")
}

func TestVisitorAddress(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.9:5000"
	req.Header.Set("CloudFront-Viewer-Address", "203.0.113.7:443")
	got, ok := visitorAddress(req, "")
	require.True(t, ok)
	require.Equal(t, netip.MustParsePrefix("198.51.100.9/32"), got, "a header is not trusted unless the deployment names it")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address")
	require.Equal(t, netip.MustParsePrefix("203.0.113.7/32"), got)

	// IPv6 counts as its /64, in CloudFront's unbracketed form and the connection's bracketed one.
	req.Header.Set("CloudFront-Viewer-Address", "2001:db8:1:2:aaaa:bbbb:cccc:dddd:46532")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address")
	require.Equal(t, netip.MustParsePrefix("2001:db8:1:2::/64"), got)
	req.Header.Del("CloudFront-Viewer-Address")
	req.RemoteAddr = "[2001:db8:1:2::99]:443"
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address")
	require.Equal(t, netip.MustParsePrefix("2001:db8:1:2::/64"), got, "a missing header falls back to the connection")

	require.NotEqual(t, addressKey([]byte("a"), got), addressKey([]byte("b"), got), "addresses are stored as keyed hashes")
}

func TestSession(t *testing.T) {
	secret := []byte("s")
	now := time.Unix(1_800_000_000, 0)
	rec := httptest.NewRecorder()
	setSession(rec, secret, "k1", 3, now)
	cookie := rec.Result().Cookies()[0]
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, "/api/shared/k1", cookie.Path)

	with := func(c *http.Cookie) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)
		return r
	}
	require.True(t, unlocked(with(cookie), secret, "k1", 3, now))
	require.False(t, unlocked(with(cookie), secret, "k1", 4, now), "a new passcode signs the visitor out")
	require.False(t, unlocked(with(cookie), secret, "k2", 3, now), "one share's unlock opens no other")
	require.False(t, unlocked(with(cookie), []byte("other"), "k1", 3, now))
	require.False(t, unlocked(with(cookie), secret, "k1", 3, now.Add(sessionTTL+time.Second)), "it expires")
	forged := *cookie
	forged.Value = cookie.Value[:len(cookie.Value)-2] + "AA"
	require.False(t, unlocked(with(&forged), secret, "k1", 3, now))
}

// memGate keeps attempts and bans in memory.
type memGate struct {
	attempts []struct {
		addr   string
		failed bool
		at     time.Time
	}
	bans map[string]struct {
		until   time.Time
		strikes int
	}
}

func (m *memGate) RecordShareUnlockAttempt(_ context.Context, addr string, failed bool, at time.Time, keep time.Duration) error {
	m.attempts = append(m.attempts, struct {
		addr   string
		failed bool
		at     time.Time
	}{addr, failed, at})
	return nil
}
func (m *memGate) CountShareUnlockAttempts(_ context.Context, addr string, since time.Time, failedOnly bool) (int, error) {
	n := 0
	for _, a := range m.attempts {
		if a.addr == addr && !a.at.Before(since) && (!failedOnly || a.failed) {
			n++
		}
	}
	return n, nil
}
func (m *memGate) ShareBan(_ context.Context, addr string) (time.Time, int, error) {
	b := m.bans[addr]
	return b.until, b.strikes, nil
}
func (m *memGate) SetShareBan(_ context.Context, addr string, until time.Time, strikes int) error {
	if m.bans == nil {
		m.bans = map[string]struct {
			until   time.Time
			strikes int
		}{}
	}
	m.bans[addr] = struct {
		until   time.Time
		strikes int
	}{until, strikes}
	return nil
}

func TestGate_BansAfterFailures_LongerTheSecondTime(t *testing.T) {
	ctx := context.Background()
	g := &memGate{}
	now := time.Unix(1_800_000_000, 0)
	fail := func() (time.Duration, int) {
		ban, left, err := recordUnlock(ctx, g, "a", true, now)
		require.NoError(t, err)
		now = now.Add(20 * time.Second)
		return ban, left
	}
	for i := 4; i >= 1; i-- {
		ban, left := fail()
		require.Zero(t, ban)
		require.Equal(t, i, left)
	}
	ban, _ := fail()
	require.Equal(t, firstBan, ban)
	wait, err := waitBeforeUnlock(ctx, g, "a", now)
	require.NoError(t, err)
	require.Greater(t, wait, 50*time.Minute, "banned, right passcode or not")
	wait, _ = waitBeforeUnlock(ctx, g, "b", now)
	require.Zero(t, wait, "another address is not")

	// After the ban, the failures it was for do not count again; five more ban it for a day.
	now = now.Add(firstBan)
	ban, left := fail()
	require.Zero(t, ban)
	require.Equal(t, 4, left)
	for i := 0; i < 3; i++ {
		fail()
	}
	ban, _ = fail()
	require.Equal(t, repeatBan, ban)
}

func TestGate_LimitsTriesPerMinute(t *testing.T) {
	ctx := context.Background()
	g := &memGate{}
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < attemptsPerMinute; i++ {
		_, _, err := recordUnlock(ctx, g, "a", false, now)
		require.NoError(t, err)
	}
	wait, err := waitBeforeUnlock(ctx, g, "a", now)
	require.NoError(t, err)
	require.Equal(t, time.Minute, wait)
	wait, _ = waitBeforeUnlock(ctx, g, "a", now.Add(61*time.Second))
	require.Zero(t, wait)
}
