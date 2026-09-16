package providermodels

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// cacheTTL is short on purpose. A provider's catalog changes rarely, so a long
// TTL is tempting — but the moment it bites is exactly when a user has just
// added a key or a provider has just shipped something, and "I added my key and
// my model is not there" is a worse failure than an extra request.
const cacheTTL = 5 * time.Minute

type cacheEntry struct {
	models    []Model
	fetchedAt time.Time
}

// Service lists provider catalogs on behalf of accounts, caching per account.
//
// Caching is per account, not per provider: two users on one deployment can
// hold keys with different model access, so one user's list is not a safe
// answer for another.
type Service struct {
	listers map[string]Lister
	keys    KeyResolver
	now     func() time.Time

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	userID   uuid.UUID
	provider string
}

// KeyResolver hands back the key the calling account should use for a
// provider. The account comes from the request context, matching how the rest
// of the key plumbing identifies a caller — the userID passed to List is for
// cache keying only, and the handler takes both from the same context so they
// cannot disagree.
type KeyResolver interface {
	KeyFor(ctx context.Context, provider string) string
}

func NewService(listers map[string]Lister, keys KeyResolver) *Service {
	return &Service{
		listers: listers,
		keys:    keys,
		now:     time.Now,
		cache:   map[cacheKey]cacheEntry{},
	}
}

// Supported reports whether this build can list a provider's catalog at all.
func (s *Service) Supported(provider string) bool {
	_, ok := s.listers[provider]
	return ok
}

// List returns a provider's catalog for one account. refresh bypasses the cache
// for the case where the user is staring at a list they believe is stale.
func (s *Service) List(ctx context.Context, userID uuid.UUID, provider string, refresh bool) ([]Model, error) {
	lister, ok := s.listers[provider]
	if !ok {
		return nil, ErrUnsupportedProvider
	}

	key := cacheKey{userID: userID, provider: provider}
	if !refresh {
		s.mu.Lock()
		entry, hit := s.cache[key]
		s.mu.Unlock()
		if hit && s.now().Sub(entry.fetchedAt) < cacheTTL {
			return entry.models, nil
		}
	}

	apiKey := s.keys.KeyFor(ctx, provider)
	models, err := lister.List(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[key] = cacheEntry{models: models, fetchedAt: s.now()}
	s.mu.Unlock()
	return models, nil
}

// Invalidate drops an account's cached list for a provider. Called when a key
// changes, since a new key can see a different catalog.
func (s *Service) Invalidate(userID uuid.UUID, provider string) {
	s.mu.Lock()
	delete(s.cache, cacheKey{userID: userID, provider: provider})
	s.mu.Unlock()
}
