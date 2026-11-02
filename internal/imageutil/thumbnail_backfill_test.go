package imageutil

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *memStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = content
	return nil
}

func (s *memStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objects[key], nil
}

func (s *memStore) DeleteFile(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *memStore) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.objects[key]) > 0
}

// useBackfiller swaps in a fresh backfiller whose goroutines the returned WaitGroup tracks.
func useBackfiller(t *testing.T, workers int) *sync.WaitGroup {
	t.Helper()
	var wg sync.WaitGroup
	b := &thumbnailBackfiller{slots: make(chan struct{}, workers), done: wg.Done}
	prev := thumbnailBackfills
	thumbnailBackfills = b
	t.Cleanup(func() { thumbnailBackfills = prev })
	return &wg
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 600, 300))
	for x := 0; x < 600; x++ {
		img.Set(x, x%300, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBackfillThumbnailWritesThumbnail(t *testing.T) {
	rec := telemetrytest.UseGlobal(t)
	wg := useBackfiller(t, 2)
	store := &memStore{objects: map[string][]byte{}}
	userID, attID := uuid.New(), uuid.New()

	wg.Add(1)
	BackfillThumbnail(store, nil, userID, attID, testPNG(t))
	wg.Wait()

	if !store.has(storage.FileKeyForImageThumbnail(userID, attID)) {
		t.Fatal("thumbnail was not written")
	}
	if n := rec.CounterValue(t, telemetry.ThumbnailBackfills.Name, telemetry.AttrOutcome.String(telemetry.ThumbnailBackfillWritten)); n != 1 {
		t.Errorf("written = %d, want 1", n)
	}
}

func TestBackfillThumbnailFailureIsNotRetriedRightAway(t *testing.T) {
	rec := telemetrytest.UseGlobal(t)
	wg := useBackfiller(t, 2)
	store := &memStore{objects: map[string][]byte{}}
	userID, attID := uuid.New(), uuid.New()

	wg.Add(1)
	BackfillThumbnail(store, nil, userID, attID, []byte("not an image"))
	wg.Wait()
	BackfillThumbnail(store, nil, userID, attID, []byte("not an image"))

	if n := rec.CounterValue(t, telemetry.ThumbnailBackfills.Name, telemetry.AttrOutcome.String(telemetry.ThumbnailBackfillFailed)); n != 1 {
		t.Errorf("failed = %d, want 1", n)
	}
	if n := rec.CounterValue(t, telemetry.ThumbnailBackfills.Name, telemetry.AttrOutcome.String(telemetry.ThumbnailBackfillSkipped)); n != 1 {
		t.Errorf("skipped = %d, want 1", n)
	}
}

func TestBackfillThumbnailDropsWhenPoolIsFull(t *testing.T) {
	rec := telemetrytest.UseGlobal(t)
	useBackfiller(t, 1)
	thumbnailBackfills.slots <- struct{}{} // the only worker is busy
	t.Cleanup(func() { <-thumbnailBackfills.slots })
	store := &memStore{objects: map[string][]byte{}}
	userID, attID := uuid.New(), uuid.New()

	BackfillThumbnail(store, nil, userID, attID, testPNG(t))

	if n := rec.CounterValue(t, telemetry.ThumbnailBackfills.Name, telemetry.AttrOutcome.String(telemetry.ThumbnailBackfillDropped)); n != 1 {
		t.Errorf("dropped = %d, want 1", n)
	}
	if _, busy := thumbnailBackfills.inFlight.Load(attID); busy {
		t.Error("a dropped backfill must not stay marked in flight")
	}
}

func TestCopyThumbnail(t *testing.T) {
	wg := useBackfiller(t, 2)
	store := &memStore{objects: map[string][]byte{}}
	userID, src, dst := uuid.New(), uuid.New(), uuid.New()
	store.objects[storage.FileKeyForImageThumbnail(userID, src)] = []byte("jpeg")

	wg.Add(1)
	CopyThumbnail(store, nil, userID, src, dst)
	wg.Wait()

	if got := store.objects[storage.FileKeyForImageThumbnail(userID, dst)]; string(got) != "jpeg" {
		t.Fatalf("copy has thumbnail %q, want the source's", got)
	}
}

// A source without a thumbnail leaves the copy for a read to backfill, rather than marking it
// failed and holding that backfill off.
func TestCopyThumbnailWithoutSourceThumbnailLeavesBackfillOpen(t *testing.T) {
	wg := useBackfiller(t, 2)
	store := &memStore{objects: map[string][]byte{}}
	userID, src, dst := uuid.New(), uuid.New(), uuid.New()

	wg.Add(1)
	CopyThumbnail(store, nil, userID, src, dst)
	wg.Wait()
	wg.Add(1)
	BackfillThumbnail(store, nil, userID, dst, testPNG(t))
	wg.Wait()

	if !store.has(storage.FileKeyForImageThumbnail(userID, dst)) {
		t.Fatal("the read-path backfill should still write the copy's thumbnail")
	}
}

func TestFailedSetIsCapped(t *testing.T) {
	f := &failedSet{max: 2}
	now := time.Now()
	old, a, b, c := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	f.add(old, now.Add(-2*thumbnailBackfillRetryAfter))
	f.add(a, now)
	f.add(b, now) // full: the expired entry is swept to make room
	if f.size() != 2 || !f.recent(a, now) || !f.recent(b, now) {
		t.Fatalf("want a and b remembered after sweeping the expired entry, size=%d", f.size())
	}
	f.add(c, now) // full of live entries: c is not remembered
	if f.size() != 2 || f.recent(c, now) {
		t.Fatalf("a full set must not grow, size=%d", f.size())
	}
	f.add(a, now.Add(time.Minute)) // refreshing a remembered id still works when full
	if !f.recent(a, now.Add(time.Minute)) {
		t.Fatal("a remembered id should be refreshable when the set is full")
	}
}
