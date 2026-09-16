// Package providerkeys resolves which model-provider credential a request
// should use.
//
// Keys belong to accounts. The deployment-level environment variables remain a
// fallback so an existing install keeps working and a single user can still
// configure a file if they prefer, but an account's own key takes precedence.
package providerkeys

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/providerpolicy"
)

// cacheTTL bounds how long a key change takes to reach in-flight work that
// resolved before the change. Writes invalidate the entry immediately, so this
// only covers the case of another process updating the same row.
const cacheTTL = 5 * time.Minute

type entry struct {
	key      string
	cachedAt time.Time
}

// Resolver looks up an account's provider key, with a small cache.
//
// The cache is not an optimisation detail: this is called from inside the HTTP
// round trip of every outgoing provider request, and embeddings alone can fire
// several times per message. Without it each one would add a database query and
// a decryption to the request path.
type Resolver struct {
	ds       *datastore.Datastore
	provider string
	fallback string

	mu    sync.RWMutex
	cache map[uuid.UUID]entry
	now   func() time.Time
}

// NewResolver builds a resolver for one provider. fallback is the
// deployment-level key, used when the account has not set one of their own.
func NewResolver(ds *datastore.Datastore, provider, fallback string) *Resolver {
	return &Resolver{
		ds:       ds,
		provider: provider,
		fallback: fallback,
		cache:    make(map[uuid.UUID]entry),
		now:      time.Now,
	}
}

// Resolve returns the key for the actor on ctx.
//
// With no actor — a background task that never carried one, or an
// unauthenticated path — this falls back to the deployment key rather than
// failing, so behaviour matches what the install did before accounts had keys
// of their own.
func (r *Resolver) Resolve(ctx context.Context) string {
	if r == nil {
		return ""
	}
	// Where the operator supplies the credentials, a stored account key is not
	// consulted at all. The API refuses to store one, but a key can predate a
	// deployment changing its answer, and silently spending it afterwards would
	// bill the wrong party for as long as nobody noticed.
	if !providerpolicy.AccountsSupplyKeys() {
		return r.fallback
	}

	userID, ok := apicontext.UserIDFrom(ctx)
	if !ok || r.ds == nil {
		return r.fallback
	}

	r.mu.RLock()
	e, hit := r.cache[userID]
	r.mu.RUnlock()
	if hit && r.now().Sub(e.cachedAt) < cacheTTL {
		return e.key
	}

	key, err := r.ds.GetUserProviderKey(ctx, userID, r.provider)
	if err != nil && !errors.Is(err, datastore.ErrProviderKeyNotFound) {
		// A lookup failure must not silently downgrade to someone else's
		// credential; serve a stale cached value if there is one, otherwise
		// the deployment key, and let the provider's own error surface.
		if hit {
			return e.key
		}
		return r.fallback
	}
	if key == "" {
		key = r.fallback
	}

	r.mu.Lock()
	r.cache[userID] = entry{key: key, cachedAt: r.now()}
	r.mu.Unlock()
	return key
}

// Configured reports whether the actor on ctx has any usable key, counting the
// deployment fallback. Used to decide which models to offer.
func (r *Resolver) Configured(ctx context.Context) bool {
	return r.Resolve(ctx) != ""
}

// Invalidate drops an account's cached key. Called after a write so the next
// request uses the new value immediately rather than waiting out the TTL.
func (r *Resolver) Invalidate(userID uuid.UUID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.cache, userID)
	r.mu.Unlock()
}
