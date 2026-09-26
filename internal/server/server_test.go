package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry/telemetrytest"
)

func TestMetricsMiddlewareRecordsRouteTemplateAndStatusClass(t *testing.T) {
	t.Parallel()
	tm := telemetrytest.New(t)
	s := &Server{router: mux.NewRouter(), telemetry: &telemetry.Telemetry{Metrics: tm.Metrics}}
	s.router.Use(s.metricsMiddleware)
	s.router.HandleFunc("/chat/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}).Methods(http.MethodGet)

	s.router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/chat/123", nil))

	require.Equal(t, uint64(1), tm.HistogramCount(t, telemetry.HTTPServerDuration.Name,
		telemetry.AttrHTTPMethod.String(http.MethodGet),
		telemetry.AttrHTTPRoute.String("/chat/{id}"),
		telemetry.AttrHTTPStatusClass.String("4xx"),
	))
}

func TestNormalizeMetricRoutePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"/api/foo", "/api/foo"},
		{"api/foo", "/api/foo"},
		{"/api//v1//x", "/api/v1/x"},
		{"  /x/y  ", "/x/y"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, normalizeMetricRoutePattern(tt.in))
		})
	}
}

func TestMetricHTTPRoute_matchedUsesTemplate(t *testing.T) {
	t.Parallel()

	r := mux.NewRouter()
	var got string
	r.HandleFunc("/api/widgets/{id}", func(w http.ResponseWriter, req *http.Request) {
		got = metricHTTPRoute(req)
	}).Methods(http.MethodGet)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/widgets/abc", nil))
	assert.Equal(t, "/api/widgets/{id}", got)
}

func TestMetricHTTPRoute_unmatched(t *testing.T) {
	t.Parallel()

	r := mux.NewRouter()
	var got string
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = metricHTTPRoute(req)
	})
	r.HandleFunc("/ok", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no-such-path", nil))
	assert.Equal(t, metricRouteUnmatched, got)
}
