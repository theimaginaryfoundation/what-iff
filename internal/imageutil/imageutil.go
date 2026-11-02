// Package imageutil provides image processing and gallery upload helpers that
// are shared between the agent layer and the HTTP handler layer without
// creating import cycles.
package imageutil

import (
	"bytes"
	"context"
	"fmt"
	"image"
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
	// UploadJPEGQuality is the quality opaque uploads are re-encoded at. A 2048px photo
	// lands around 1 MB, where the PNG it used to become was ~5 MB (what-iff#419).
	UploadJPEGQuality = 90
)

// NormalizeForUpload decodes an accepted image from input, scales it down when
// its longest edge exceeds maxPx, and re-encodes it to output: JPEG for opaque
// images, PNG for images with transparency (JPEG has no alpha channel). It
// returns the renamed filename and its content type. Images that already fit
// keep their original dimensions; normalization never upscales. Re-encoding
// also drops metadata such as EXIF.
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
	if !isOpaque(src) {
		if err := png.Encode(output, dst); err != nil {
			return "", "", fmt.Errorf("encode upload image as PNG: %w", err)
		}
		return base + ".png", "image/png", nil
	}
	if err := jpeg.Encode(output, dst, &jpeg.Options{Quality: UploadJPEGQuality}); err != nil {
		return "", "", fmt.Errorf("encode upload image as JPEG: %w", err)
	}
	return base + ".jpg", "image/jpeg", nil
}

// isOpaque reports whether every pixel of img is fully opaque. Decoded image types
// implement Opaque (YCbCr, as JPEG decodes, always is); anything else is treated as
// possibly transparent so it keeps its alpha as a PNG.
func isOpaque(img image.Image) bool {
	o, ok := img.(interface{ Opaque() bool })
	return ok && o.Opaque()
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
