package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

func TestHTTPStatusClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code int
		want string
	}{
		{0, "other"},
		{99, "other"},
		{100, "1xx"},
		{199, "1xx"},
		{200, "2xx"},
		{204, "2xx"},
		{299, "2xx"},
		{301, "3xx"},
		{399, "3xx"},
		{400, "4xx"},
		{404, "4xx"},
		{499, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{599, "5xx"},
		{600, "other"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.code), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, httpStatusClass(tt.code))
		})
	}
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
