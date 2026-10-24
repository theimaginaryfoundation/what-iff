package buzzrelay

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/buzzrelay/nostr"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// nameCache remembers authors' display names (kind 0) for a while, so a busy channel does not
// look every author up on every mention.
type nameCache struct {
	ttl time.Duration

	mu    sync.Mutex
	names map[string]cachedName
}

type cachedName struct {
	name string
	// bot: the profile marks an automated account.
	bot bool
	at  time.Time
}

// name is pubkey's display name in the community, or a short npub when it has no profile.
func (c *nameCache) name(ctx context.Context, relay Relay, ep Endpoint, signer Signer, pubkey string) string {
	return c.profile(ctx, relay, ep, signer, pubkey).name
}

// profile is what pubkey's profile (kind 0) says, looked up at most once per ttl.
func (c *nameCache) profile(ctx context.Context, relay Relay, ep Endpoint, signer Signer, pubkey string) cachedName {
	key := ep.Host() + "|" + pubkey
	c.mu.Lock()
	if n, ok := c.names[key]; ok && time.Since(n.at) < c.ttl {
		c.mu.Unlock()
		return n
	}
	c.mu.Unlock()

	var p Profile
	events, err := relay.Query(ctx, ep, signer, Filter{"kinds": []int{KindProfile}, "authors": []string{pubkey}, "limit": 1})
	if err == nil && len(events) > 0 && events[0].Verify() == nil && events[0].PubKey == pubkey {
		p = ParseProfile(events[0])
	}
	n := cachedName{name: p.Label(), bot: p.Bot, at: time.Now()}
	if strings.TrimSpace(n.name) == "" {
		n.name = ShortNPub(pubkey)
	}
	c.mu.Lock()
	if c.names == nil {
		c.names = map[string]cachedName{}
	}
	if len(c.names) > 5000 {
		c.names = map[string]cachedName{}
	}
	c.names[key] = n
	c.mu.Unlock()
	return n
}

// channelCheck remembers when each binding's channel name was last read from the relay, so a
// busy channel reads it at most once per ttl.
type channelCheck struct {
	ttl time.Duration

	mu      sync.Mutex
	checked map[uuid.UUID]time.Time
}

// due reports whether the binding's channel name should be read again, and records it.
func (c *channelCheck) due(bindingID uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.checked[bindingID]; ok && time.Since(at) < c.ttl {
		return false
	}
	if c.checked == nil || len(c.checked) > 5000 {
		c.checked = map[uuid.UUID]time.Time{}
	}
	c.checked[bindingID] = time.Now()
	return true
}

// channelName is the bound channel's current name from its relay-signed metadata (kind
// 39000), read when a mention is answered and at most once per ttl; "" when not due or unknown.
// A channel renamed in Buzz is then renamed on the binding, as the Discord relay does.
func (s *Service) channelName(ctx context.Context, t models.BuzzBindingTarget, ep Endpoint, signer Signer) string {
	if !s.channels.due(t.Binding.ID) {
		return ""
	}
	events, err := s.Relay.Query(ctx, ep, signer, Filter{"kinds": []int{KindGroupMetadata}, "#d": []string{t.Binding.ChannelID}, "limit": 1})
	if err != nil {
		return ""
	}
	var newest *nostr.Event
	for i, ev := range events {
		if ev.Verify() != nil || ev.Tags.Value("d") != t.Binding.ChannelID {
			continue
		}
		if t.Community.RelayPubKey != "" && ev.PubKey != t.Community.RelayPubKey {
			continue // channel state is relay-signed
		}
		if newest == nil || ev.CreatedAt > newest.CreatedAt {
			newest = &events[i]
		}
	}
	if newest == nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(newest.Tags.Value("name")), "#")
}
