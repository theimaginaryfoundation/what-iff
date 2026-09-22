package personality

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

var candidateTestKeys = []string{"happy", "content", "sad", "angry", "surprised", "confused", "tired", "in-love", "smug"}

func candidatesRequest(t *testing.T, personalityID uuid.UUID, body any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/personality/"+personalityID.String()+"/expressions/generate-candidates", strings.NewReader(string(raw)))
	return req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
}

func serveCandidates(fake *fakePersonalityAgent, req *http.Request) *httptest.ResponseRecorder {
	h := NewHandler(&fakeStore{}, zap.NewNop(), nil)
	h.personalityAgent = fake
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGenerateExpressionCandidates_EnqueuesWithKeysAndReference(t *testing.T) {
	t.Parallel()
	personalityID := uuid.New()
	refID := uuid.New()
	jobID := uuid.New()

	var gotKeys []string
	var gotRef *uuid.UUID
	var gotPersonality uuid.UUID
	fake := &fakePersonalityAgent{
		enqueueExpressionCandidatesJobFn: func(_ context.Context, _, pid uuid.UUID, keys []string, ref *uuid.UUID) (*models.Job, error) {
			gotPersonality, gotKeys, gotRef = pid, keys, ref
			return &models.Job{ID: jobID}, nil
		},
	}

	ref := refID.String()
	rec := serveCandidates(fake, candidatesRequest(t, personalityID, map[string]any{
		"expressions":        candidateTestKeys,
		"reference_image_id": ref,
	}))

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var resp models.PersonalityMediaJobResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, jobID.String(), resp.JobID)
	require.Equal(t, agent.JobTypeExpressionGrid, resp.JobType)
	require.Equal(t, personalityID, gotPersonality)
	require.Equal(t, candidateTestKeys, gotKeys)
	require.NotNil(t, gotRef)
	require.Equal(t, refID, *gotRef)
}

func TestGenerateExpressionCandidates_NullReference(t *testing.T) {
	t.Parallel()
	var gotRef *uuid.UUID
	called := false
	fake := &fakePersonalityAgent{
		enqueueExpressionCandidatesJobFn: func(_ context.Context, _, _ uuid.UUID, _ []string, ref *uuid.UUID) (*models.Job, error) {
			called, gotRef = true, ref
			return &models.Job{ID: uuid.New()}, nil
		},
	}
	rec := serveCandidates(fake, candidatesRequest(t, uuid.New(), map[string]any{
		"expressions":        candidateTestKeys,
		"reference_image_id": nil,
	}))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.True(t, called)
	require.Nil(t, gotRef)
}

func TestGenerateExpressionCandidates_ValidationErrors(t *testing.T) {
	t.Parallel()
	dup := append([]string(nil), candidateTestKeys...)
	dup[8] = "happy"
	bad := append([]string(nil), candidateTestKeys...)
	bad[0] = "Big Smile"

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"too few", map[string]any{"expressions": candidateTestKeys[:8]}, "exactly 9"},
		{"duplicate", map[string]any{"expressions": dup}, "duplicate expression key"},
		{"invalid key", map[string]any{"expressions": bad}, "invalid expression key"},
		{"bad reference", map[string]any{"expressions": candidateTestKeys, "reference_image_id": "nope"}, "reference_image_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakePersonalityAgent{
				enqueueExpressionCandidatesJobFn: func(context.Context, uuid.UUID, uuid.UUID, []string, *uuid.UUID) (*models.Job, error) {
					t.Fatal("enqueue must not be called on invalid input")
					return nil, nil
				},
			}
			rec := serveCandidates(fake, candidatesRequest(t, uuid.New(), tc.body))
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), tc.want)
		})
	}
}

func TestGenerateExpressionCandidates_EnqueueErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"personality missing", datastore.ErrPersonalityNotFound, http.StatusNotFound},
		{"reference missing", agent.ErrExpressionReferenceImageNotFound, http.StatusNotFound},
		{"images disabled", agent.ErrExpressionImagesDisabled, http.StatusBadRequest},
		{"active job", &agent.ErrPersonalityMediaJobActive{Job: &models.Job{ID: uuid.New(), JobType: agent.JobTypeExpressionGrid, Reference: uuid.New().String()}}, http.StatusConflict},
		{"other", fmt.Errorf("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakePersonalityAgent{
				enqueueExpressionCandidatesJobFn: func(context.Context, uuid.UUID, uuid.UUID, []string, *uuid.UUID) (*models.Job, error) {
					return nil, tc.err
				},
			}
			rec := serveCandidates(fake, candidatesRequest(t, uuid.New(), map[string]any{"expressions": candidateTestKeys}))
			require.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}
