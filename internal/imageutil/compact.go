package imageutil

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
)

// Compact PNG encoding (ADR 0x047 §9). PNGs stay PNGs, for compatibility, but are
// stored far smaller than image vendors and screenshot tools write them:
//   - 8 bits per channel whatever the source had, and no alpha channel when every
//     pixel is opaque;
//   - an exact palette when the image has at most 256 colours (masks, sketches,
//     diagrams, most screenshots): lossless, and several times smaller;
//   - otherwise (photos, generated images) near-lossless residual quantization:
//     see quantizeResiduals. On a 2 MB OpenAI image it saved 63% at 43 dB PSNR,
//     against 9–14% for the best lossless optimizers.

// CompactPNG re-encodes a PNG as a compact one (see encodeCompactPNG), keeping its
// dimensions. It returns data unchanged when it is not a PNG, or when the result
// would not be smaller. Ancillary chunks (text, EXIF, C2PA provenance) are dropped.
func CompactPNG(data []byte) ([]byte, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if format != "png" {
		return data, nil
	}
	var out bytes.Buffer
	if err := encodeCompactPNG(&out, img); err != nil {
		return nil, err
	}
	if out.Len() >= len(data) {
		return data, nil
	}
	return out.Bytes(), nil
}

// CompactPNGForStorage is CompactPNG for code about to store an image: it returns
// the compact PNG, or data unchanged when data is not a PNG, would not shrink, or
// cannot be decoded (the caller stores what it has rather than nothing). Every path
// that stores a generated image goes through it (ADR 0x047 §9).
func CompactPNGForStorage(data []byte) []byte {
	compact, err := CompactPNG(data)
	if err != nil {
		return data
	}
	return compact
}

// encodeCompactPNG writes img as a compact PNG at the default zlib level (the best
// level saved under 1% for twice the time).
func encodeCompactPNG(w io.Writer, img image.Image) error {
	flat := toNRGBA(img)
	var out image.Image
	if p := exactPalette(flat); p != nil {
		out = p
	} else {
		out = quantizeResiduals(flat, residualStep)
	}
	return (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(w, out)
}

// toNRGBA copies img into 8-bit non-premultiplied RGBA, which the PNG encoder
// writes as RGB when every pixel is opaque.
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Draw(out, out.Bounds(), img, b.Min, xdraw.Src)
	return out
}

// maxPaletteColors is the most colours a paletted PNG holds.
const maxPaletteColors = 256

// exactPalette returns img as a paletted image when it has at most 256 distinct
// colours, nil otherwise. It stops at the 257th colour, so a photo costs little.
func exactPalette(img *image.NRGBA) *image.Paletted {
	index := make(map[color.NRGBA]uint8, maxPaletteColors)
	palette := make(color.Palette, 0, maxPaletteColors)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if _, ok := index[c]; ok {
				continue
			}
			if len(palette) == maxPaletteColors {
				return nil
			}
			index[c] = uint8(len(palette))
			palette = append(palette, c)
		}
	}
	out := image.NewPaletted(b, palette)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := out.Pix[(y-b.Min.Y)*out.Stride:]
		for x := b.Min.X; x < b.Max.X; x++ {
			row[x-b.Min.X] = index[img.NRGBAAt(x, y)]
		}
	}
	return out
}

// residualStep is the quantization step for colour residuals: each channel moves
// by at most residualStep/2 (±3 of 255). Measured on a generated photo, step 6
// showed no banding at 3× zoom; step 8 began to band smooth gradients.
const residualStep = 6

// quantizeResiduals returns a near-lossless copy of img that deflate compresses
// far better. PNG's Up filter stores each byte as its difference from the byte
// above; here every colour channel is rebuilt as the (already rebuilt) value above
// plus the original difference rounded to a multiple of step. The filtered rows the
// encoder sees are then mostly a handful of repeated values. Each channel stays
// within step/2 of the original; alpha is kept exactly.
func quantizeResiduals(img *image.NRGBA, step int) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	rowLen := b.Dx() * 4
	half := step / 2
	for y := 0; y < b.Dy(); y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+rowLen]
		dst := out.Pix[y*out.Stride : y*out.Stride+rowLen]
		var up []uint8
		if y > 0 {
			up = out.Pix[(y-1)*out.Stride : (y-1)*out.Stride+rowLen]
		}
		for i := 0; i < rowLen; i++ {
			if i%4 == 3 { // alpha
				dst[i] = src[i]
				continue
			}
			pred := 0
			if up != nil {
				pred = int(up[i])
			}
			diff := int(src[i]) - pred
			// Round to the nearest multiple of step, ties towards zero: a zero
			// residual compresses best, and on a generated photo this rounding
			// made the file a quarter smaller than rounding ties away from zero.
			if diff >= 0 {
				diff = (diff + half - 1) / step * step
			} else {
				diff = -((-diff + half - 1) / step * step)
			}
			v := pred + diff
			// Clamping only moves v towards the original, which is in range.
			dst[i] = uint8(min(255, max(0, v)))
		}
	}
	return out
}
