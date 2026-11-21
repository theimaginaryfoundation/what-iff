package sharelink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
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
	got, ok := visitorAddress(req, "", "")
	require.True(t, ok)
	require.Equal(t, netip.MustParsePrefix("198.51.100.9/32"), got, "a header is not trusted unless the deployment names it")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "")
	require.Equal(t, netip.MustParsePrefix("203.0.113.7/32"), got)

	// IPv6 counts as its /64, in CloudFront's unbracketed form and the connection's bracketed one.
	req.Header.Set("CloudFront-Viewer-Address", "2001:db8:1:2:aaaa:bbbb:cccc:dddd:46532")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "")
	require.Equal(t, netip.MustParsePrefix("2001:db8:1:2::/64"), got)
	req.Header.Del("CloudFront-Viewer-Address")
	req.RemoteAddr = "[2001:db8:1:2::99]:443"
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "")
	require.Equal(t, netip.MustParsePrefix("2001:db8:1:2::/64"), got, "a missing header falls back to the connection")

	req.Header.Set("CloudFront-Viewer-Address", "::ffff:203.0.113.7:443")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "")
	require.Equal(t, netip.MustParsePrefix("203.0.113.7/32"), got, "an IPv4-mapped address is the IPv4 address")
	req.Header.Del("CloudFront-Viewer-Address")
	req.RemoteAddr = "[2001:db8:1:2::99]:443"
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "")

	require.NotEqual(t, addressKey([]byte("a"), got), addressKey([]byte("b"), got), "addresses are stored as keyed hashes")

	// With a proxy secret, the header is trusted only on a request that carries it.
	req.RemoteAddr = "10.0.0.5:443"
	req.Header.Set("CloudFront-Viewer-Address", "203.0.113.7:443")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "s3cret")
	require.Equal(t, netip.MustParsePrefix("10.0.0.5/32"), got, "a visitor who reached the API directly cannot choose their address")
	req.Header.Set(ProxySecretHeader, "s3cret")
	got, _ = visitorAddress(req, "CloudFront-Viewer-Address", "s3cret")
	require.Equal(t, netip.MustParsePrefix("203.0.113.7/32"), got)
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

// memGate keeps attempts and bans in memory, safe for concurrent use as the database is.
type memGate struct {
	mu       sync.Mutex
	attempts []memAttempt
	bans     map[string]memBan
}

type memAttempt struct {
	id     uuid.UUID
	addr   string
	failed bool
	at     time.Time
}

type memBan struct {
	until   time.Time
	strikes int
}

func (m *memGate) RecordShareUnlockAttempt(_ context.Context, addr string, at time.Time, _ time.Duration) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	m.attempts = append(m.attempts, memAttempt{id, addr, true, at})
	return id, nil
}
func (m *memGate) ClearShareUnlockFailure(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.attempts {
		if m.attempts[i].id == id {
			m.attempts[i].failed = false
		}
	}
	return nil
}
func (m *memGate) DiscardShareUnlockAttempt(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.attempts {
		if m.attempts[i].id == id {
			m.attempts = append(m.attempts[:i], m.attempts[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memGate) ShareUnlockAttempts(_ context.Context, addr string, since time.Time, failedOnly bool) (int, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, oldest := 0, time.Time{}
	for _, a := range m.attempts {
		if a.addr == addr && !a.at.Before(since) && (!failedOnly || a.failed) {
			n++
			if oldest.IsZero() || a.at.Before(oldest) {
				oldest = a.at
			}
		}
	}
	return n, oldest, nil
}
func (m *memGate) ShareBan(_ context.Context, addr string) (time.Time, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.bans[addr]
	return b.until, b.strikes, nil
}
func (m *memGate) SetShareBan(_ context.Context, addr string, until time.Time, strikes int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bans == nil {
		m.bans = map[string]memBan{}
	}
	m.bans[addr] = memBan{until, strikes}
	return nil
}

// try is one unlock through the gate: admitted, then concluded right or wrong.
func try(t *testing.T, g *memGate, addr string, right bool, now time.Time) (wait time.Duration, left int) {
	t.Helper()
	ctx := context.Background()
	id, wait, err := admit(ctx, g, addr, now)
	require.NoError(t, err)
	if wait > 0 {
		return wait, 0
	}
	ban, left, err := conclude(ctx, g, addr, id, right, now)
	require.NoError(t, err)
	return ban, left
}

func TestGate_BansAfterFailures_LongerTheSecondTime(t *testing.T) {
	g := &memGate{}
	now := time.Unix(1_800_000_000, 0)
	fail := func() (time.Duration, int) {
		wait, left := try(t, g, "a", false, now)
		now = now.Add(20 * time.Second)
		return wait, left
	}
	for i := 4; i >= 1; i-- {
		ban, left := fail()
		require.Zero(t, ban)
		require.Equal(t, i, left)
	}
	ban, _ := fail()
	require.Equal(t, firstBan, ban)
	wait, _ := try(t, g, "a", true, now)
	require.Greater(t, wait, 50*time.Minute, "banned, right passcode or not")
	wait, _ = try(t, g, "b", true, now)
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
	g := &memGate{}
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < attemptsPerMinute; i++ {
		wait, _ := try(t, g, "a", true, now.Add(time.Duration(i)*time.Second))
		require.Zero(t, wait)
	}
	wait, _ := try(t, g, "a", true, now.Add(20*time.Second))
	require.Equal(t, 40*time.Second, wait, "until the oldest try of the minute ages out")
	wait, _ = try(t, g, "a", true, now.Add(61*time.Second))
	require.Zero(t, wait)
	n, _, _ := g.ShareUnlockAttempts(context.Background(), "a", now, false)
	require.Equal(t, attemptsPerMinute+1, n, "a try refused unchecked is not kept")
}

func TestGate_HoldsUnderConcurrency(t *testing.T) {
	g := &memGate{}
	now := time.Unix(1_800_000_000, 0)
	var wg sync.WaitGroup
	var checked atomic.Int32
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, wait, err := admit(context.Background(), g, "a", now)
			if err != nil || wait > 0 {
				return
			}
			checked.Add(1)
			_, _, _ = conclude(context.Background(), g, "a", id, false, now)
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, int(checked.Load()), failuresBeforeBan, "at most the allowed wrong guesses are checked, however many arrive at once")
	until, _, _ := g.ShareBan(context.Background(), "a")
	require.True(t, until.After(now), "and the address is banned")
}
