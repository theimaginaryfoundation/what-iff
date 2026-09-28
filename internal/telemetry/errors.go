package telemetry

import (
	"context"
	"errors"
	"net"
	"sync"
)

// Error types for the error.type attribute. A fixed set, so dashboards can group by it and one
// misbehaving model or dependency shows up as a shift between types.
const (
	ErrorTypeCanceled    = "canceled"
	ErrorTypeTimeout     = "timeout"
	ErrorTypeRateLimited = "rate_limited"
	ErrorTypeServer      = "server_error"
	ErrorTypeClient      = "client_error"
	ErrorTypeAuth        = "auth"
	ErrorTypeNotFound    = "not_found"
	ErrorTypeNetwork     = "network"
	ErrorTypeOther       = "other"
)

// StatusCodeFunc extracts an HTTP status code from an SDK error type this package can't import
// (e.g. *openai.Error). It returns false when err isn't that type.
type StatusCodeFunc func(err error) (int, bool)

var (
	statusCodeFuncsMu sync.RWMutex
	statusCodeFuncs   []StatusCodeFunc
)

// RegisterStatusCodeFunc teaches ClassifyError about an SDK's error type. Call from an init
// function in the package that owns the SDK.
func RegisterStatusCodeFunc(f StatusCodeFunc) {
	statusCodeFuncsMu.Lock()
	defer statusCodeFuncsMu.Unlock()
	statusCodeFuncs = append(statusCodeFuncs, f)
}

// httpStatusCoder matches errors that report their status, such as AWS smithy response errors.
type httpStatusCoder interface{ HTTPStatusCode() int }

// ClassifyError maps err to an error type, or "" for nil. Context errors win over status codes,
// because a cancelled request often surfaces as a wrapped transport error.
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return ErrorTypeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorTypeTimeout
	}
	if code, ok := statusCode(err); ok {
		if t := ClassifyHTTPStatus(code); t != "" {
			return t
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return ErrorTypeTimeout
		}
		return ErrorTypeNetwork
	}
	return ErrorTypeOther
}

// ClassifyHTTPStatus maps a response status to an error type, or "" for non-error statuses.
func ClassifyHTTPStatus(code int) string {
	switch {
	case code == 401 || code == 403:
		return ErrorTypeAuth
	case code == 404:
		return ErrorTypeNotFound
	case code == 408:
		return ErrorTypeTimeout
	case code == 429:
		return ErrorTypeRateLimited
	case code >= 500:
		// Includes Anthropic's 529 "overloaded".
		return ErrorTypeServer
	case code >= 400:
		return ErrorTypeClient
	default:
		return ""
	}
}

func statusCode(err error) (int, bool) {
	var coder httpStatusCoder
	if errors.As(err, &coder) {
		if code := coder.HTTPStatusCode(); code > 0 {
			return code, true
		}
	}
	statusCodeFuncsMu.RLock()
	defer statusCodeFuncsMu.RUnlock()
	for _, f := range statusCodeFuncs {
		if code, ok := f(err); ok && code > 0 {
			return code, true
		}
	}
	return 0, false
}

// HTTPStatusClass maps a numeric status to a coarse class (1xx–5xx), which keeps series counts
// far below per-code labels.
func HTTPStatusClass(code int) string {
	switch {
	case code >= 100 && code < 200:
		return "1xx"
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}
