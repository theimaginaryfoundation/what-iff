package slackrelay

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// nameCache remembers workspace members' display names (users.info) per app, so a busy channel
// does not ask Slack for the same person on every message. A failed lookup is remembered too,
// briefly, as "".
type nameCache struct {
	ttl time.Duration

	mu      sync.Mutex
	entries map[nameKey]nameEntry
}

type nameKey struct {
	app  uuid.UUID
	user string
}

type nameEntry struct {
	name string
	at   time.Time
}

// maxCachedNames bounds the cache; past it, the cache starts over.
const maxCachedNames = 5000

func (c *nameCache) get(ctx context.Context, slack Slack, app models.SlackAppCredentials, userID string) nameEntry {
	if userID == "" {
		return nameEntry{}
	}
	key := nameKey{app: app.ID, user: userID}
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok && time.Since(e.at) < c.ttl {
		return e
	}
	fresh := nameEntry{name: e.name, at: time.Now()} // on a failed lookup, keep what we had
	if u, err := slack.UserInfo(ctx, app.BotToken, userID); err == nil && u != nil {
		fresh.name = u.DisplayName()
	}
	e = fresh
	c.mu.Lock()
	if c.entries == nil || len(c.entries) >= maxCachedNames {
		c.entries = map[nameKey]nameEntry{}
	}
	c.entries[key] = e
	c.mu.Unlock()
	return e
}
