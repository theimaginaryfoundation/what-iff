package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Keep the cause available to errors.Is/As without printing paths, endpoints,
// response bodies or malformed values when a live test logs the error.
type connectorDiagnostic struct {
	summary string
	cause   error
}

func (e *connectorDiagnostic) Error() string { return e.summary }
func (e *connectorDiagnostic) Unwrap() error { return e.cause }

var connectorHTTPStatus = regexp.MustCompile(`^mcp server returned status ([1-5][0-9]{2}) for `)

func connectorFailureCategory(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "request timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "request cancelled"
	}
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		if requestErr.Timeout() {
			return "request timed out"
		}
		return "network request failed"
	}
	message := err.Error()
	if match := connectorHTTPStatus.FindStringSubmatch(message); len(match) > 0 {
		return "HTTP " + match[1]
	}
	if strings.HasPrefix(message, "mcp rpc ") {
		return "JSON-RPC error"
	}
	if strings.Contains(message, "context deadline exceeded") {
		return "request timed out"
	}
	if strings.Contains(message, "context canceled") {
		return "request cancelled"
	}
	if strings.HasPrefix(message, `Post "`) {
		return "network request failed"
	}
	return "connector error"
}

func connectorRequestFailure(phase string, cause error) error {
	return &connectorDiagnostic{summary: fmt.Sprintf("%s: transport or protocol failure (%s)", phase, connectorFailureCategory(cause)), cause: cause}
}

func connectorDiscoveryFailure(result DiscoveryResult, cause error) error {
	categories := map[string]struct{}{}
	for _, detail := range result.Errors {
		categories[connectorFailureCategory(errors.New(detail))] = struct{}{}
	}
	if len(categories) == 0 {
		categories[connectorFailureCategory(cause)] = struct{}{}
	}
	var names []string
	for category := range categories {
		names = append(names, category)
	}
	sort.Strings(names)
	return &connectorDiagnostic{summary: "discovery failed (" + strings.Join(names, ", ") + ")", cause: cause}
}

func connectorPlanFailure(cause error) error {
	var syntax *json.SyntaxError
	var mismatch *json.UnmarshalTypeError
	var fileError *os.PathError
	summary := "invalid connector scenario JSON"
	switch {
	case errors.Is(cause, io.EOF):
		summary += ": empty input"
	case errors.Is(cause, io.ErrUnexpectedEOF):
		summary += ": unexpected end of input"
	case errors.As(cause, &syntax):
		summary = fmt.Sprintf("invalid connector scenario JSON: syntax error at byte %d", syntax.Offset)
	case errors.As(cause, &mismatch):
		summary = fmt.Sprintf("invalid connector scenario JSON: expected %s at byte %d", mismatch.Type, mismatch.Offset)
	case errors.As(cause, &fileError):
		summary = "could not read MCP_SMOKE_PLAN"
		if errors.Is(cause, os.ErrNotExist) {
			summary += ": file not found"
		}
		if errors.Is(cause, os.ErrPermission) {
			summary += ": permission denied"
		}
	case strings.HasPrefix(cause.Error(), "json: unknown field "):
		summary += ": unknown field; check the documented plan keys"
	}
	return &connectorDiagnostic{summary: summary, cause: cause}
}
