package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const maxAccountExportFileObjects = 10_000

// ObjectInfo describes a stored object for the account-export files manifest. It carries no bytes —
// the export lists a user's files and presigns them per-object rather than repackaging.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	LastModified time.Time `json:"last_modified"`
}

// ObjectLister lists objects under a key prefix. Implemented by the S3 and local stores; consumers
// type-assert for it so the core FileStore interface (and its mocks) stay unchanged.
type ObjectLister interface {
	ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error)
}

// Presigner mints a time-limited GET URL for a single object.
type Presigner interface {
	PresignGetObject(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// ── S3 implementation ──────────────────────────────────────────────────────────

// ListObjects enumerates objects under prefix using the V2 paginator. The account-export inventory is
// deliberately bounded so an unexpectedly large file collection cannot exhaust API-task memory.
// ETag is returned unquoted for stable manifest checksums.
func (s *s3FileStore) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	var out []ObjectInfo
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucketName),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 ListObjectsV2 s3://%s/%s: %w", s.bucketName, prefix, err)
		}
		for _, obj := range page.Contents {
			if len(out) >= maxAccountExportFileObjects {
				return nil, fmt.Errorf("s3 object listing s3://%s/%s exceeds %d objects", s.bucketName, prefix, maxAccountExportFileObjects)
			}
			info := ObjectInfo{Key: aws.ToString(obj.Key)}
			if obj.Size != nil {
				info.Size = *obj.Size
			}
			if obj.ETag != nil {
				info.ETag = strings.Trim(*obj.ETag, "\"")
			}
			if obj.LastModified != nil {
				info.LastModified = *obj.LastModified
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// PresignGetObject returns a presigned GET URL for key valid for ttl.
func (s *s3FileStore) PresignGetObject(ctx context.Context, key string, ttl time.Duration) (string, error) {
	ps := s3.NewPresignClient(s.client)
	req, err := ps.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 presign s3://%s/%s: %w", s.bucketName, key, err)
	}
	return req.URL, nil
}

// ── Local implementation (dev/test) ─────────────────────────────────────────────

// ListObjects walks the local data dir under prefix, returning file:// keys relative to the store.
func (l *localFileStore) ListObjects(_ context.Context, prefix string) ([]ObjectInfo, error) {
	root := filepath.Join(l.dataDir, filepath.FromSlash(prefix))
	var out []ObjectInfo
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // nothing uploaded yet
			}
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if len(out) >= maxAccountExportFileObjects {
			return fmt.Errorf("local object listing %s exceeds %d objects", prefix, maxAccountExportFileObjects)
		}
		rel, relErr := filepath.Rel(l.dataDir, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, ObjectInfo{
			Key:          filepath.ToSlash(rel),
			Size:         fi.Size(),
			LastModified: fi.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PresignGetObject returns a file:// URL for local dev — there is no signing, but it keeps the worker
// path working end-to-end without S3.
func (l *localFileStore) PresignGetObject(_ context.Context, key string, _ time.Duration) (string, error) {
	abs, err := filepath.Abs(filepath.Join(l.dataDir, filepath.FromSlash(key)))
	if err != nil {
		return "", err
	}
	return "file://" + filepath.ToSlash(abs), nil
}
