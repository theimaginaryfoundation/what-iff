package handlerutils

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"go.uber.org/zap"
)

// RefreshResponseWriteDeadline resets the HTTP response write deadline. Handlers that call
// SetWriteDeadline at the start of the request must refresh before writing the JSON body after
// long-running work; otherwise the deadline expires during the slow phase and RespondWithJSON fails.
func RefreshResponseWriteDeadline(w http.ResponseWriter, d time.Duration) {
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Now().Add(d))
	}
}

// RespondWithHTML responds with an HTML payload
func RespondWithHTML(w http.ResponseWriter, logger *zap.Logger, code int, payload string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, err := w.Write([]byte(payload))
	if err != nil {
		logger.Error("failed to write html response", zap.Error(err))
	}
}

const (
	// internalErrorMessage is what a client sees when the failure is ours and
	// there is nothing actionable to say.
	internalErrorMessage = "Something went wrong. Please try again"
	// internalErrorJSON is the last-resort body, used only if marshalling the
	// envelope itself somehow fails. Hand-written so it needs no encoder.
	internalErrorJSON = `{"error":"Something went wrong. Please try again",` +
		`"message":"Something went wrong. Please try again","code":"internal_error"}`
)

// RespondWithJSON responds with a JSON payload
func RespondWithJSON(w http.ResponseWriter, logger *zap.Logger, statusCode int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal json response", zap.Error(err))
		// The encoder is not broken — one caller-supplied value was unmarshalable
		// (a channel, a func, a cycle). So the envelope can still be sent; it just
		// cannot go through RespondWithError, which calls this function and would
		// recurse. Marshal it here instead.
		statusCode = http.StatusInternalServerError
		response, err = json.Marshal(models.ErrorResponse{
			Error:   internalErrorMessage,
			Message: internalErrorMessage,
			Code:    models.ErrCodeInternalError,
		})
		if err != nil {
			// Unreachable: a struct of three strings has no unmarshalable state.
			// The literal exists so the write below can never see a nil body.
			logger.Error("failed to marshal the error envelope itself", zap.Error(err))
			response = []byte(internalErrorJSON)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	// http.Error sets this for us; hand-written JSON responses have to set it
	// themselves. Without it, callers converted away from http.Error would
	// quietly lose the header they used to get for free.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(statusCode)
	_, err = w.Write(response)
	if err != nil {
		logger.Error("failed to write json response", zap.Error(err))
	}
}

// CodeNotSet marks a call site that has not been given a specific error code
// yet. It is a placeholder state, not an instruction: RespondWithError fills in
// the generic code for the HTTP status when it sees one.
//
// Passing it rather than the matching generic constant keeps one source of
// truth. Writing models.ErrCodeNotFound beside http.StatusNotFound duplicates
// information already in the call, and the two drift apart the first time
// somebody changes the status and forgets the code.
//
// It lives here rather than in the models taxonomy because it is never a value
// that reaches a client. Counting its occurrences measures how much of ADR
// 0x020's phase 2 is left.
const CodeNotSet = ""

// RespondWithError writes the error envelope and logs the failure.
//
// code is either a specific code from the models taxonomy, which clients may
// branch on, or CodeNotSet for the generic code matching statusCode.
//
// err is deliberately logged and never placed in the response: it routinely
// carries driver and constraint text that should not reach a client. The
// response body says only what the handler chose to say via message.
func RespondWithError(w http.ResponseWriter, logger *zap.Logger, statusCode int, code, message string, err error) {
	// Trimmed rather than compared to CodeNotSet directly: a caller passing " "
	// or a stray tab means the same thing as passing nothing, and Code has no
	// omitempty, so letting whitespace through would put it on the wire.
	if strings.TrimSpace(code) == "" {
		code = models.GenericErrorCode(statusCode)
	}
	if err != nil {
		logger.Error("API error",
			zap.String("message", message),
			zap.String("code", code),
			zap.Int("status_code", statusCode),
			zap.Error(err))
	} else {
		logger.Info("API error",
			zap.String("message", message),
			zap.String("code", code),
			zap.Int("status_code", statusCode))
	}

	response := models.ErrorResponse{
		// Error duplicates Message and is deprecated; see models.ErrorResponse.
		Error:   message,
		Message: message,
		Code:    code,
	}

	RespondWithJSON(w, logger, statusCode, response)
}

// RespondWithNoContent responds with an HTTP 204 no content
func RespondWithNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
