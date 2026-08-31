package handlerutils

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

type capturingFileAttachmentUploader struct {
	filename    string
	contentType string
	format      string
	width       int
	height      int
	data        []byte
}

func (u *capturingFileAttachmentUploader) UploadFileAttachment(
	_ context.Context,
	_ uuid.UUID,
	_ map[string]string,
	file io.Reader,
	fileName string,
	fileTypeInfo utils.FileTypeInfo,
) (string, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	u.filename = fileName
	u.contentType = fileTypeInfo.ContentType
	u.data = data

	if strings.HasPrefix(fileTypeInfo.ContentType, "image/") {
		img, format, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		bounds := img.Bounds()
		u.format = format
		u.width = bounds.Dx()
		u.height = bounds.Dy()
	}
	return "file-normalized", nil
}

func multipartUploadRequest(t *testing.T, filename string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("attachment", filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/attachments", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestUploadFileAttachmentNormalizesOversizedImageBeforeProviderUpload(t *testing.T) {
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&imageBytes, img))

	response := httptest.NewRecorder()
	uploader := &capturingFileAttachmentUploader{}
	attachment, tempPath, err := UploadFileAttachment(
		response,
		multipartUploadRequest(t, "wide.png", imageBytes.Bytes()),
		zap.NewNop(), uploader, uuid.New(), nil,
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, 2048, uploader.width)
	require.Equal(t, 1024, uploader.height)
	require.Equal(t, "png", uploader.format)
	require.Equal(t, "wide.png", uploader.filename)
	require.Equal(t, "image/png", uploader.contentType)
	require.Equal(t, "wide.png", attachment.Name)
	require.Equal(t, "image/png", attachment.FileType)

	tempBytes, err := os.ReadFile(tempPath)
	require.NoError(t, err)
	_, tempFormat, err := image.Decode(bytes.NewReader(tempBytes))
	require.NoError(t, err)
	require.Equal(t, "png", tempFormat)
}

func TestUploadFileAttachmentReencodesJPEGAsPNGWithoutUpscaling(t *testing.T) {
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 640, 320))
	require.NoError(t, jpeg.Encode(&imageBytes, img, &jpeg.Options{Quality: 90}))

	response := httptest.NewRecorder()
	uploader := &capturingFileAttachmentUploader{}
	attachment, tempPath, err := UploadFileAttachment(
		response,
		multipartUploadRequest(t, "photo.jpg", imageBytes.Bytes()),
		zap.NewNop(), uploader, uuid.New(), nil,
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, 640, uploader.width)
	require.Equal(t, 320, uploader.height)
	require.Equal(t, "png", uploader.format)
	require.Equal(t, "photo.png", uploader.filename)
	require.Equal(t, "image/png", uploader.contentType)
	require.Equal(t, "photo.png", attachment.Name)
	require.Equal(t, "image/png", attachment.FileType)
}

func TestUploadFileAttachmentLeavesNonImageBytesAndMetadataUnchanged(t *testing.T) {
	original := []byte("plain text upload\n")
	response := httptest.NewRecorder()
	uploader := &capturingFileAttachmentUploader{}
	attachment, tempPath, err := UploadFileAttachment(
		response,
		multipartUploadRequest(t, "notes.txt", original),
		zap.NewNop(), uploader, uuid.New(), nil,
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, "notes.txt", uploader.filename)
	require.Equal(t, "text/plain", uploader.contentType)
	require.Equal(t, original, uploader.data)
	require.Equal(t, "notes.txt", attachment.Name)
	require.Equal(t, "text/plain", attachment.FileType)

	tempBytes, err := os.ReadFile(tempPath)
	require.NoError(t, err)
	require.Equal(t, original, tempBytes)
}
