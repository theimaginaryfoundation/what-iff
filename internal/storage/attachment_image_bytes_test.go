package storage

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

// countingStore is an in-memory FileStore that records every download.
type countingStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	downloads []string
}

func (s *countingStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = content
	return nil
}

func (s *countingStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloads = append(s.downloads, key)
	return s.objects[key], nil
}

func (s *countingStore) DeleteFile(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func TestResolveAttachmentImage(t *testing.T) {
	userID, attID, srcID, chatID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	canonical := FileKeyForImage(userID, attID, "a.png")
	thumb := FileKeyForImageThumbnail(userID, attID)
	shared := FileKeyForImage(userID, srcID, "a.png") // a reference copy's key: its source's object
	chat := FileKeyForChat(userID, chatID, attID, "a.png")

	tests := []struct {
		name      string
		s3Key     string
		objects   []string
		wantThumb bool
		source    string
		downloads int
	}{
		{"thumbnail hit is one fetch", canonical, []string{thumb, canonical}, true, telemetry.ImageSourceThumbnail, 1},
		{"no thumbnail falls back to the row's key", canonical, []string{canonical}, true, telemetry.ImageSourceS3Key, 2},
		{"reference copy serves the shared object", shared, []string{shared}, true, telemetry.ImageSourceS3Key, 2},
		{"canonical key that is also the row's key is fetched once", canonical, []string{chat}, true, telemetry.ImageSourceChat, 3},
		{"canonical after a distinct row key", shared, []string{canonical}, true, telemetry.ImageSourceCanonical, 3},
		{"full image is one fetch", canonical, []string{canonical}, false, telemetry.ImageSourceCanonical, 1},
		{"full image skips a row key equal to canonical", canonical, []string{chat}, false, telemetry.ImageSourceChat, 2},
		{"everything misses", canonical, nil, true, telemetry.ImageSourceNone, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := telemetrytest.UseGlobal(t)
			store := &countingStore{objects: map[string][]byte{}}
			for _, k := range tt.objects {
				store.objects[k] = []byte("bytes:" + k)
			}
			att := &models.FileAttachment{ID: attID, Name: "a.png", FileType: "image/png", S3Key: tt.s3Key, ChatID: &chatID}

			data, _, source := ResolveAttachmentImage(context.Background(), nil, store, userID, att, tt.wantThumb)
			if source != tt.source {
				t.Errorf("source = %q, want %q", source, tt.source)
			}
			if (len(data) > 0) != (tt.source != telemetry.ImageSourceNone) {
				t.Errorf("got %d bytes for source %q", len(data), source)
			}
			if len(store.downloads) != tt.downloads {
				t.Errorf("downloads = %v, want %d", store.downloads, tt.downloads)
			}
			variant := telemetry.ImageVariantFull
			if tt.wantThumb {
				variant = telemetry.ImageVariantThumbnail
			}
			if n := rec.CounterValue(t, telemetry.ImageResolves.Name, telemetry.AttrVariant.String(variant), telemetry.AttrSource.String(tt.source)); n != 1 {
				t.Errorf("image_resolve{variant=%s,source=%s} = %d, want 1", variant, tt.source, n)
			}
		})
	}
}
