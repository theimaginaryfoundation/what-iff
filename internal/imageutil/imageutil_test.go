package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeForUploadScalesOpaqueToJPEG(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.RGBA{R: 40, G: 90, B: 160, A: 255}), image.Point{}, draw.Src)
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, src))

	var output bytes.Buffer
	fileName, contentType, err := NormalizeForUpload(&input, &output, "original.png", DefaultUploadImageMaxPx)
	require.NoError(t, err)
	require.Equal(t, "original.jpg", fileName)
	require.Equal(t, "image/jpeg", contentType)

	normalized, format, err := image.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, 2048, normalized.Bounds().Dx())
	require.Equal(t, 1024, normalized.Bounds().Dy())
}

// Transparency survives normalization: JPEG has no alpha channel, so an image with any
// non-opaque pixel stays a PNG rather than being flattened.
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
	require.Equal(t, "sunset.jpg", fileName)
	require.Equal(t, "image/jpeg", contentType)

	normalized, format, err := image.Decode(&output)
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, src.Bounds().Size(), normalized.Bounds().Size(), "small images keep their size")
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
