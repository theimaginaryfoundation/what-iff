package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeScratchpadStore keeps one personality and applies the datastore's conditional-write rule:
// a write succeeds only at the current revision, and bumps it.
type fakeScratchpadStore struct {
	mu         sync.Mutex
	p          models.Personality
	writes     []int // expected revision of every conditional write attempted
	writeErr   error
	beforeSave func() // runs before each conditional write (outside the lock), to force interleaves
}

func (f *fakeScratchpadStore) GetPersonality(_ context.Context, _, _ uuid.UUID) (*models.Personality, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := f.p
	return &cp, nil
}

func (f *fakeScratchpadStore) UpdatePersonalityScratchpadIfRevision(_ context.Context, _, _ uuid.UUID, content string, expectedRevision int) (*models.Personality, error) {
	if f.beforeSave != nil {
		f.beforeSave()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, expectedRevision)
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	if f.p.ScratchpadRevision != expectedRevision {
		return nil, datastore.ErrScratchpadConflict
	}
	f.p.Scratchpad = content
	f.p.ScratchpadRevision++
	cp := f.p
	return &cp, nil
}

// appendingGenerator returns a generator that appends note to whatever scratchpad it builds on:
// the turn's starting scratchpad first, then the latest one on a conflict retry.
func appendingGenerator(start, note string, calls *[]*models.Personality) scratchpadGenerateFunc {
	return func(_ context.Context, latest *models.Personality) (ScratchpadUpdate, error) {
		*calls = append(*calls, latest)
		base := start
		if latest != nil {
			base = latest.Scratchpad
		}
		return ScratchpadUpdate{Content: base + note}, nil
	}
}

func TestCommitScratchpadUpdate_WritesAtTheTurnsRevision(t *testing.T) {
	t.Parallel()
	store := &fakeScratchpadStore{p: models.Personality{Scratchpad: "base", ScratchpadRevision: 3}}
	var calls []*models.Personality

	got, err := commitScratchpadUpdate(context.Background(), store, zap.NewNop(), uuid.New(), uuid.New(), 3,
		appendingGenerator("base", "+A", &calls))
	require.NoError(t, err)
	require.Equal(t, "base+A", got.Content)
	require.Equal(t, []*models.Personality{nil}, calls, "generated once, from the turn's scratchpad")
	require.Equal(t, []int{3}, store.writes)
	require.Equal(t, 4, store.p.ScratchpadRevision)
}

func TestCommitScratchpadUpdate_StaleRevisionRegeneratesAgainstLatest(t *testing.T) {
	t.Parallel()
	// The turn read revision 0 ("base"); another chat's checkpoint has since written revision 1.
	store := &fakeScratchpadStore{p: models.Personality{Scratchpad: "base+B", ScratchpadRevision: 1}}
	var calls []*models.Personality

	got, err := commitScratchpadUpdate(context.Background(), store, zap.NewNop(), uuid.New(), uuid.New(), 0,
		appendingGenerator("base", "+A", &calls))
	require.NoError(t, err)
	require.Equal(t, "base+B+A", got.Content, "the retry builds on the newer scratchpad")
	require.Len(t, calls, 2)
	require.Nil(t, calls[0])
	require.Equal(t, "base+B", calls[1].Scratchpad)
	require.Equal(t, []int{0, 1}, store.writes, "first write conflicts at the stale revision, retry writes at the latest")
	require.Equal(t, "base+B+A", store.p.Scratchpad)
	require.Equal(t, 2, store.p.ScratchpadRevision)
}

func TestCommitScratchpadUpdate_SecondConflictSkipsInsteadOfOverwriting(t *testing.T) {
	t.Parallel()
	store := &fakeScratchpadStore{p: models.Personality{Scratchpad: "base+B", ScratchpadRevision: 1}}
	// Every write is preceded by yet another concurrent write, so the retry conflicts too.
	store.beforeSave = func() {
		store.mu.Lock()
		store.p.Scratchpad += "+C"
		store.p.ScratchpadRevision++
		store.mu.Unlock()
	}
	var calls []*models.Personality

	_, err := commitScratchpadUpdate(context.Background(), store, zap.NewNop(), uuid.New(), uuid.New(), 0,
		appendingGenerator("base", "+A", &calls))
	require.ErrorIs(t, err, datastore.ErrScratchpadConflict)
	require.Len(t, calls, 2, "regenerated exactly once")
	require.Equal(t, "base+B+C+C", store.p.Scratchpad, "the concurrent writes are kept; this update never lands")
}

func TestCommitScratchpadUpdate_ErrorsAreReturnedWithoutRetry(t *testing.T) {
	t.Parallel()
	genErr := errors.New("provider down")
	store := &fakeScratchpadStore{}
	_, err := commitScratchpadUpdate(context.Background(), store, zap.NewNop(), uuid.New(), uuid.New(), 0,
		func(context.Context, *models.Personality) (ScratchpadUpdate, error) {
			return ScratchpadUpdate{}, genErr
		})
	require.ErrorIs(t, err, genErr)
	require.Empty(t, store.writes)

	writeErr := errors.New("db down")
	store = &fakeScratchpadStore{writeErr: writeErr}
	var calls []*models.Personality
	_, err = commitScratchpadUpdate(context.Background(), store, zap.NewNop(), uuid.New(), uuid.New(), 0,
		appendingGenerator("", "x", &calls))
	require.ErrorIs(t, err, writeErr)
	require.NotErrorIs(t, err, datastore.ErrScratchpadConflict)
	require.Len(t, calls, 1, "only a conflict triggers a regeneration")
}

// TestCommitScratchpadUpdate_ForcedInterleaveKeepsBothUpdates is the issue's acceptance case: two
// chats sharing a personality checkpoint at once, both starting from the same scratchpad, and both
// generate their update before either writes. Neither update is lost.
func TestCommitScratchpadUpdate_ForcedInterleaveKeepsBothUpdates(t *testing.T) {
	t.Parallel()
	store := &fakeScratchpadStore{p: models.Personality{Scratchpad: "base", ScratchpadRevision: 0}}
	userID, personalityID := uuid.New(), uuid.New()

	// Both first attempts finish generating before either is written.
	var generated sync.WaitGroup
	generated.Add(2)
	interleaved := func(note string) scratchpadGenerateFunc {
		first := true
		return func(_ context.Context, latest *models.Personality) (ScratchpadUpdate, error) {
			base := "base"
			if latest != nil {
				base = latest.Scratchpad
			}
			if first {
				first = false
				generated.Done()
				generated.Wait()
			}
			return ScratchpadUpdate{Content: base + note}, nil
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, note := range []string{"+A", "+B"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = commitScratchpadUpdate(context.Background(), store, zap.NewNop(), userID, personalityID, 0, interleaved(note))
		}()
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	require.Equal(t, 2, store.p.ScratchpadRevision)
	require.Contains(t, []string{"base+A+B", "base+B+A"}, store.p.Scratchpad, "both checkpoints' updates survive")
}

func TestAppendScratchpadUpdateTurn(t *testing.T) {
	t.Parallel()
	mc := &provider.ModelContext{}
	appendScratchpadUpdateTurn(mc, nil, "update it")
	appendScratchpadUpdateTurn(mc, &models.Personality{Scratchpad: "newer notes"}, "update it")
	require.Len(t, mc.Segments, 2)
	require.Equal(t, "update it", mc.Segments[0].Content)
	require.Equal(t, provider.SegmentKindUserMessage, mc.Segments[1].Kind)
	require.Contains(t, mc.Segments[1].Content, "LATEST SCRATCHPAD:\nnewer notes")
	require.Contains(t, mc.Segments[1].Content, "update it")
}
