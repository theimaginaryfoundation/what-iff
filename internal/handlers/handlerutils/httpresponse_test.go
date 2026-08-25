package handlerutils

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) models.ErrorResponse {
	t.Helper()
	var body models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// The raw error is the reason this helper changed: it routinely carries driver
// and constraint text, and it used to be written straight into the response.
func TestRespondWithError_DoesNotLeakRawError(t *testing.T) {
	rec := httptest.NewRecorder()
	raw := errors.New(`pq: duplicate key value violates unique constraint "users_email_key"`)

	RespondWithError(rec, zap.NewNop(), http.StatusConflict, CodeNotSet,
		"That email is already registered", raw)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.NotContains(t, rec.Body.String(), "users_email_key")
	require.NotContains(t, rec.Body.String(), "pq:")

	body := decodeErrorBody(t, rec)
	require.Equal(t, "That email is already registered", body.Message)
	require.Equal(t, "That email is already registered", body.Error)
	require.Equal(t, models.ErrCodeConflict, body.Code)
}

// CodeNotSet exists so a call site never restates what the status already
// says. Deriving has to cover every status, including ones with no explicit
// mapping, or the envelope would go out with an empty code.
func TestRespondWithError_CodeNotSetAlwaysDerivesACode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
		wantCode   string
	}{
		{"bad request", http.StatusBadRequest, nil, models.ErrCodeBadRequest},
		{"unauthorized", http.StatusUnauthorized, nil, models.ErrCodeUnauthorized},
		{"not found", http.StatusNotFound, errors.New("boom"), models.ErrCodeNotFound},
		{"internal", http.StatusInternalServerError, errors.New("boom"), models.ErrCodeInternalError},
		{"unmapped 4xx falls back to class", http.StatusTeapot, nil, models.ErrCodeClientError},
		{"unmapped 5xx falls back to class", http.StatusNotExtended, nil, models.ErrCodeServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondWithError(rec, zap.NewNop(), tt.statusCode, CodeNotSet,
				"something went wrong", tt.err)

			body := decodeErrorBody(t, rec)
			require.Equal(t, tt.wantCode, body.Code)
			require.NotEmpty(t, body.Code, "every error response must carry a code")
		})
	}
}

// A whitespace-only code means the same thing as CodeNotSet. Code has no
// omitempty, so without the trim it would be written to the wire as " " —
// present, unbranchable, and absent from the published enum.
func TestRespondWithError_BlankCodeIsTreatedAsNotSet(t *testing.T) {
	for _, code := range []string{"", " ", "  ", "\t", "\n", " \t\n "} {
		t.Run(strconv.Quote(code), func(t *testing.T) {
			rec := httptest.NewRecorder()
			RespondWithError(rec, zap.NewNop(), http.StatusNotFound, code, "Chat not found", nil)

			require.Equal(t, models.ErrCodeNotFound, decodeErrorBody(t, rec).Code)
		})
	}
}

func TestRespondWithError_SpecificCodeIsUsedVerbatim(t *testing.T) {
	rec := httptest.NewRecorder()

	RespondWithError(rec, zap.NewNop(), http.StatusTooManyRequests,
		models.ErrCodeQuotaExceeded, "You have reached your monthly limit", nil)

	body := decodeErrorBody(t, rec)
	require.Equal(t, models.ErrCodeQuotaExceeded, body.Code)
	require.Equal(t, "You have reached your monthly limit", body.Message)
}

// A specific code must survive even when it happens to disagree with the status
// the caller chose — deriving is opt-in, not a correction applied behind the
// caller's back.
func TestRespondWithError_SpecificCodeIsNotOverriddenByStatus(t *testing.T) {
	rec := httptest.NewRecorder()

	RespondWithError(rec, zap.NewNop(), http.StatusBadRequest,
		models.ErrCodeMessageTooLong, "Message is too long", nil)

	require.Equal(t, models.ErrCodeMessageTooLong, decodeErrorBody(t, rec).Code)
}

func TestRespondWithError_LeaksNothingWithASpecificCode(t *testing.T) {
	rec := httptest.NewRecorder()
	raw := errors.New("dial tcp 10.0.0.1:5432: connect: connection refused")

	RespondWithError(rec, zap.NewNop(), http.StatusServiceUnavailable,
		models.ErrCodeServiceUnavailable, "Service temporarily unavailable", raw)

	require.NotContains(t, rec.Body.String(), "10.0.0.1")
	require.Equal(t, "Service temporarily unavailable", decodeErrorBody(t, rec).Error)
}

// http.Error writes text/plain with no code, which is the shape ADR 0x020 moved
// the API off. There are no remaining legitimate uses: the three encode-failure
// fallbacks that used to justify an allow list are gone — two were duplicated
// respondWithJSON helpers that were deleted, and the third was a streaming
// encoder replaced by marshal-then-write.
//
// RespondWithJSON handles its own marshal failure by writing the envelope
// inline, so even that path does not need http.Error.
//
// An empty allow list is deliberate. A list with entries invites additions;
// this one has to be argued for.
func TestHTTPErrorIsNeverUsed(t *testing.T) {
	allowed := map[string]bool{}

	root := repoRoot(t)

	call := regexp.MustCompile(`\bhttp\.Error\(`)
	var offenders []string
	scanned := 0

	require.NoError(t, filepath.WalkDir(filepath.Join(root, "internal"),
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if allowed[filepath.ToSlash(rel)] {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for n, line := range strings.Split(string(body), "\n") {
				if call.MatchString(line) {
					offenders = append(offenders, filepath.ToSlash(rel)+":"+strconv.Itoa(n+1))
				}
			}
			return nil
		}))

	// The guard on the guard. A scan that reaches nothing reports no offenders
	// and passes, so the only thing separating "clean" from "never looked" is
	// this count. internal/ held 370 non-test .go files when this was written.
	require.Greater(t, scanned, 200,
		"scanned only %d Go files under %s — the scan root is wrong, so this "+
			"test is passing without checking anything", scanned, root)

	require.Empty(t, offenders,
		"http.Error bypasses the error envelope — use handlerutils.RespondWithError.\n"+
			"RespondWithJSON already handles its own marshal failure, so an encode-failure\n"+
			"fallback is not a reason to reach for http.Error.")
}

// repoRoot walks up from the working directory until it finds go.mod.
//
// The guards used to resolve the root as a fixed number of parent directories
// ("../../.."), which is coupled to how deep this file happens to sit. Moving
// the package would have pointed the walk at a directory that does not exist —
// and WalkDir over a missing root reports no offenders, so the guard would have
// gone on passing while checking nothing.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "reached the filesystem root without finding go.mod")
		dir = parent
	}
}

// RespondWithJSON has to survive a payload it cannot marshal. It cannot delegate
// to RespondWithError — that calls this function, so a failure would recurse —
// so it marshals the envelope itself. This pins that: an unmarshalable payload
// still produces a valid JSON error envelope, not a truncated or empty body.
func TestRespondWithJSON_UnmarshalablePayloadStillYieldsAnEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	// A channel cannot be marshalled; json.Marshal fails on the caller's value
	// while the encoder itself is perfectly healthy.
	RespondWithJSON(rec, zap.NewNop(), http.StatusOK, map[string]any{"bad": make(chan int)})

	require.Equal(t, http.StatusInternalServerError, rec.Code,
		"a failed marshal must not report the caller's intended status")
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	body := decodeErrorBody(t, rec)
	require.Equal(t, models.ErrCodeInternalError, body.Code)
	require.NotEmpty(t, body.Message)
	require.NotContains(t, rec.Body.String(), "chan",
		"the marshalling failure must not describe the caller's payload to a client")
}
