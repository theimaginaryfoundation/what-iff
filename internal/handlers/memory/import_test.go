package memory

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"go.uber.org/zap"
)

func TestImportMemoriesUnavailableWhenOpenAIKeyMissing(t *testing.T) {
	handler := NewHandler(nil, zap.NewNop(), " ", nil)
	req := requestWithUser(t, "not multipart", "multipart/form-data; boundary=test")
	rec := httptest.NewRecorder()

	handler.ImportMemories(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "Memory import is not configured")
}

func TestImportMemoriesRejectsOversizedMultipartAtHTTPBody(t *testing.T) {
	handler := NewHandler(nil, zap.NewNop(), "test-key", nil)
	body := io.MultiReader(
		strings.NewReader("--test\r\nContent-Disposition: form-data; name=\"file\"; filename=\"import.zip\"\r\nContent-Type: application/zip\r\n\r\n"),
		io.LimitReader(repeatingByteReader('a'), maxMemoryImportBytes+1),
		strings.NewReader("\r\n--test--\r\n"),
	)
	req := requestWithUser(t, body, "multipart/form-data; boundary=test")
	rec := httptest.NewRecorder()

	handler.ImportMemories(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Invalid multipart form")
}

func requestWithUser(t *testing.T, body any, contentType string) *http.Request {
	t.Helper()

	reader, ok := body.(io.Reader)
	if !ok {
		reader = strings.NewReader(body.(string))
	}
	req := httptest.NewRequest(http.MethodPost, "/memory/import", reader)
	req.Header.Set("Content-Type", contentType)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	return req.WithContext(ctx)
}

type repeatingByteReader byte

func (r repeatingByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}
