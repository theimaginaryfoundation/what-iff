package handlerutils

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.uber.org/zap"
)

type fakeRecords struct {
	mu        sync.Mutex
	created   []models.FileAttachment
	deleted   []uuid.UUID
	s3Keys    map[uuid.UUID]string
	createErr error
	s3KeyErr  error
}

func (f *fakeRecords) CreateFileAttachment(_ context.Context, _ uuid.UUID, fa models.FileAttachment) (*models.FileAttachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	fa.ID = uuid.New()
	f.created = append(f.created, fa)
	return &fa, nil
}

func (f *fakeRecords) DeleteFileAttachment(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeRecords) SetFileAttachmentS3Key(_ context.Context, _, id uuid.UUID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.s3KeyErr != nil {
		return f.s3KeyErr
	}
	if f.s3Keys == nil {
		f.s3Keys = map[uuid.UUID]string{}
	}
	f.s3Keys[id] = key
	return nil
}

type recordingFileStore struct {
	mu       sync.Mutex
	uploaded map[string][]byte
	err      error
}

func (s *recordingFileStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.uploaded == nil {
		s.uploaded = map[string][]byte{}
	}
	s.uploaded[key] = content
	return nil
}
func (s *recordingFileStore) DownloadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (s *recordingFileStore) DeleteFile(context.Context, string) error             { return nil }

var _ storage.FileStore = (*recordingFileStore)(nil)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 32))))
	return buf.Bytes()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { return len(p), nil }

func TestProcessUploadBuffersAndUploadsAFileWithoutHTTP(t *testing.T) {
	uploader := &capturingFileAttachmentUploader{}
	userID := uuid.New()

	attachment, tempPath, err := ProcessUpload(context.Background(), uploader, userID, nil, strings.NewReader("hello"), "notes.txt")
	require.NoError(t, err)
	defer os.Remove(tempPath)

	require.Equal(t, userID, attachment.UserID)
	require.Equal(t, "notes.txt", attachment.Name)
	require.Equal(t, "text/plain", attachment.FileType)
	require.NotNil(t, attachment.FileID)
	require.Equal(t, []byte("hello"), uploader.data)
	onDisk, err := os.ReadFile(tempPath)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), onDisk, "the caller gets the buffered file to store and chunk")
}

func TestProcessUploadNormalizesImagesToPNG(t *testing.T) {
	attachment, tempPath, err := ProcessUpload(context.Background(), &capturingFileAttachmentUploader{}, uuid.New(), nil, bytes.NewReader(pngBytes(t)), "pic.png")
	require.NoError(t, err)
	defer os.Remove(tempPath)
	require.True(t, strings.HasPrefix(attachment.FileType, models.ImageMIMEPrefix))
}

func TestProcessUploadRejectsAnUnsupportedTypeAndCountsIt(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)

	_, tempPath, err := ProcessUpload(context.Background(), &capturingFileAttachmentUploader{}, uuid.New(), nil, strings.NewReader("MZ"), "binary.exe")

	require.Empty(t, tempPath)
	require.ErrorIs(t, err, ErrUnsupportedFileType)
	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, http.StatusBadRequest, ue.Status)
	require.Equal(t, "Unsupported file type", ue.Message)
	require.Equal(t, int64(1), uploadCount(tm, t, "other", telemetry.FileUploadFailure))
}

func TestProcessUploadRejectsAFileOverTheUploadLimit(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)

	_, tempPath, err := ProcessUpload(context.Background(), &capturingFileAttachmentUploader{}, uuid.New(), nil, zeroReader{}, "big.txt")

	require.Empty(t, tempPath)
	require.ErrorIs(t, err, ErrFileTooLarge)
	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, http.StatusBadRequest, ue.Status)
	require.Equal(t, int64(1), uploadCount(tm, t, "text", telemetry.FileUploadFailure))
}

func TestProcessUploadReportsAProviderFailureAsAServerError(t *testing.T) {
	_, tempPath, err := ProcessUpload(context.Background(), failingFileAttachmentUploader{}, uuid.New(), nil, strings.NewReader("hi"), "notes.txt")
	require.Empty(t, tempPath)
	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, http.StatusInternalServerError, ue.Status)
}

func prepared(t *testing.T, name string, data []byte) (models.FileAttachment, string) {
	t.Helper()
	attachment, tempPath, err := ProcessUpload(context.Background(), &capturingFileAttachmentUploader{}, uuid.New(), nil, bytes.NewReader(data), name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(tempPath) })
	return attachment, tempPath
}

func TestStoreChatAttachmentFilesATextUploadUnderTheChat(t *testing.T) {
	records, files := &fakeRecords{}, &recordingFileStore{}
	userID, chatID := uuid.New(), uuid.New()
	attachment, tempPath := prepared(t, "notes.txt", []byte("hello"))

	created, err := StoreChatAttachment(context.Background(), zap.NewNop(), AttachmentStorage{Records: records, Files: files}, userID, chatID, attachment, tempPath)

	require.NoError(t, err)
	key := storage.FileKeyForChat(userID, chatID, created.ID, "notes.txt")
	require.Equal(t, []byte("hello"), files.uploaded[key])
	require.Equal(t, key, records.s3Keys[created.ID])
	require.Len(t, files.uploaded, 1, "no thumbnail for a text file")
}

func TestStoreChatAttachmentFilesAnImageInTheGalleryWithAThumbnail(t *testing.T) {
	records, files := &fakeRecords{}, &recordingFileStore{}
	userID := uuid.New()
	attachment, tempPath := prepared(t, "pic.png", pngBytes(t))

	created, err := StoreChatAttachment(context.Background(), zap.NewNop(), AttachmentStorage{Records: records, Files: files}, userID, uuid.New(), attachment, tempPath)

	require.NoError(t, err)
	key := storage.FileKeyForImage(userID, created.ID, attachment.Name)
	require.Contains(t, files.uploaded, key)
	require.Equal(t, key, records.s3Keys[created.ID])
	require.Len(t, files.uploaded, 2, "the image and its thumbnail")
}

func TestStoreChatAttachmentRollsBackWhenTheObjectStoreFails(t *testing.T) {
	records, files := &fakeRecords{}, &recordingFileStore{err: errors.New("s3 down")}
	deleter := &recordingFileAttachmentDeleter{}
	attachment, tempPath := prepared(t, "notes.txt", []byte("hi"))

	_, err := StoreChatAttachment(context.Background(), zap.NewNop(), AttachmentStorage{Records: records, Files: files, Provider: deleter}, uuid.New(), uuid.New(), attachment, tempPath)

	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, http.StatusInternalServerError, ue.Status)
	require.Equal(t, "Error saving file — please retry", ue.Message)
	require.Len(t, records.deleted, 1, "the record is removed")
	require.Equal(t, []string{*attachment.FileID}, deleter.deleted, "so is the provider's copy")
	_, statErr := os.Stat(tempPath)
	require.True(t, os.IsNotExist(statErr))
}

func TestStoreChatAttachmentCleansUpWhenTheRecordCannotBeSaved(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	records := &fakeRecords{createErr: errors.New("db down")}
	deleter := &recordingFileAttachmentDeleter{}
	attachment, tempPath := prepared(t, "notes.txt", []byte("hi"))

	_, err := StoreChatAttachment(context.Background(), zap.NewNop(), AttachmentStorage{Records: records, Provider: deleter}, uuid.New(), uuid.New(), attachment, tempPath)

	var ue *UploadError
	require.ErrorAs(t, err, &ue)
	require.Equal(t, "Error creating file attachment", ue.Message)
	require.Equal(t, []string{*attachment.FileID}, deleter.deleted)
	require.Equal(t, int64(1), uploadCount(tm, t, "text", telemetry.FileUploadFailure))
}

func TestStoreChatAttachmentWorksWithoutAnObjectStore(t *testing.T) {
	records := &fakeRecords{}
	attachment, tempPath := prepared(t, "notes.txt", []byte("hi"))

	created, err := StoreChatAttachment(context.Background(), zap.NewNop(), AttachmentStorage{Records: records}, uuid.New(), uuid.New(), attachment, tempPath)

	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)
}

func TestRespondWithUploadError(t *testing.T) {
	rec := httptest.NewRecorder()
	RespondWithUploadError(rec, zap.NewNop(), uploadErr(http.StatusBadRequest, "Unsupported file type", ErrUnsupportedFileType))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Unsupported file type")

	rec = httptest.NewRecorder()
	RespondWithUploadError(rec, zap.NewNop(), errors.New("boom"))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
