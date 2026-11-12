package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// photoLike is a w×h image with smooth gradients and per-pixel noise: more than 256
// colours, and as hard for deflate as a photo or a generated image.
func photoLike(w, h int, alpha func(x, y int) uint8) *image.NRGBA {
	rng := rand.New(rand.NewSource(1))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := func(v int) uint8 { return uint8(min(255, max(0, v+rng.Intn(9)-4))) }
			a := uint8(255)
			if alpha != nil {
				a = alpha(x, y)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: n(x * 255 / w), G: n(y * 255 / h), B: n(128 + (x-y)/4), A: a})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func decodeNRGBA(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return toNRGBA(img)
}

// maxChannelError is the largest difference between a and b in any colour channel,
// and whether every alpha value matches.
func maxChannelError(a, b *image.NRGBA) (int, bool) {
	worst, alphaExact := 0, true
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		if i%4 == 3 {
			alphaExact = alphaExact && d == 0
			continue
		}
		worst = max(worst, d)
	}
	return worst, alphaExact
}

func TestQuantizeResidualsStaysWithinHalfAStep(t *testing.T) {
	t.Parallel()
	src := photoLike(97, 61, func(x, y int) uint8 { return uint8(x * 2) })
	out := quantizeResiduals(src, residualStep)
	worst, alphaExact := maxChannelError(src, out)
	require.LessOrEqual(t, worst, residualStep/2)
	require.True(t, alphaExact, "alpha is never quantized")

	// What makes it compress: every colour byte the Up filter would store is a multiple of the step.
	for y := 1; y < 61; y++ {
		for i := 0; i < 97*4; i++ {
			if i%4 == 3 {
				continue
			}
			v, up := int(out.Pix[y*out.Stride+i]), int(out.Pix[(y-1)*out.Stride+i])
			if v == 0 || v == 255 {
				continue // clamped
			}
			require.Zero(t, (v-up)%residualStep, "row %d byte %d", y, i)
		}
	}
}

func TestCompactPNGShrinksPhotosNearLosslessly(t *testing.T) {
	t.Parallel()
	src := photoLike(512, 384, nil)
	original := encodePNG(t, src)

	compact, err := CompactPNG(original)
	require.NoError(t, err)
	require.Less(t, len(compact), len(original)*2/3, "at least a third smaller (got %d of %d bytes)", len(compact), len(original))
	out := decodeNRGBA(t, compact)
	require.Equal(t, src.Bounds(), out.Bounds())
	worst, _ := maxChannelError(src, out)
	require.LessOrEqual(t, worst, residualStep/2)

	cfg, err := png.DecodeConfig(bytes.NewReader(compact))
	require.NoError(t, err)
	require.Equal(t, color.RGBAModel, cfg.ColorModel, "opaque: written as 8-bit RGB")
}

func TestCompactPNGKeepsFewColourImagesExact(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if x < 100 {
				src.SetNRGBA(x, y, color.NRGBA{A: 255})
			} else {
				src.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	var raw bytes.Buffer
	require.NoError(t, (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&raw, src))

	compact, err := CompactPNG(raw.Bytes())
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(compact))
	require.NoError(t, err)
	_, paletted := img.(*image.Paletted)
	require.True(t, paletted, "a two-colour mask is stored as an exact palette")
	worst, _ := maxChannelError(src, toNRGBA(img))
	require.Zero(t, worst)
}

func TestCompactPNGLeavesOtherFormatsAndBiggerResultsAlone(t *testing.T) {
	t.Parallel()
	var jpg bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpg, photoLike(64, 64, nil), nil))
	out, err := CompactPNG(jpg.Bytes())
	require.NoError(t, err)
	require.Equal(t, jpg.Bytes(), out, "a JPEG is not turned into a PNG")

	// A tiny PNG already at its smallest is returned as it is.
	tiny := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	out, err = CompactPNG(tiny)
	require.NoError(t, err)
	require.Equal(t, tiny, out)

	garbage := []byte("not an image")
	out, err = CompactPNG(garbage)
	require.NoError(t, err)
	require.Equal(t, garbage, out, "not a PNG: returned without decoding")

	_, err = CompactPNG(append([]byte(pngSignature), "truncated"...))
	require.Error(t, err, "a broken PNG is an error")
}

func TestCompactPNGForStorageNeverFails(t *testing.T) {
	t.Parallel()
	original := encodePNG(t, photoLike(256, 192, nil))
	require.Less(t, len(CompactPNGForStorage(original)), len(original))
	garbage := []byte("not an image")
	require.Equal(t, garbage, CompactPNGForStorage(garbage), "stored as it came rather than lost")
}
