package models

import "net/http"

// ErrorResponse represents an API error response
type ErrorResponse struct {
	// Error is DEPRECATED and duplicates Message. It is retained only so existing
	// clients that read it keep working; new clients should read Message and Code.
	// See ADR 0x020 — it is removed once nothing reads it.
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	// Code is a stable, machine-readable error identifier (e.g. "quota_exceeded").
	// It is always populated — no omitempty, so a call site that fails to set one
	// surfaces as an empty string rather than silently vanishing from the payload.
	Code string `json:"code"`
	// Details contains optional structured error metadata for clients.
	Details any `json:"details,omitempty"`
}

// Error codes come in two tiers, per ADR 0x020.
//
// Specific codes name a single failure condition and are a contract: once
// published they are not renamed or repurposed, and clients may branch on them.
//
// Generic codes are derived from the HTTP status and are the default for call
// sites that have not been given a specific one. They are NOT stable branch
// targets — a call site may narrow from ErrCodeBadRequest to a specific code at
// any time, and that is not a breaking change. Clients must not branch on them.
const (
	// Generic — derived from status, not a stable branch target.
	ErrCodeBadRequest          = "bad_request"
	ErrCodeUnauthorized        = "unauthorized"
	ErrCodeForbidden           = "forbidden"
	ErrCodeNotFound            = "not_found"
	ErrCodeMethodNotAllowed    = "method_not_allowed"
	ErrCodeConflict            = "conflict"
	ErrCodePayloadTooLarge     = "payload_too_large"
	ErrCodeUnprocessableEntity = "unprocessable_entity"
	ErrCodeTooManyRequests     = "too_many_requests"
	ErrCodeInternalError       = "internal_error"
	ErrCodeBadGateway          = "bad_gateway"
	ErrCodeServiceUnavailable  = "service_unavailable"
	ErrCodeGatewayTimeout      = "gateway_timeout"

	// Fallbacks for statuses without a specific mapping above.
	ErrCodeClientError = "client_error"
	ErrCodeServerError = "server_error"

	// Specific — stable contracts clients may branch on. These three predate
	// this ADR and keep their original spelling. The handler packages still
	// define their own copies; those collapse onto these in phase 2.
	ErrCodeMessageTooLong      = "message_too_long"
	ErrCodeQuotaExceeded       = "quota_exceeded"
	ErrCodeSystemPromptTooLong = "system_prompt_too_long"
)

// AllErrorCodes is every code this API can emit. It is the source the
// ErrorCode enum in openapi.yaml mirrors; TestErrorCodeEnumMatchesSpec fails if
// the two drift apart, so adding a code here without adding it to the spec (or
// the reverse) is caught rather than discovered by a client.
var AllErrorCodes = []string{
	// Generic — derived from status, not stable branch targets.
	ErrCodeBadRequest,
	ErrCodeUnauthorized,
	ErrCodeForbidden,
	ErrCodeNotFound,
	ErrCodeMethodNotAllowed,
	ErrCodeConflict,
	ErrCodePayloadTooLarge,
	ErrCodeUnprocessableEntity,
	ErrCodeTooManyRequests,
	ErrCodeInternalError,
	ErrCodeBadGateway,
	ErrCodeServiceUnavailable,
	ErrCodeGatewayTimeout,
	ErrCodeClientError,
	ErrCodeServerError,

	// Specific — stable contracts clients may branch on.
	ErrCodeMessageTooLong,
	ErrCodeQuotaExceeded,
	ErrCodeSystemPromptTooLong,
}

// genericErrorCodes maps HTTP statuses to their generic code.
var genericErrorCodes = map[int]string{
	http.StatusBadRequest:            ErrCodeBadRequest,
	http.StatusUnauthorized:          ErrCodeUnauthorized,
	http.StatusForbidden:             ErrCodeForbidden,
	http.StatusNotFound:              ErrCodeNotFound,
	http.StatusMethodNotAllowed:      ErrCodeMethodNotAllowed,
	http.StatusConflict:              ErrCodeConflict,
	http.StatusRequestEntityTooLarge: ErrCodePayloadTooLarge,
	http.StatusUnprocessableEntity:   ErrCodeUnprocessableEntity,
	http.StatusTooManyRequests:       ErrCodeTooManyRequests,
	http.StatusInternalServerError:   ErrCodeInternalError,
	http.StatusBadGateway:            ErrCodeBadGateway,
	http.StatusServiceUnavailable:    ErrCodeServiceUnavailable,
	http.StatusGatewayTimeout:        ErrCodeGatewayTimeout,
}

// GenericErrorCode returns the generic error code for an HTTP status. Statuses
// without an explicit mapping fall back to their class so that every error
// response carries a code; a non-error status yields ErrCodeInternalError, since
// reaching the error path with one is itself a bug.
func GenericErrorCode(statusCode int) string {
	if code, ok := genericErrorCodes[statusCode]; ok {
		return code
	}
	switch {
	case statusCode >= 500:
		return ErrCodeServerError
	case statusCode >= 400:
		return ErrCodeClientError
	default:
		return ErrCodeInternalError
	}
}
