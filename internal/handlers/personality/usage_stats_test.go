package personality

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

// promptHistoryFakeStore opts the fake into the history-aware PUT seam so the
// production PUT /personality/{id} path (UpdatePersonalityWithPromptHistory) is
// exercised instead of the legacy fallback.
type promptHistoryFakeStore struct {
	*fakeStore
	updateWithHistoryFn func(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error)
}

func (f *promptHistoryFakeStore) UpdatePersonalityWithPromptHistory(ctx context.Context, userID uuid.UUID, personality models.Personality) (*models.Personality, error) {
	return f.updateWithHistoryFn(ctx, userID, personality)
}
func (f *promptHistoryFakeStore) ListPersonalityPromptChanges(ctx context.Context, userID, personalityID uuid.UUID) ([]models.PersonalityPromptChange, error) {
	return nil, errors.New("not implemented")
}
func (f *promptHistoryFakeStore) RevertPersonalityPromptChange(ctx context.Context, userID, personalityID, changeID uuid.UUID) (*models.PersonalityPromptChange, error) {
	return nil, errors.New("not implemented")
}

func serveUsageStatsRequest(t *testing.T, store Store, method, path, body string, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(store, zap.NewNop(), nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// Regression for #129: the stand-alone personality page reads stats from the
// single-personality responses (GET and PUT /personality/{id}). The datastore
// returns those with zeroed Stats, so the handler must fill them in or the page
// shows "0 threads" while the list card shows the real count.
func TestSinglePersonalityResponses_IncludeUsageStats(t *testing.T) {
	t.Parallel()

	lastUsed := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	wantStats := models.PersonalityUsageStats{ChatCount: 3, LastUsedAt: &lastUsed}
	putBody := `{"name":"Trickster","system_prompt":"sp"}`

	tests := []struct {
		name   string
		method string
		body   string
		store  func(base *fakeStore) Store
	}{
		{
			name:   "GET",
			method: http.MethodGet,
			store:  func(base *fakeStore) Store { return base },
		},
		{
			name:   "PUT legacy fallback",
			method: http.MethodPut,
			body:   putBody,
			store:  func(base *fakeStore) Store { return base },
		},
		{
			name:   "PUT with prompt history",
			method: http.MethodPut,
			body:   putBody,
			store: func(base *fakeStore) Store {
				return &promptHistoryFakeStore{fakeStore: base, updateWithHistoryFn: base.updatePersonalityFn}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			personalityID := uuid.New()
			// Mirror the datastore: single-personality reads/writes carry zero Stats.
			stored := func() *models.Personality {
				return &models.Personality{ID: personalityID, Name: "Trickster", SystemPrompt: "sp"}
			}
			var statsCalls int
			base := &fakeStore{
				getPersonalityFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Personality, error) {
					return stored(), nil
				},
				updatePersonalityFn: func(ctx context.Context, uid uuid.UUID, p models.Personality) (*models.Personality, error) {
					return stored(), nil
				},
				getPersonalityUsageStatsFn: func(ctx context.Context, uid, pid uuid.UUID) (models.PersonalityUsageStats, error) {
					statsCalls++
					require.Equal(t, userID, uid)
					require.Equal(t, personalityID, pid)
					return wantStats, nil
				},
			}

			w := serveUsageStatsRequest(t, tt.store(base), tt.method, "/personality/"+personalityID.String(), tt.body, userID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, statsCalls)

			var resp models.Personality
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			require.Equal(t, wantStats.ChatCount, resp.Stats.ChatCount)
			require.NotNil(t, resp.Stats.LastUsedAt)
			require.True(t, wantStats.LastUsedAt.Equal(*resp.Stats.LastUsedAt))
		})
	}
}

// A stats lookup failure must not block loading the personality itself.
func TestGetPersonality_UsageStatsErrorStillReturnsPersonality(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	personalityID := uuid.New()
	store := &fakeStore{
		getPersonalityFn: func(ctx context.Context, uid, id uuid.UUID) (*models.Personality, error) {
			return &models.Personality{ID: personalityID, Name: "Trickster", SystemPrompt: "sp"}, nil
		},
		getPersonalityUsageStatsFn: func(ctx context.Context, uid, pid uuid.UUID) (models.PersonalityUsageStats, error) {
			return models.PersonalityUsageStats{}, errors.New("db down")
		},
	}

	w := serveUsageStatsRequest(t, store, http.MethodGet, "/personality/"+personalityID.String(), "", userID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp models.Personality
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, personalityID, resp.ID)
	require.Equal(t, 0, resp.Stats.ChatCount)
}
