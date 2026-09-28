package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type statusErr struct{ code int }

func (e statusErr) Error() string       { return fmt.Sprintf("status %d", e.code) }
func (e statusErr) HTTPStatusCode() int { return e.code }

// sdkErr stands in for an SDK error type this package can't import (e.g. *openai.Error).
type sdkErr struct{ StatusCode int }

func (e *sdkErr) Error() string { return "sdk error" }

func init() {
	RegisterStatusCodeFunc(func(err error) (int, bool) {
		var e *sdkErr
		if errors.As(err, &e) {
			return e.StatusCode, true
		}
		return 0, false
	})
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassifyError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"canceled", fmt.Errorf("wrapped: %w", context.Canceled), ErrorTypeCanceled},
		{"deadline", context.DeadlineExceeded, ErrorTypeTimeout},
		{"canceled beats status", fmt.Errorf("%w: %w", context.Canceled, statusErr{500}), ErrorTypeCanceled},
		{"rate limited", statusErr{429}, ErrorTypeRateLimited},
		{"overloaded", statusErr{529}, ErrorTypeServer},
		{"bad gateway", statusErr{502}, ErrorTypeServer},
		{"auth", statusErr{401}, ErrorTypeAuth},
		{"forbidden", statusErr{403}, ErrorTypeAuth},
		{"not found", statusErr{404}, ErrorTypeNotFound},
		{"bad request", statusErr{400}, ErrorTypeClient},
		{"registered sdk error", fmt.Errorf("call: %w", &sdkErr{StatusCode: 429}), ErrorTypeRateLimited},
		{"net timeout", &net.OpError{Op: "dial", Err: timeoutErr{}}, ErrorTypeTimeout},
		{"net error", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, ErrorTypeNetwork},
		{"unknown", errors.New("boom"), ErrorTypeOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ClassifyError(tt.err))
		})
	}
}

func TestHTTPStatusClass(t *testing.T) {
	t.Parallel()
	for code, want := range map[int]string{0: "other", 101: "1xx", 200: "2xx", 304: "3xx", 404: "4xx", 503: "5xx", 600: "other"} {
		require.Equal(t, want, HTTPStatusClass(code), code)
	}
}

func TestFileKind(t *testing.T) {
	t.Parallel()
	for mime, want := range map[string]string{
		"image/png": "image", "IMAGE/JPEG": "image", "application/pdf": "pdf", "audio/mpeg": "audio",
		"text/markdown": "text", "application/json": "text", "application/zip": "other", "": "other",
	} {
		require.Equal(t, want, FileKind(mime), mime)
	}
}
