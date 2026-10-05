package handlerutils

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.uber.org/zap"
)

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestUploadErrorMessageIncludesTheCauseWhenThereIsOne(t *testing.T) {
	require.Equal(t, "File too large", (&UploadError{Message: "File too large"}).Error())
	require.Equal(t, "Error uploading file: s3 down", uploadErr(http.StatusInternalServerError, "Error uploading file", errors.New("s3 down")).Error())
}

func TestUploadFileAttachmentRejectsAnUnparseableBody(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	req := httptest.NewRequest(http.MethodPost, "/attachments", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	rec := httptest.NewRecorder()

	_, tempPath, err := UploadFileAttachment(rec, req, zap.NewNop(), &capturingFileAttachmentUploader{}, uuid.New(), nil)

	require.Error(t, err)
	require.Empty(t, tempPath)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, int64(1), uploadCount(tm, t, "other", telemetry.FileUploadFailure))
}

func TestUploadFileAttachmentRejectsARequestWithoutTheAttachmentPart(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("wrong-field", "notes.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hi"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/attachments", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	_, tempPath, err := UploadFileAttachment(rec, req, zap.NewNop(), &capturingFileAttachmentUploader{}, uuid.New(), nil)

	require.Error(t, err)
	require.Empty(t, tempPath)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Error retrieving file")
	require.Equal(t, int64(1), uploadCount(tm, t, "other", telemetry.FileUploadFailure))
}

func TestUploadFileAttachmentRespondsWithTheProcessUploadError(t *testing.T) {
	rec := httptest.NewRecorder()

	_, _, err := UploadFileAttachment(rec, multipartUploadRequest(t, "binary.exe", []byte("MZ")), zap.NewNop(), &capturingFileAttachmentUploader{}, uuid.New(), nil)

	require.ErrorIs(t, err, ErrUnsupportedFileType)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Unsupported file type")
}

func TestProcessUploadReportsAnUnreadableSourceAsAServerError(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)

	_, tempPath, err := ProcessUpload(context.Background(), &capturingFileAttachmentUploader{}, uuid.New(), nil, errReader{err: errors.New("connection reset")}, "notes.txt")

	require.Empty(t, tempPath)
	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, http.StatusInternalServerError, ue.Status)
	require.Equal(t, "Error buffering file", ue.Message)
	require.Equal(t, int64(1), uploadCount(tm, t, "text", telemetry.FileUploadFailure))
}

func TestProcessUploadRejectsAnImageThatCannotBeDecoded(t *testing.T) {
	_, tempPath, err := ProcessUpload(context.Background(), &capturingFileAttachmentUploader{}, uuid.New(), nil, strings.NewReader("this is not a png"), "pic.png")

	require.Empty(t, tempPath)
	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, http.StatusBadRequest, ue.Status)
	require.Equal(t, "Invalid image", ue.Message)
}

func TestStoreChatAttachmentSucceedsWhenTheS3KeyCannotBePersisted(t *testing.T) {
	records, files := &fakeRecords{s3KeyErr: errors.New("db hiccup")}, &recordingFileStore{}
	attachment, tempPath := prepared(t, "notes.txt", []byte("hi"))

	created, err := StoreChatAttachment(context.Background(), zap.NewNop(), AttachmentStorage{Records: records, Files: files}, uuid.New(), uuid.New(), attachment, tempPath)

	require.NoError(t, err, "the key is a convenience for rename/delete; the file is already saved")
	require.NotEqual(t, uuid.Nil, created.ID)
	require.Len(t, files.uploaded, 1)
}

// tempUpload writes a file the way ProcessUpload leaves one for TriggerAsyncFileChunking.
func tempUpload(t *testing.T, size int64) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "upload-*")
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	require.NoError(t, f.Close())
	return f.Name()
}

func requireRemoved(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}, 2*time.Second, 10*time.Millisecond, "the temp upload is cleaned up")
}

func TestTriggerAsyncFileChunkingDropsAnUploadTooBigToChunk(t *testing.T) {
	path := tempUpload(t, maxAsyncProcessingBytes+1)

	TriggerAsyncFileChunking(zap.NewNop(), nil, uuid.New(), path, "big.txt")

	requireRemoved(t, path)
}

func TestTriggerAsyncFileChunkingKeepsGoingWhenTheTempFileCannotBeStatted(t *testing.T) {
	// The stat failure is only logged; an unrecognised name then ends it without chunking.
	missing := t.TempDir() + string(os.PathSeparator) + "gone"

	require.NotPanics(t, func() {
		TriggerAsyncFileChunking(zap.NewNop(), nil, uuid.New(), missing, "binary.exe")
	})
}

func TestTriggerAsyncFileChunkingRemovesFilesItWillNotChunk(t *testing.T) {
	for name, fileName := range map[string]string{
		"unrecognised type":    "binary.exe",
		"no vector support":    "table.csv",
		"vector but not text":  "report.docx",
		"image without vector": "pic.gif",
	} {
		t.Run(name, func(t *testing.T) {
			path := tempUpload(t, 4)

			TriggerAsyncFileChunking(zap.NewNop(), nil, uuid.New(), path, fileName)

			requireRemoved(t, path)
		})
	}
}

func TestTriggerAsyncFileChunkingChunksATextFileInTheBackgroundAndCleansUp(t *testing.T) {
	path := tempUpload(t, 4)
	pipeline := filechunker.NewMockFileChunkPipeline(nil, zap.NewNop())

	TriggerAsyncFileChunking(zap.NewNop(), pipeline, uuid.New(), path, "notes.txt")

	requireRemoved(t, path)
}

func TestTriggerAsyncFileChunkingRecoversFromAPipelineFailure(t *testing.T) {
	path := tempUpload(t, 4)

	// A nil pipeline panics inside the goroutine; the recover keeps it from taking the server down.
	TriggerAsyncFileChunking(zap.NewNop(), nil, uuid.New(), path, "notes.txt")

	requireRemoved(t, path)
}
