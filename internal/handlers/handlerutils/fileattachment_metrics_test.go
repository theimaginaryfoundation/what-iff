package handlerutils

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"
	"go.uber.org/zap"
)

type failingFileAttachmentUploader struct{}

func (failingFileAttachmentUploader) UploadFileAttachment(context.Context, uuid.UUID, map[string]string, io.Reader, string, utils.FileTypeInfo) (string, error) {
	return "", errors.New("provider down")
}

type failingFileStore struct{}

func (failingFileStore) UploadFile(context.Context, string, []byte, string) error {
	return errors.New("s3 down")
}
func (failingFileStore) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (failingFileStore) DeleteFile(context.Context, string) error             { return nil }

func uploadCount(tm *telemetrytest.Recorder, t *testing.T, kind, outcome string) int64 {
	return tm.CounterValue(t, telemetry.FileUploads.Name,
		telemetry.AttrKind.String(kind), telemetry.AttrOutcome.String(outcome))
}

// A successful upload records its size and kind, and is counted once as a success: by
// TriggerAsyncFileChunking, the step every upload path runs after storing the attachment.
func TestUploadFileAttachmentMetricsCountSuccessOnce(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	payload := []byte("hello, world")

	attachment, tempPath, err := UploadFileAttachment(httptest.NewRecorder(),
		multipartUploadRequest(t, "notes.txt", payload), zap.NewNop(), &capturingFileAttachmentUploader{}, uuid.New(), nil)
	require.NoError(t, err)
	defer os.Remove(tempPath)

	upload := telemetry.AttrOperation.String(telemetry.FileOpUpload)
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileSize.Name, upload, telemetry.AttrKind.String("text")))
	require.Equal(t, float64(len(payload)), tm.HistogramSum(t, telemetry.FileSize.Name, upload))
	require.Zero(t, tm.CounterValue(t, telemetry.FileUploads.Name), "success is counted later, not by UploadFileAttachment")

	TriggerAsyncFileChunking(zap.NewNop(), nil, uuid.New(), "", attachment.Name)
	require.Equal(t, int64(1), uploadCount(tm, t, "text", telemetry.FileUploadSuccess))
	require.Equal(t, int64(1), tm.CounterValue(t, telemetry.FileUploads.Name))
}

func TestUploadFileAttachmentMetricsCountFailures(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)

	_, _, err := UploadFileAttachment(httptest.NewRecorder(),
		multipartUploadRequest(t, "notes.txt", []byte("hi")), zap.NewNop(), failingFileAttachmentUploader{}, uuid.New(), nil)
	require.Error(t, err)
	require.Equal(t, int64(1), uploadCount(tm, t, "text", telemetry.FileUploadFailure))

	// An unrecognised extension fails before the type is known, so it is counted as "other".
	_, _, err = UploadFileAttachment(httptest.NewRecorder(),
		multipartUploadRequest(t, "binary.exe", []byte("MZ")), zap.NewNop(), &capturingFileAttachmentUploader{}, uuid.New(), nil)
	require.Error(t, err)
	require.Equal(t, int64(1), uploadCount(tm, t, "other", telemetry.FileUploadFailure))

	// An S3 archive failure after a good provider upload is also a failed upload.
	tmp, err := os.CreateTemp(t.TempDir(), "upload-*")
	require.NoError(t, err)
	require.NoError(t, tmp.Close())
	require.Error(t, UploadToS3(context.Background(), failingFileStore{}, "key", tmp.Name(), "text/plain"))
	require.Equal(t, int64(2), uploadCount(tm, t, "text", telemetry.FileUploadFailure))
	require.Zero(t, tm.CounterValue(t, telemetry.FileUploads.Name, telemetry.AttrOutcome.String(telemetry.FileUploadSuccess)))
}

func TestUploadFileAttachmentRecordsImageNormalizeStage(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 32))))

	_, tempPath, err := UploadFileAttachment(httptest.NewRecorder(),
		multipartUploadRequest(t, "pic.png", buf.Bytes()), zap.NewNop(), &capturingFileAttachmentUploader{}, uuid.New(), nil)
	require.NoError(t, err)
	defer os.Remove(tempPath)

	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileOperationDuration.Name,
		telemetry.AttrOperation.String(telemetry.FileOpUpload), telemetry.AttrStage.String(telemetry.FileStageNormalize)))
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.FileSize.Name, telemetry.AttrKind.String("image")))
}
