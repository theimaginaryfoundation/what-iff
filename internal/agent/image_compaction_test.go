package agent

import (
	"bytes"
	"context"
	"database/sql/driver"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

// noisyPNG is a photo-like PNG (gradients plus noise) as an image vendor would write it.
func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := func(v int) uint8 { return uint8(min(255, max(0, v+rng.Intn(9)-4))) }
			img.SetNRGBA(x, y, color.NRGBA{R: n(x * 255 / w), G: n(y * 255 / h), B: n(100), A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// An expression cell (a generated image) is stored as a compact PNG, with its thumbnail.
func TestUploadExpressionCellAttachment_StoresACompactPNG(t *testing.T) {
	t.Parallel()
	ds, mock, cleanup := newTestDatastore(t)
	defer cleanup()

	uid, pid, attID := uuid.New(), uuid.New(), uuid.New()
	any := sqlmock.AnyArg()
	expectCreatePersonalityAttachmentWithInsert(mock, uid, pid, attID,
		[]driver.Value{"expression-calm.png", "image/png", "expressions/aster", false, any, "user", any, any, any, any})
	mock.ExpectExec("UPDATE `file_attachments`").WillReturnResult(sqlmock.NewResult(0, 1))

	store := newRecordingFileStore()
	a := &Agent{ds: ds, logger: zap.NewNop(), fileStore: store}
	raw := noisyPNG(t, 256, 192)
	_, err := a.uploadExpressionCellAttachment(context.Background(), uid, pid, "expressions/aster", "calm", raw)
	require.NoError(t, err)

	stored := store.objects[storage.FileKeyForPersonality(uid, pid, attID, "expression-calm.png")]
	require.NotEmpty(t, stored)
	require.Less(t, len(stored), len(raw))
	img, err := png.Decode(bytes.NewReader(stored))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 256, 192), img.Bounds())
	require.Contains(t, store.objects, storage.FileKeyForImageThumbnail(uid, attID))
	require.NoError(t, mock.ExpectationsWereMet())
}
