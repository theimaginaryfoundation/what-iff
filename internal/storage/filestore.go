package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// FileStore handles raw file archival to object storage.
type FileStore interface {
	UploadFile(ctx context.Context, key string, content []byte, contentType string) error
	// DownloadFile fetches the object at key. Returns (nil, nil) when the key does
	// not exist so callers can distinguish "not found" from a genuine I/O error.
	DownloadFile(ctx context.Context, key string) ([]byte, error)
	// DeleteFile removes the object at key. Returns nil if the key does not exist.
	DeleteFile(ctx context.Context, key string) error
}

// FileKeyForChat constructs the S3 object key for a chat-uploaded file.
// Format: users/{userID}/chats/{chatID}/{attachmentID_filename}
func FileKeyForChat(userID, chatID, attachmentID uuid.UUID, filename string) string {
	return path.Join("users", userID.String(), "chats", chatID.String(), filenameWithFallback(sanitizeFilename(filename), attachmentID.String()))
}

// FileKeyForPersonality constructs the S3 object key for a personality-uploaded file.
// Format: users/{userID}/personalities/{personalityID}/{attachmentID_filename}
func FileKeyForPersonality(userID, personalityID, attachmentID uuid.UUID, filename string) string {
	return path.Join("users", userID.String(), "personalities", personalityID.String(), filenameWithFallback(sanitizeFilename(filename), attachmentID.String()))
}

// FileKeyFallback is used when the file context (chat or personality) is not available.
// Format: users/{userID}/fallback/{attachmentID_filename}
func FileKeyFallback(userID, attachmentID uuid.UUID, filename string) string {
	return path.Join("users", userID.String(), filenameWithFallback(sanitizeFilename(filename), attachmentID.String()))
}

// FileKeyForImage constructs the S3 object key for the canonical image store used
// by the gallery and Claude vision. Uploaded and generated images both land here.
// Format: users/{userID}/images/{attachmentID_filename}
func FileKeyForImage(userID, attachmentID uuid.UUID, filename string) string {
	return path.Join("users", userID.String(), "images", filenameWithFallback(sanitizeFilename(filename), attachmentID.String()))
}

// FileKeyForAttachment resolves the canonical object key for an attachment based
// on MIME type and scope. It falls back to a user-scoped key when scope is absent.
func FileKeyForAttachment(userID, attachmentID uuid.UUID, filename, fileType string, chatID, personalityID *uuid.UUID) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(fileType)), "image/") {
		return FileKeyForImage(userID, attachmentID, filename)
	}
	if chatID != nil && *chatID != uuid.Nil {
		return FileKeyForChat(userID, *chatID, attachmentID, filename)
	}
	if personalityID != nil && *personalityID != uuid.Nil {
		return FileKeyForPersonality(userID, *personalityID, attachmentID, filename)
	}
	return FileKeyFallback(userID, attachmentID, filename)
}

// FileKeyForImageThumbnail constructs the S3 object key for a JPEG thumbnail.
// Thumbnails are always stored as JPEG regardless of the original format.
// Format: users/{userID}/images/thumbs/{attachmentID}.jpg
func FileKeyForImageThumbnail(userID, attachmentID uuid.UUID) string {
	return path.Join("users", userID.String(), "images", "thumbs", attachmentID.String()+".jpg")
}

func sanitizeFilename(filename string) string {
	if filename == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(filename, "\\", "/")
	cleaned = path.Clean(cleaned)
	cleaned = path.Base(cleaned)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return ""
	}
	cleaned = strings.ReplaceAll(cleaned, "/", "_")
	cleaned = strings.ReplaceAll(cleaned, "\\", "_")
	return cleaned
}

// isS3ObjectMissing reports whether err is a missing-object response from S3.
// Without s3:ListBucket on the bucket, missing keys often surface as AccessDenied
// instead of NoSuchKey; callers treat all of these as "not found".
func isS3ObjectMissing(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	// AWS SDK v2 OperationError: unwrap and check API error code.
	var opErr interface {
		Error() string
		Unwrap() error
	}
	if errors.As(err, &opErr) {
		if isS3ObjectMissing(opErr.Unwrap()) {
			return true
		}
	}
	type apiError interface {
		ErrorCode() string
	}
	var apiErr apiError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}

func filenameWithFallback(filename, fallback string) string {
	if sanitized := sanitizeFilename(filename); sanitized != "" {
		return fallback + "_" + sanitized
	}
	return fallback
}

// ── S3 implementation ─────────────────────────────────────────────────────────

// s3FileStore uploads/downloads files to AWS S3 using the SDK v2 default credential chain.
type s3FileStore struct {
	client     *s3.Client
	bucketName string
	logger     *zap.Logger
}

func (s *s3FileStore) UploadFile(ctx context.Context, key string, content []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucketName),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 PutObject s3://%s/%s: %w", s.bucketName, key, err)
	}
	s.logger.Debug("archived file to S3", zap.String("key", key))
	return nil
}

func (s *s3FileStore) DownloadFile(ctx context.Context, key string) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3ObjectMissing(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("s3 GetObject s3://%s/%s: %w", s.bucketName, key, err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 read body s3://%s/%s: %w", s.bucketName, key, err)
	}
	return data, nil
}

func (s *s3FileStore) DeleteFile(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3ObjectMissing(err) {
			return nil
		}
		return fmt.Errorf("s3 DeleteObject s3://%s/%s: %w", s.bucketName, key, err)
	}
	s.logger.Debug("deleted file from S3", zap.String("key", key))
	return nil
}

// ── Local file store (dev/test fallback) ──────────────────────────────────────

// localFileStore persists files to the local filesystem. Used when S3_FILE_BUCKET
// is not configured. Not suitable for multi-instance deployments.
type localFileStore struct {
	dataDir string
	logger  *zap.Logger
}

func (l *localFileStore) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	fullPath := filepath.Join(l.dataDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return fmt.Errorf("local file store mkdir for %s: %w", key, err)
	}
	if err := os.WriteFile(fullPath, content, 0o600); err != nil {
		return fmt.Errorf("local file store write %s: %w", key, err)
	}
	l.logger.Debug("archived file to local store", zap.String("key", key), zap.String("path", fullPath))
	return nil
}

func (l *localFileStore) DownloadFile(_ context.Context, key string) ([]byte, error) {
	fullPath := filepath.Join(l.dataDir, filepath.FromSlash(key))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("local file store read %s: %w", key, err)
	}
	return data, nil
}

func (l *localFileStore) DeleteFile(_ context.Context, key string) error {
	fullPath := filepath.Join(l.dataDir, filepath.FromSlash(key))
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("local file store delete %s: %w", key, err)
	}
	l.logger.Debug("deleted file from local store", zap.String("key", key), zap.String("path", fullPath))
	return nil
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewFileStore returns an S3-backed FileStore when bucketName is non-empty, or a
// local filesystem store otherwise (local dev / CI). The local store writes to
// LOCAL_FILE_STORE_DIR when set, defaulting to $TMPDIR/chat-app-files.
func NewFileStore(ctx context.Context, bucketName, region string, logger *zap.Logger) (FileStore, error) {
	if bucketName == "" {
		dataDir := os.Getenv("LOCAL_FILE_STORE_DIR")
		if dataDir == "" {
			dataDir = filepath.Join(os.TempDir(), "chat-app-files")
		}
		logger.Info("S3_FILE_BUCKET not set — using local file store (dev only)",
			zap.String("data_dir", dataDir))
		return &localFileStore{dataDir: dataDir, logger: logger}, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for S3 file store: %w", err)
	}

	return &s3FileStore{
		client:     s3.NewFromConfig(cfg),
		bucketName: bucketName,
		logger:     logger,
	}, nil
}
