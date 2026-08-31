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
)

const (
	DefaultThumbnailMaxPx       = 256
	DefaultUploadImageMaxPx     = 2048
	NormalizedUploadContentType = "image/png"
)

// NormalizeForUpload decodes an accepted image from input, scales it down when
// its longest edge exceeds maxPx, and encodes the result as PNG to output. It
// returns the canonical PNG filename. Images that already fit keep their
// original dimensions; normalization never upscales.
func NormalizeForUpload(input io.Reader, output io.Writer, fileName string, maxPx int) (string, error) {
	if maxPx <= 0 {
		maxPx = DefaultUploadImageMaxPx
	}

	src, _, err := image.Decode(input)
	if err != nil {
		return "", fmt.Errorf("decode image for upload: %w", err)
	}

	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return "", fmt.Errorf("image has zero dimension (%dx%d)", srcW, srcH)
	}

	dst := src
	if srcW > maxPx || srcH > maxPx {
		dstW, dstH := scaledDimensions(srcW, srcH, maxPx)
		resized := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
		xdraw.BiLinear.Scale(resized, resized.Bounds(), src, bounds, xdraw.Over, nil)
		dst = resized
	}

	if err := png.Encode(output, dst); err != nil {
		return "", fmt.Errorf("encode upload image as PNG: %w", err)
	}
	return fileName[:len(fileName)-len(filepath.Ext(fileName))] + ".png", nil
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
// image attachment and generates + uploads a thumbnail. Both are best-effort:
// failures are logged but never returned.
func UploadBytesToGalleryPath(
	ctx context.Context,
	fileStore storage.FileStore,
	logger *zap.Logger,
	userID, attachmentID uuid.UUID,
	filename, contentType string,
	data []byte,
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

	thumb, err := GenerateThumbnail(data, DefaultThumbnailMaxPx)
	if err != nil {
		logger.Warn("imageutil: failed to generate thumbnail",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
		return
	}
	thumbKey := storage.FileKeyForImageThumbnail(userID, attachmentID)
	if err := fileStore.UploadFile(ctx, thumbKey, thumb, "image/jpeg"); err != nil {
		logger.Warn("imageutil: failed to upload thumbnail",
			zap.String("attachment_id", attachmentID.String()),
			zap.Error(err))
	}
}
