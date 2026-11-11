// Package imageutil provides image processing and gallery upload helpers that
// are shared between the agent layer and the HTTP handler layer without
// creating import cycles.
package imageutil

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // register GIF decoder
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"go.uber.org/zap"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register WebP decoder: some image vendors return WebP
)

const (
	DefaultThumbnailMaxPx   = 256
	DefaultUploadImageMaxPx = 2048
)

// NormalizeForUpload decodes an accepted image from input, scales it down when
// its longest edge exceeds maxPx, and re-encodes it to output as PNG. It returns
// the renamed filename and its content type. Images that already fit keep their
// original dimensions; normalization never upscales. Re-encoding also drops
// metadata such as EXIF.
//
// Uploads are lossless PNG again (ADR 0x045 reverts what-iff#419's JPEG for
// opaque uploads): JPEG blurred screenshots and drawings, and masks and markup
// need exact pixels. See encodeUploadPNG for how the PNG is kept small.
func NormalizeForUpload(input io.Reader, output io.Writer, fileName string, maxPx int) (string, string, error) {
	if maxPx <= 0 {
		maxPx = DefaultUploadImageMaxPx
	}

	src, _, err := image.Decode(input)
	if err != nil {
		return "", "", fmt.Errorf("decode image for upload: %w", err)
	}

	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return "", "", fmt.Errorf("image has zero dimension (%dx%d)", srcW, srcH)
	}

	dst := src
	if srcW > maxPx || srcH > maxPx {
		dstW, dstH := scaledDimensions(srcW, srcH, maxPx)
		resized := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
		xdraw.BiLinear.Scale(resized, resized.Bounds(), src, bounds, xdraw.Over, nil)
		dst = resized
	}

	base := fileName[:len(fileName)-len(filepath.Ext(fileName))]
	if err := encodeUploadPNG(output, dst); err != nil {
		return "", "", fmt.Errorf("encode upload image as PNG: %w", err)
	}
	return base + ".png", "image/png", nil
}

// encodeUploadPNG writes img as a compact PNG:
//   - 8 bits per channel, whatever the source had (a 16-bit PNG doubles in size for
//     nothing a screen shows), with no alpha channel when every pixel is opaque;
//   - an exact palette when the image has at most 256 colours (masks, sketches,
//     diagrams, most screenshots), which is lossless and several times smaller;
//   - default zlib compression: the best level saved well under 1% on photos for
//     twice the time.
func encodeUploadPNG(w io.Writer, img image.Image) error {
	flat := toNRGBA(img)
	var out image.Image = flat
	if p := exactPalette(flat); p != nil {
		out = p
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

func scaledDimensions(srcW, srcH, maxPx int) (int, int) {
	if srcW >= srcH {
		return maxPx, max(1, srcH*maxPx/srcW)
	}
	return max(1, srcW*maxPx/srcH), maxPx
}

// GenerateThumbnail decodes data as an image, scales it so the longest edge is
// at most maxPx pixels (aspect ratio preserved), and returns JPEG-encoded bytes.
func GenerateThumbnail(data []byte, maxPx int) ([]byte, error) {
	if maxPx <= 0 {
		maxPx = DefaultThumbnailMaxPx
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image for thumbnail: %w", err)
	}

	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return nil, fmt.Errorf("image has zero dimension (%dx%d)", srcW, srcH)
	}

	dstW, dstH := srcW, srcH
	if srcW > maxPx || srcH > maxPx {
		dstW, dstH = scaledDimensions(srcW, srcH, maxPx)
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode thumbnail as JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// UploadBytesToGalleryPath uploads data to the canonical gallery path for an
// image attachment, and its thumbnail: thumb when the caller already made one,
// otherwise one generated here. Both are best-effort: failures are logged but
// never returned.
func UploadBytesToGalleryPath(
	ctx context.Context,
	fileStore storage.FileStore,
	logger *zap.Logger,
	userID, attachmentID uuid.UUID,
	filename, contentType string,
	data, thumb []byte,
) {
	if fileStore == nil || len(data) == 0 {
		return
	}

	imageKey := storage.FileKeyForImage(userID, attachmentID, filename)
	if err := fileStore.UploadFile(ctx, imageKey, data, contentType); err != nil {
		logger.Warn("imageutil: failed to upload image to gallery path",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
	}

	if len(thumb) == 0 {
		var err error
		if thumb, err = GenerateThumbnail(data, DefaultThumbnailMaxPx); err != nil {
			logger.Warn("imageutil: failed to generate thumbnail",
				zap.String("attachment_id", attachmentID.String()),
				zap.Error(err))
			return
		}
	}
	thumbKey := storage.FileKeyForImageThumbnail(userID, attachmentID)
	if err := fileStore.UploadFile(ctx, thumbKey, thumb, "image/jpeg"); err != nil {
		logger.Warn("imageutil: failed to upload thumbnail",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
	}
}
