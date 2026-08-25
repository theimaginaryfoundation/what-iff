package imagegallery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// fakeListStore captures the filters passed to ListFileAttachments so tests can
// assert that handler-level query parsing forwards them correctly.
type fakeListStore struct {
	Store
	gotFilters models.FileAttachmentFilters
}

func (f *fakeListStore) ListFileAttachments(_ context.Context, _ uuid.UUID, _, _ int, filters models.FileAttachmentFilters) (*models.PaginatedResponse, error) {
	f.gotFilters = filters
	return &models.PaginatedResponse{Results: []any{}, TotalCount: 0, Page: 1}, nil
}

func TestListImages_ForwardsNameFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		queryName string
		wantSet   bool
		wantValue string
	}{
		{name: "name omitted leaves filter unset", queryName: "", wantSet: false},
		{name: "whitespace-only name leaves filter unset", queryName: "   ", wantSet: false},
		{name: "name forwarded after trim", queryName: "  atlas  ", wantSet: true, wantValue: "atlas"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeListStore{}
			h := NewHandler(fake, zap.NewNop(), nil)

			target := "/image-gallery"
			if tc.queryName != "" {
				q := url.Values{}
				q.Set("name", tc.queryName)
				target = "/image-gallery?" + q.Encode()
			}
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))

			rr := httptest.NewRecorder()
			r := mux.NewRouter()
			h.RegisterRoutes(r)
			r.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			if tc.wantSet {
				require.NotNil(t, fake.gotFilters.Name)
				require.Equal(t, tc.wantValue, *fake.gotFilters.Name)
			} else {
				require.Nil(t, fake.gotFilters.Name)
			}

			require.NotNil(t, fake.gotFilters.FileType)
			require.Equal(t, models.ImageMIMEPrefix, *fake.gotFilters.FileType)
		})
	}
}

func TestListImages_ForwardsPersonalityFilter(t *testing.T) {
	t.Parallel()

	fake := &fakeListStore{}
	h := NewHandler(fake, zap.NewNop(), nil)
	personalityID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/image-gallery?personality_id="+personalityID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))

	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotNil(t, fake.gotFilters.PersonalityID)
	require.Equal(t, personalityID, *fake.gotFilters.PersonalityID)
}

func TestListImages_InvalidPersonalityFilterReturnsBadRequest(t *testing.T) {
	t.Parallel()

	fake := &fakeListStore{}
	h := NewHandler(fake, zap.NewNop(), nil)

	req := httptest.NewRequest(http.MethodGet, "/image-gallery?personality_id=not-a-uuid", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))

	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Nil(t, fake.gotFilters.PersonalityID)
}

func TestListImages_ForwardsGlobalOnlyFilter(t *testing.T) {
	t.Parallel()

	fake := &fakeListStore{}
	h := NewHandler(fake, zap.NewNop(), nil)

	req := httptest.NewRequest(http.MethodGet, "/image-gallery?global_only=true", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))

	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotNil(t, fake.gotFilters.GlobalOnly)
	require.True(t, *fake.gotFilters.GlobalOnly)
	require.Nil(t, fake.gotFilters.PersonalityID)
}

func TestListImages_InvalidGlobalOnlyFilterReturnsBadRequest(t *testing.T) {
	t.Parallel()

	fake := &fakeListStore{}
	h := NewHandler(fake, zap.NewNop(), nil)

	req := httptest.NewRequest(http.MethodGet, "/image-gallery?global_only=wat", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))

	rr := httptest.NewRecorder()
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	require.Nil(t, fake.gotFilters.GlobalOnly)
}
