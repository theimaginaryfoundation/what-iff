package handlerutils

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// decodedImage is the format and size of the image in a temp file ProcessUpload returned: the
// bytes that get archived to the object store and later sent to every vision provider.
type decodedImage struct {
	format        string
	width, height int
}

func decodeTempImage(t *testing.T, path string) decodedImage {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	img, format, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	bounds := img.Bounds()
	return decodedImage{format: format, width: bounds.Dx(), height: bounds.Dy()}
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

func TestUploadFileAttachmentNormalizesOversizedImage(t *testing.T) {
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&imageBytes, img))

	response := httptest.NewRecorder()
	attachment, tempPath, err := UploadFileAttachment(
		response,
		multipartUploadRequest(t, "wide.png", imageBytes.Bytes()),
		zap.NewNop(), uuid.New(),
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, "wide.png", attachment.Name)
	require.Equal(t, "image/png", attachment.FileType)
	require.Nil(t, attachment.FileID, "nothing is uploaded to a provider, so there is no provider file id")
	require.Equal(t, decodedImage{format: "png", width: 2048, height: 1024}, decodeTempImage(t, tempPath))
}

func TestUploadFileAttachmentReencodesJPEGAsPNGWithoutUpscaling(t *testing.T) {
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 640, 320))
	require.NoError(t, jpeg.Encode(&imageBytes, img, &jpeg.Options{Quality: 90}))

	response := httptest.NewRecorder()
	attachment, tempPath, err := UploadFileAttachment(
		response,
		multipartUploadRequest(t, "photo.jpg", imageBytes.Bytes()),
		zap.NewNop(), uuid.New(),
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, "photo.png", attachment.Name)
	require.Equal(t, "image/png", attachment.FileType)
	require.Equal(t, decodedImage{format: "png", width: 640, height: 320}, decodeTempImage(t, tempPath))
}

func TestUploadFileAttachmentLeavesNonImageBytesAndMetadataUnchanged(t *testing.T) {
	original := []byte("plain text upload\n")
	response := httptest.NewRecorder()
	attachment, tempPath, err := UploadFileAttachment(
		response,
		multipartUploadRequest(t, "notes.txt", original),
		zap.NewNop(), uuid.New(),
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, "notes.txt", attachment.Name)
	require.Equal(t, "text/plain", attachment.FileType)

	tempBytes, err := os.ReadFile(tempPath)
	require.NoError(t, err)
	require.Equal(t, original, tempBytes)
}
