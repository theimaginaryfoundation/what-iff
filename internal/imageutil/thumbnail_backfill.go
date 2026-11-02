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
)

var thumbnailBackfills = &thumbnailBackfiller{slots: make(chan struct{}, thumbnailBackfillWorkers)}

type thumbnailBackfiller struct {
	slots    chan struct{}
	inFlight sync.Map // attachment id -> struct{}
	failed   sync.Map // attachment id -> time.Time of the last failure
	// done, when set (tests), is called after each backfill goroutine finishes.
	done func()
}

// BackfillThumbnail writes the thumbnail for an attachment from its full image bytes, in the
// background. It returns at once; nothing is reported to the caller. full must not be modified
// afterwards.
func BackfillThumbnail(store storage.FileStore, logger *zap.Logger, userID, attachmentID uuid.UUID, full []byte) {
	thumbnailBackfills.enqueue(store, logger, userID, attachmentID, func() ([]byte, error) {
		return GenerateThumbnail(full, DefaultThumbnailMaxPx)
	})
}

// CopyThumbnail gives attachment dst the thumbnail attachment src already has, in the
// background. Reference copies share their source's full image but get a new id, and thumbnails
// are stored per id, so without this every thumbnail read of a copy fetches the full image. If
// src has no thumbnail there is nothing to copy; the first read of dst backfills one instead.
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

func (b *thumbnailBackfiller) enqueue(store storage.FileStore, logger *zap.Logger, userID, attachmentID uuid.UUID, thumbnail func() ([]byte, error)) {
	if store == nil || attachmentID == uuid.Nil {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	metrics := telemetry.Global()
	ctx := context.Background()
	if at, ok := b.failed.Load(attachmentID); ok {
		if time.Since(at.(time.Time)) < thumbnailBackfillRetryAfter {
			metrics.RecordThumbnailBackfill(ctx, telemetry.ThumbnailBackfillSkipped)
			return
		}
		b.failed.Delete(attachmentID)
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
			b.failed.Store(attachmentID, time.Now())
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
