package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestIsS3ObjectMissing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"no such key", &types.NoSuchKey{}, true},
		{"not found", &types.NotFound{}, true},
		{"wrapped no such key", fmt.Errorf("get: %w", &types.NoSuchKey{}), true},
		{"other", errors.New("timeout"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isS3ObjectMissing(tc.err); got != tc.want {
				t.Fatalf("isS3ObjectMissing() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "report.pdf", "report.pdf"},
		{"nested path", "docs/../hidden.txt", "hidden.txt"},
		{"with dots", "../../etc/passwd", "passwd"},
		{"with spaces", "  filename.txt  ", "filename.txt"},
		{"empty string", "", ""},
		{"dot only", ".", ""},
		{"parent dir", "..", ""},
		{"mixed separators", "dir\\sub\\file.txt", "file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.input); got != tt.expected {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFilenameWithFallback(t *testing.T) {
	fallback := uuid.NewString()
	if got := filenameWithFallback("", fallback); got != fallback {
		t.Fatalf("filenameWithFallback(empty) = %q, want fallback %q", got, fallback)
	}

	if got := filenameWithFallback("..", fallback); got != fallback {
		t.Fatalf("filenameWithFallback(\"..\") = %q, want fallback %q", got, fallback)
	}

	if got := filenameWithFallback("good-name.txt", fallback); got != fallback+"_good-name.txt" {
		t.Fatalf("filenameWithFallback(\"good-name.txt\") = %q, want [fallback]_good-name.txt", got)
	}
}

// TestLocalFileStoreUploadFileWritesOwnerOnlyPermissions asserts POSIX
// permission bits; localFileStore is a dev/single-instance fallback and the
// project only builds and deploys linux/amd64 (see `make build`), so this is
// not expected to run on a platform where those bits are merely advisory.
func TestLocalFileStoreUploadFileWritesOwnerOnlyPermissions(t *testing.T) {
	dataDir := t.TempDir()
	store := &localFileStore{dataDir: dataDir, logger: zap.NewNop()}

	key := "users/some-user/file.txt"
	if err := store.UploadFile(context.Background(), key, []byte("content"), "text/plain"); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	fullPath := filepath.Join(dataDir, filepath.FromSlash(key))
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(fullPath))
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", got)
	}
}
