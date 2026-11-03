package imageutil

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"go.uber.org/zap"
)

// Thumbnail backfill: an image read that wanted a thumbnail but got the full image (an image
// saved before every save path wrote one, or a reference copy made before copies got their own)
// hands the full bytes it already downloaded to BackfillThumbnail, which writes the thumbnail out
// of band so the next read is one small fetch. Best effort throughout: a full pool drops the
// work, since the next read of that image offers it again.

const (
	// thumbnailBackfillWorkers bounds concurrent backfills per process. Each holds a decoded
	// full-size image in memory while it scales.
	thumbnailBackfillWorkers = 4
	// thumbnailBackfillTimeout bounds one backfill's encode and upload.
	thumbnailBackfillTimeout = 30 * time.Second
	// thumbnailBackfillRetryAfter keeps an image that failed to thumbnail (undecodable, or a
	// storage error) from being retried on every read.
	thumbnailBackfillRetryAfter = time.Hour
	// thumbnailBackfillMaxFailed caps the failures remembered for thumbnailBackfillRetryAfter
	// (about 100 bytes each). When a storage outage fails every backfill, the set fills up; past
	// the cap, expired entries are swept and, if it is still full, a new failure is not
	// remembered: that image is retried on its next read, which the worker pool still bounds.
	thumbnailBackfillMaxFailed = 10_000
)

// thumbnailBackfills is the process's backfill pool. Only this package's tests replace it
// (useBackfiller), so they must not run in parallel with each other.
var thumbnailBackfills = &thumbnailBackfiller{slots: make(chan struct{}, thumbnailBackfillWorkers)}

type thumbnailBackfiller struct {
	slots chan struct{}
	// inFlight holds the ids with a backfill running, so at most len(slots) of them.
	inFlight sync.Map // attachment id -> struct{}
	failed   failedSet
	// done, when set (tests), is called after each backfill goroutine finishes.
	done func()
}

// failedSet remembers recent backfill failures, at most thumbnailBackfillMaxFailed of them.
type failedSet struct {
	mu  sync.Mutex
	at  map[uuid.UUID]time.Time
	max int // 0 means thumbnailBackfillMaxFailed
}

// recent reports whether id failed within thumbnailBackfillRetryAfter, forgetting an older failure.
func (f *failedSet) recent(id uuid.UUID, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	at, ok := f.at[id]
	if !ok {
		return false
	}
	if now.Sub(at) < thumbnailBackfillRetryAfter {
		return true
	}
	delete(f.at, id)
	return false
}

func (f *failedSet) add(id uuid.UUID, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	limit := f.max
	if limit == 0 {
		limit = thumbnailBackfillMaxFailed
	}
	if f.at == nil {
		f.at = make(map[uuid.UUID]time.Time)
	}
	if _, ok := f.at[id]; !ok && len(f.at) >= limit {
		for k, at := range f.at {
			if now.Sub(at) >= thumbnailBackfillRetryAfter {
				delete(f.at, k)
			}
		}
		if len(f.at) >= limit {
			return
		}
	}
	f.at[id] = now
}

func (f *failedSet) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.at)
}

// BackfillThumbnail writes the thumbnail for an attachment from its full image bytes, in the
// background. It returns at once and reports nothing to the caller by design: every outcome is
// counted on telemetry.ThumbnailBackfills, and failures are logged at debug. A nil store makes it a
// no-op and a nil logger is allowed. full must not be modified afterwards.
func BackfillThumbnail(store storage.FileStore, logger *zap.Logger, userID, attachmentID uuid.UUID, full []byte) {
	thumbnailBackfills.enqueue(store, logger, userID, attachmentID, func() ([]byte, error) {
		return GenerateThumbnail(full, DefaultThumbnailMaxPx)
	})
}

// CopyThumbnail gives attachment dst the thumbnail attachment src already has, in the
// background. Reference copies share their source's full image but get a new id, and thumbnails
// are stored per id, so without this every thumbnail read of a copy fetches the full image. If
// src has no thumbnail there is nothing to copy; the first read of dst backfills one instead.
// Like BackfillThumbnail it returns at once, reports nothing, and accepts a nil store or logger.
func CopyThumbnail(store storage.FileStore, logger *zap.Logger, userID, src, dst uuid.UUID) {
	if src == dst {
		return
	}
	thumbnailBackfills.enqueue(store, logger, userID, dst, func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), thumbnailBackfillTimeout)
		defer cancel()
		return store.DownloadFile(ctx, storage.FileKeyForImageThumbnail(userID, src))
	})
}

// enqueue runs thumbnail on a free worker and uploads what it returns as attachmentID's thumbnail.
// thumbnail is called at most once, on the worker goroutine, and must not depend on buffers the
// caller may change. It runs without a context: GenerateThumbnail is a CPU-bound decode and scale
// of bytes already in memory (it cannot block on I/O, and image decoding cannot be cancelled), and
// CopyThumbnail's download sets its own timeout. Returning no bytes and no error means there was
// nothing to write, which is not recorded as a failure.
func (b *thumbnailBackfiller) enqueue(store storage.FileStore, logger *zap.Logger, userID, attachmentID uuid.UUID, thumbnail func() ([]byte, error)) {
	if store == nil || attachmentID == uuid.Nil {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	metrics := telemetry.Global()
	ctx := context.Background()
	if b.failed.recent(attachmentID, time.Now()) {
		metrics.RecordThumbnailBackfill(ctx, telemetry.ThumbnailBackfillSkipped)
		return
	}
	if _, busy := b.inFlight.LoadOrStore(attachmentID, struct{}{}); busy {
		return
	}
	select {
	case b.slots <- struct{}{}:
	default:
		b.inFlight.Delete(attachmentID)
		metrics.RecordThumbnailBackfill(ctx, telemetry.ThumbnailBackfillDropped)
		return
	}
	go func() {
		defer func() {
			<-b.slots
			b.inFlight.Delete(attachmentID)
			if b.done != nil {
				b.done()
			}
		}()
		fail := func(msg string, err error) {
			b.failed.add(attachmentID, time.Now())
			metrics.RecordThumbnailBackfill(ctx, telemetry.ThumbnailBackfillFailed)
			logger.Debug("thumbnail backfill: "+msg, zap.String("attachment_id", attachmentID.String()), zap.Error(err))
		}
		thumb, err := thumbnail()
		if err != nil {
			fail("no thumbnail to write", err)
			return
		}
		if len(thumb) == 0 {
			return // nothing to copy; not a failure, so a read can still backfill it
		}
		uploadCtx, cancel := context.WithTimeout(ctx, thumbnailBackfillTimeout)
		defer cancel()
		if err := store.UploadFile(uploadCtx, storage.FileKeyForImageThumbnail(userID, attachmentID), thumb, "image/jpeg"); err != nil {
			fail("upload failed", err)
			return
		}
		metrics.RecordThumbnailBackfill(ctx, telemetry.ThumbnailBackfillWritten)
	}()
}
