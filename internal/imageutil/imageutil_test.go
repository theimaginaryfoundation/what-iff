package imageutil

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
)

func TestNormalizeForUploadScalesOpaqueToPNG(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{R: 40, G: 90, B: 160, A: 255}), image.Point{}, draw.Src)
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, src))

	var output bytes.Buffer
	fileName, contentType, err := NormalizeForUpload(&input, &output, "original.png", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "original.png", fileName)
	require.Equal(t, "image/png", contentType)

	normalized, format, err := image.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, "png", format)
	require.Equal(t, 2048, normalized.Bounds().Dx())
	require.Equal(t, 1024, normalized.Bounds().Dy())
}

// Transparency survives normalization.
func TestNormalizeForUploadKeepsTransparentAsPNG(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	src.SetNRGBA(3, 3, color.NRGBA{R: 255, A: 255}) // the rest stays fully transparent
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, src))

	var output bytes.Buffer
	fileName, contentType, err := NormalizeForUpload(&input, &output, "sticker.png", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "sticker.png", fileName)
	require.Equal(t, "image/png", contentType)

	normalized, format, err := image.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, "png", format)
	_, _, _, a := normalized.At(0, 0).RGBA()
	require.Zero(t, a, "transparent pixels stay transparent")
}

// WebP uploads are accepted and re-encoded like every other image, so nothing
// downstream (thumbnails, vendors, gallery) ever sees a WebP upload.
func TestNormalizeForUploadConvertsWebP(t *testing.T) {
	t.Parallel()

	webp, err := os.ReadFile("testdata/lossy.webp")
	require.NoError(t, err)
	src, format, err := image.Decode(bytes.NewReader(webp))
	require.NoError(t, err, "the WebP decoder is registered")
	require.Equal(t, "webp", format)

	var output bytes.Buffer
	fileName, contentType, err := NormalizeForUpload(bytes.NewReader(webp), &output, "sunset.webp", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "sunset.png", fileName)
	require.Equal(t, "image/png", contentType)

	normalized, format, err := image.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, "png", format)
	require.Equal(t, src.Bounds().Size(), normalized.Bounds().Size(), "small images keep their size")
}

// A JPEG stays a JPEG: storing an already lossy photo as PNG would only make it
// several times larger.
func TestNormalizeForUploadKeepsJPEGAsJPEG(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	var input bytes.Buffer
	require.NoError(t, jpeg.Encode(&input, src, &jpeg.Options{Quality: 90}))
	inputLen := input.Len()

	var output bytes.Buffer
	fileName, contentType, err := NormalizeForUpload(&input, &output, "photo.jpeg", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "photo.jpg", fileName)
	require.Equal(t, "image/jpeg", contentType)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, [2]int{300, 200}, [2]int{cfg.Width, cfg.Height})
	require.LessOrEqual(t, output.Len(), inputLen*3/2, "about the size it came in at, not a PNG's")
}

// Few-colour images (masks, sketches, screenshots) are stored with an exact palette:
// lossless, and much smaller than RGB.
func TestNormalizeForUploadPalettesFewColourImages(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 800, 600))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.Draw(src, image.Rect(0, 0, 800, 200), image.NewUniform(color.White), image.Point{}, draw.Src)
	src.Set(400, 400, color.RGBA{R: 239, G: 68, B: 68, A: 255})
	var input bytes.Buffer
	require.NoError(t, (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&input, src))

	var output bytes.Buffer
	_, _, err := NormalizeForUpload(bytes.NewReader(input.Bytes()), &output, "mask.png", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	normalized, err := png.Decode(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	paletted, ok := normalized.(*image.Paletted)
	require.True(t, ok, "want a paletted PNG, got %T", normalized)
	require.Len(t, paletted.Palette, 3)
	require.Equal(t, color.NRGBA{R: 239, G: 68, B: 68, A: 255}, color.NRGBAModel.Convert(normalized.At(400, 400)))
	require.Equal(t, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBAModel.Convert(normalized.At(10, 10)))
}

func TestNormalizeForUploadDropsSixteenBitSamples(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA64(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			src.SetRGBA64(x, y, color.RGBA64{R: uint16(x * 160), G: uint16(y * 210), B: 0x8000, A: 0xffff})
		}
	}
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, src))

	var output bytes.Buffer
	_, _, err := NormalizeForUpload(bytes.NewReader(input.Bytes()), &output, "deep.png", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	cfg, err := png.DecodeConfig(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	require.Equal(t, color.RGBAModel, cfg.ColorModel)
	require.Less(t, output.Len(), input.Len())
}

func TestScaledDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		srcW, srcH           int
		maxPx                int
		expectedW, expectedH int
	}{
		{name: "wide", srcW: 4000, srcH: 2000, maxPx: 2048, expectedW: 2048, expectedH: 1024},
		{name: "tall", srcW: 2000, srcH: 4000, maxPx: 2048, expectedW: 1024, expectedH: 2048},
		{name: "one pixel wide", srcW: 1, srcH: 4000, maxPx: 2048, expectedW: 1, expectedH: 2048},
		{name: "one pixel tall", srcW: 4000, srcH: 1, maxPx: 2048, expectedW: 2048, expectedH: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height := scaledDimensions(tt.srcW, tt.srcH, tt.maxPx)
			require.Equal(t, tt.expectedW, width)
			require.Equal(t, tt.expectedH, height)
		})
	}
}

type recordingStore struct{ files map[string][]byte }

func (s *recordingStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.files[key] = content
	return nil
}
func (s *recordingStore) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (s *recordingStore) DeleteFile(context.Context, string) error             { return nil }

// A thumbnail the caller already made is stored as is; without one, it is generated.
func TestUploadBytesToGalleryPathUsesTheGivenThumbnail(t *testing.T) {
	t.Parallel()

	var pic bytes.Buffer
	require.NoError(t, png.Encode(&pic, image.NewNRGBA(image.Rect(0, 0, 512, 512))))
	userID, attID := uuid.New(), uuid.New()
	thumbKey := storage.FileKeyForImageThumbnail(userID, attID)

	store := &recordingStore{files: map[string][]byte{}}
	UploadBytesToGalleryPath(t.Context(), store, zap.NewNop(), userID, attID, "a.png", "image/png", pic.Bytes(), []byte("premade"))
	require.Equal(t, []byte("premade"), store.files[thumbKey])
	require.Equal(t, pic.Bytes(), store.files[storage.FileKeyForImage(userID, attID, "a.png")])

	store = &recordingStore{files: map[string][]byte{}}
	UploadBytesToGalleryPath(t.Context(), store, zap.NewNop(), userID, attID, "a.png", "image/png", pic.Bytes(), nil)
	_, format, err := image.Decode(bytes.NewReader(store.files[thumbKey]))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
}
