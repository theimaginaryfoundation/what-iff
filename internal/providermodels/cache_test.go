package providermodels

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type countingLister struct {
	calls  int
	models []Model
	err    error
}

func (c *countingLister) List(context.Context, string) ([]Model, error) {
	c.calls++
	return c.models, c.err
}

func newTestService(l Lister, now func() time.Time) *Service {
	s := NewService(map[string]Lister{"openai": l}, staticKeys{key: "sk-test"})
	s.now = now
	return s
}

func TestService_CachesWithinTTL(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	lister := &countingLister{models: []Model{{ID: "gpt-5.6-luna"}}}
	svc := newTestService(lister, func() time.Time { return clock })
	user := uuid.New()

	for i := 0; i < 3; i++ {
		_, err := svc.List(context.Background(), user, "openai", false)
		require.NoError(t, err)
	}
	require.Equal(t, 1, lister.calls, "repeat reads inside the TTL should not re-query the provider")

	clock = clock.Add(cacheTTL + time.Second)
	_, err := svc.List(context.Background(), user, "openai", false)
	require.NoError(t, err)
	require.Equal(t, 2, lister.calls, "past the TTL the list should be refetched")
}

// Two accounts on one deployment can hold keys with different model access, so
// one user's list must never be served to another.
func TestService_CacheIsPerAccount(t *testing.T) {
	t.Parallel()

	lister := &countingLister{models: []Model{{ID: "gpt-5.6-luna"}}}
	svc := newTestService(lister, time.Now)

	_, err := svc.List(context.Background(), uuid.New(), "openai", false)
	require.NoError(t, err)
	_, err = svc.List(context.Background(), uuid.New(), "openai", false)
	require.NoError(t, err)
	require.Equal(t, 2, lister.calls)
}

func TestService_RefreshAndInvalidateBypassCache(t *testing.T) {
	t.Parallel()

	lister := &countingLister{models: []Model{{ID: "gpt-5.6-luna"}}}
	svc := newTestService(lister, time.Now)
	user := uuid.New()

	_, _ = svc.List(context.Background(), user, "openai", false)
	_, _ = svc.List(context.Background(), user, "openai", true)
	require.Equal(t, 2, lister.calls, "refresh must bypass the cache")

	svc.Invalidate(user, "openai")
	_, _ = svc.List(context.Background(), user, "openai", false)
	require.Equal(t, 3, lister.calls, "a key change invalidates what that key could see")
}

// A failed fetch must not be cached, or one blip hides the catalog for the
// whole TTL.
func TestService_DoesNotCacheFailures(t *testing.T) {
	t.Parallel()

	lister := &countingLister{err: errors.New("network down")}
	svc := newTestService(lister, time.Now)
	user := uuid.New()

	_, err := svc.List(context.Background(), user, "openai", false)
	require.Error(t, err)
	_, err = svc.List(context.Background(), user, "openai", false)
	require.Error(t, err)
	require.Equal(t, 2, lister.calls)
}

func TestService_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	svc := newTestService(&countingLister{}, time.Now)
	require.True(t, svc.Supported("openai"))
	require.False(t, svc.Supported("anthropic"))

	_, err := svc.List(context.Background(), uuid.New(), "anthropic", false)
	require.ErrorIs(t, err, ErrUnsupportedProvider)
}
