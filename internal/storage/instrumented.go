package storage

import (
	"context"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// Operation names for the operation attribute on telemetry.DependencyDuration.
const (
	opPutObject    = "put_object"
	opGetObject    = "get_object"
	opDeleteObject = "delete_object"
	opListObjects  = "list_objects"
)

// Instrument wraps fs so every call records telemetry.DependencyDuration (through
// telemetry.Global()), with dependency "s3" or "local_fs" and operation put_object, get_object,
// delete_object or list_objects. Behaviour is unchanged: a missing object is still (nil, nil)
// from DownloadFile and counts as a successful call.
//
// The optional ObjectLister and Presigner interfaces are preserved, so callers' type
// assertions keep working. Presigning is local signing with no network call, so it is passed
// through untimed.
func Instrument(fs FileStore) FileStore {
	if fs == nil {
		return nil
	}
	if _, already := fs.(interface{ instrumented() }); already {
		return fs
	}
	base := instrumentedStore{inner: fs, dependency: dependencyOf(fs)}
	lister, isLister := fs.(ObjectLister)
	signer, isSigner := fs.(Presigner)
	switch {
	case isLister && isSigner:
		return instrumentedListerPresigner{instrumentedLister{base, lister}, signer}
	case isLister:
		return instrumentedLister{base, lister}
	case isSigner:
		return instrumentedPresigner{base, signer}
	default:
		return base
	}
}

func dependencyOf(fs FileStore) string {
	switch fs.(type) {
	case *s3FileStore:
		return telemetry.DependencyS3
	case *localFileStore:
		return telemetry.DependencyLocalFS
	default:
		return telemetry.DependencyOther
	}
}

type instrumentedStore struct {
	inner      FileStore
	dependency string
}

func (s instrumentedStore) instrumented() {}

func (s instrumentedStore) time(ctx context.Context, op string) func(error) {
	return telemetry.Global().TimeDependency(ctx, s.dependency, op)
}

func (s instrumentedStore) UploadFile(ctx context.Context, key string, content []byte, contentType string) error {
	done := s.time(ctx, opPutObject)
	err := s.inner.UploadFile(ctx, key, content, contentType)
	done(err)
	return err
}

func (s instrumentedStore) DownloadFile(ctx context.Context, key string) ([]byte, error) {
	done := s.time(ctx, opGetObject)
	data, err := s.inner.DownloadFile(ctx, key)
	done(err)
	return data, err
}

func (s instrumentedStore) DeleteFile(ctx context.Context, key string) error {
	done := s.time(ctx, opDeleteObject)
	err := s.inner.DeleteFile(ctx, key)
	done(err)
	return err
}

type instrumentedLister struct {
	instrumentedStore
	lister ObjectLister
}

func (s instrumentedLister) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	done := s.time(ctx, opListObjects)
	out, err := s.lister.ListObjects(ctx, prefix)
	done(err)
	return out, err
}

type instrumentedPresigner struct {
	instrumentedStore
	signer Presigner
}

func (s instrumentedPresigner) PresignGetObject(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return s.signer.PresignGetObject(ctx, key, ttl)
}

type instrumentedListerPresigner struct {
	instrumentedLister
	signer Presigner
}

func (s instrumentedListerPresigner) PresignGetObject(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return s.signer.PresignGetObject(ctx, key, ttl)
}
