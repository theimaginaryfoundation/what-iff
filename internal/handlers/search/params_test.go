package search

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSearchParams_QueryValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "missing query", raw: "/search", wantErr: "at least 2 characters"},
		{name: "single char", raw: "/search?query=a", wantErr: "at least 2 characters"},
		{name: "whitespace only", raw: "/search?query=%20%20", wantErr: "at least 2 characters"},
		{name: "too long", raw: "/search?query=" + strings.Repeat("a", maxQueryRunes+1), wantErr: "exceeds maximum length"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.raw, nil)
			_, _, _, err := parseSearchParams(req)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestParseSearchParams_DefaultsAndCanonicalOrdering(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/search?query=atlas", nil)
	q, types, limit, err := parseSearchParams(req)
	require.NoError(t, err)
	require.Equal(t, "atlas", q)
	require.Equal(t, AllTypes, types)
	require.Equal(t, defaultLimitPerTyp, limit)

	// Submitting a permuted CSV should still emit canonical order.
	req = httptest.NewRequest(http.MethodGet, "/search?query=atlas&types=memory,chat,IMAGE", nil)
	_, types, _, err = parseSearchParams(req)
	require.NoError(t, err)
	require.Equal(t, []string{TypeChat, TypeMemory, TypeImage}, types)
}

func TestParseSearchParams_TypeValidation(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/search?query=atlas&types=chat,unknown", nil)
	_, _, _, err := parseSearchParams(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")

	// CSV of only commas reports "must include at least one value".
	req = httptest.NewRequest(http.MethodGet, "/search?query=atlas&types=,,,", nil)
	_, _, _, err = parseSearchParams(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one")
}

func TestParseSearchParams_LimitValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "non integer", raw: "/search?query=atlas&limit_per_type=abc", wantErr: "integer"},
		{name: "below min", raw: "/search?query=atlas&limit_per_type=0", wantErr: "between 1 and 25"},
		{name: "above max", raw: "/search?query=atlas&limit_per_type=26", wantErr: "between 1 and 25"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.raw, nil)
			_, _, _, err := parseSearchParams(req)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/search?query=atlas&limit_per_type=10", nil)
	_, _, limit, err := parseSearchParams(req)
	require.NoError(t, err)
	require.Equal(t, 10, limit)
}
