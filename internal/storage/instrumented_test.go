package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
	"go.uber.org/zap"
)

// Not parallel: records through telemetry.Global().
func TestInstrumentLocalStore(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	ctx := context.Background()
	fs := Instrument(&localFileStore{dataDir: t.TempDir(), logger: zap.NewNop()})

	require.NoError(t, fs.UploadFile(ctx, "users/u/a.txt", []byte("hi"), "text/plain"))
	data, err := fs.DownloadFile(ctx, "users/u/a.txt")
	require.NoError(t, err)
	require.Equal(t, "hi", string(data))
	// Missing objects keep the (nil, nil) contract and count as successful calls.
	data, err = fs.DownloadFile(ctx, "users/u/missing.txt")
	require.NoError(t, err)
	require.Nil(t, data)
	require.NoError(t, fs.DeleteFile(ctx, "users/u/a.txt"))

	// Optional interfaces survive wrapping.
	lister, ok := fs.(ObjectLister)
	require.True(t, ok)
	_, err = lister.ListObjects(ctx, "users/u")
	require.NoError(t, err)
	signer, ok := fs.(Presigner)
	require.True(t, ok)
	_, err = signer.PresignGetObject(ctx, "users/u/a.txt", time.Minute)
	require.NoError(t, err)

	name := telemetry.DependencyDuration.Name
	dep := telemetry.AttrDependency.String(telemetry.DependencyLocalFS)
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, dep, telemetry.AttrOperation.String("put_object")))
	require.Equal(t, uint64(2), tm.HistogramCount(t, name, dep, telemetry.AttrOperation.String("get_object")))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, dep, telemetry.AttrOperation.String("delete_object")))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, dep, telemetry.AttrOperation.String("list_objects")))
	require.Empty(t, tm.AttributeValues(t, name, telemetry.AttrErrorType))
	require.Equal(t, fs, Instrument(fs), "wrapping twice is a no-op")
}

type plainStore struct{ err error }

func (p plainStore) UploadFile(context.Context, string, []byte, string) error { return p.err }
func (p plainStore) DownloadFile(context.Context, string) ([]byte, error)     { return nil, p.err }
func (p plainStore) DeleteFile(context.Context, string) error                 { return p.err }

func TestInstrumentPlainStoreFailure(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	fs := Instrument(plainStore{err: context.DeadlineExceeded})
	_, isLister := fs.(ObjectLister)
	_, isSigner := fs.(Presigner)
	require.False(t, isLister, "wrapping must not add optional interfaces")
	require.False(t, isSigner)

	err := fs.UploadFile(context.Background(), "k", nil, "")
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.DependencyDuration.Name,
		telemetry.AttrDependency.String(telemetry.DependencyOther),
		telemetry.AttrOperation.String("put_object"),
		telemetry.AttrErrorType.String(telemetry.ErrorTypeTimeout)))
}
