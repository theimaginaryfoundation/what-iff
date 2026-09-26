package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

// Not parallel: records through telemetry.Global().
func TestParallelCallsRecordDependencyDuration(t *testing.T) {
	tm := telemetrytest.UseGlobal(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/extract" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	p := NewParallel("pk", "", srv.Client())
	p.baseURL = srv.URL

	_, err := p.Search(context.Background(), Query{Query: "x"})
	require.NoError(t, err)
	_, err = p.Extract(context.Background(), "https://example.com/page", "")
	require.EqualError(t, err, "parallel extract: HTTP 429: slow down")

	name := telemetry.DependencyDuration.Name
	dep := telemetry.AttrDependency.String(telemetry.DependencyParallel)
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, dep, telemetry.AttrOperation.String("search")))
	require.Equal(t, uint64(1), tm.HistogramCount(t, name, dep, telemetry.AttrOperation.String("extract"),
		telemetry.AttrErrorType.String(telemetry.ErrorTypeRateLimited)))
	require.Equal(t, []string{telemetry.ErrorTypeRateLimited}, tm.AttributeValues(t, name, telemetry.AttrErrorType))
}
