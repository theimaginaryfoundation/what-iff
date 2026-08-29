package handlerutils

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

type capturingFileAttachmentUploader struct {
	filename    string
	contentType string
	width       int
	height      int
}

func (u *capturingFileAttachmentUploader) UploadFileAttachment(
	_ context.Context,
	_ uuid.UUID,
	_ map[string]string,
	file io.Reader,
	fileName string,
	fileTypeInfo utils.FileTypeInfo,
) (string, error) {
	img, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}
	bounds := img.Bounds()
	u.filename = fileName
	u.contentType = fileTypeInfo.ContentType
	u.width = bounds.Dx()
	u.height = bounds.Dy()
	return "file-normalized", nil
}

func TestUploadFileAttachmentNormalizesOversizedImageBeforeProviderUpload(t *testing.T) {
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4000, 2000))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&imageBytes, img))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("attachment", "wide.png")
	require.NoError(t, err)
	_, err = part.Write(imageBytes.Bytes())
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/attachments", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	uploader := &capturingFileAttachmentUploader{}

	attachment, tempPath, err := UploadFileAttachment(
		response,
		req,
		zap.NewNop(),
		uploader,
		uuid.New(),
		nil,
	)
	if tempPath != "" {
		defer os.Remove(tempPath)
	}

	require.NoError(t, err)
	require.Equal(t, 2048, uploader.width)
	require.Equal(t, 1024, uploader.height)
	require.Equal(t, "wide.png", uploader.filename)
	require.Equal(t, "image/png", uploader.contentType)
	require.Equal(t, "wide.png", attachment.Name)
	require.Equal(t, "image/png", attachment.FileType)
}
