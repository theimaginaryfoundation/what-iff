package remotesyncplugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/remotesync"
	"go.uber.org/zap"
)

type stubStore struct {
	acct  *models.RemoteSyncAccount
	saved []models.RemoteSyncUpsert
}

func (s *stubStore) GetRemoteSyncSettings(context.Context, uuid.UUID) (*models.RemoteSyncSettings, error) {
	if s.acct == nil {
		return &models.RemoteSyncSettings{}, nil
	}
	return &models.RemoteSyncSettings{Configured: true, Enabled: s.acct.Enabled, BaseURL: s.acct.BaseURL, TokenHint: "wxyz"}, nil
}

func (s *stubStore) UpsertRemoteSync(_ context.Context, _ uuid.UUID, in models.RemoteSyncUpsert) error {
	s.saved = append(s.saved, in)
	if s.acct == nil {
		if in.Token == nil {
			return datastore.ErrRemoteSyncTokenRequired
		}
		s.acct = &models.RemoteSyncAccount{Enabled: true}
	}
	s.acct.BaseURL = in.BaseURL
	if in.Token != nil {
		s.acct.Token = *in.Token
	}
	if in.Enabled != nil {
		s.acct.Enabled = *in.Enabled
	}
	return nil
}

func (s *stubStore) DeleteRemoteSync(context.Context, uuid.UUID) error {
	if s.acct == nil {
		return datastore.ErrRemoteSyncNotFound
	}
	s.acct = nil
	return nil
}

func (s *stubStore) GetRemoteSyncAccount(context.Context, uuid.UUID) (*models.RemoteSyncAccount, error) {
	if s.acct == nil {
		return nil, datastore.ErrRemoteSyncNotFound
	}
	a := *s.acct
	return &a, nil
}

type stubSyncer struct {
	err    error
	starts int
}

func (s *stubSyncer) Start(context.Context, uuid.UUID) error { s.starts++; return s.err }
func (s *stubSyncer) Interval() time.Duration                { return 5 * time.Minute }

type checkCall struct{ baseURL, token string }

func newTestHandler() (*Handler, *stubStore, *stubSyncer, *[]checkCall, *error) {
	store, syncer := &stubStore{}, &stubSyncer{}
	var calls []checkCall
	var checkErr error
	h := &Handler{
		Store: store, Syncer: syncer, Lifecycle: context.Background(), Logger: zap.NewNop(),
		Check: func(_ context.Context, baseURL, token string) error {
			calls = append(calls, checkCall{baseURL, token})
			return checkErr
		},
	}
	return h, store, syncer, &calls, &checkErr
}

func do(t *testing.T, h *Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := mux.NewRouter()
	h.Register(r.PathPrefix("/remote-sync").Subrouter())
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSaveChecksTheTokenAndNeverReturnsIt(t *testing.T) {
	h, store, syncer, calls, _ := newTestHandler()

	rec := do(t, h, http.MethodPut, "/remote-sync", `{"base_url":"https://app.whatiff.chat/","token":"Bearer wht_secret-wxyz"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, *calls, 1)
	assert.Equal(t, checkCall{remotesync.DefaultBaseURL, "wht_secret-wxyz"}, (*calls)[0], "the app address maps to the API; Bearer is stripped")
	assert.NotContains(t, rec.Body.String(), "secret")

	var got models.RemoteSyncSettings
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, got.Configured)
	assert.Equal(t, 300, got.IntervalSeconds)
	assert.Equal(t, remotesync.DefaultBaseURL, got.DefaultBaseURL)
	assert.Equal(t, 1, syncer.starts, "a save starts the first run")

	// A later save without a token checks with the stored one.
	rec = do(t, h, http.MethodPut, "/remote-sync", `{"base_url":"api.whatiff.chat"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "wht_secret-wxyz", (*calls)[1].token)
	assert.Nil(t, store.saved[1].Token)
}

func TestSaveReportsWhyTheRemoteRefused(t *testing.T) {
	h, store, syncer, _, checkErr := newTestHandler()
	*checkErr = &remotesync.Error{Kind: remotesync.KindForbidden, Status: 403}

	rec := do(t, h, http.MethodPut, "/remote-sync", `{"token":"wht_x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Read threads")
	assert.Empty(t, store.saved, "nothing saved")
	assert.Zero(t, syncer.starts)
}

func TestSaveValidatesInput(t *testing.T) {
	h, _, _, calls, _ := newTestHandler()

	rec := do(t, h, http.MethodPut, "/remote-sync", `{"base_url":"http://localhost:8080","token":"wht_x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "https")

	rec = do(t, h, http.MethodPut, "/remote-sync", `{"base_url":"https://api.whatiff.chat"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "token")

	rec = do(t, h, http.MethodPut, "/remote-sync", `not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, *calls)

	h.AllowInsecureHTTP = true
	rec = do(t, h, http.MethodPut, "/remote-sync", `{"base_url":"http://localhost:8080","token":"wht_x"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestSwitchingOffSkipsTheCheck(t *testing.T) {
	h, store, syncer, calls, checkErr := newTestHandler()
	require.Equal(t, http.StatusOK, do(t, h, http.MethodPut, "/remote-sync", `{"token":"wht_x"}`).Code)
	*checkErr = &remotesync.Error{Kind: remotesync.KindUnreachable}
	starts := syncer.starts

	rec := do(t, h, http.MethodPut, "/remote-sync", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, *calls, 1, "no check when switching off")
	assert.False(t, store.acct.Enabled)
	assert.Equal(t, starts, syncer.starts)
}

func TestRunNowAndDelete(t *testing.T) {
	h, _, syncer, _, _ := newTestHandler()

	syncer.err = datastore.ErrRemoteSyncNotFound
	assert.Equal(t, http.StatusNotFound, do(t, h, http.MethodPost, "/remote-sync/run", "").Code)
	syncer.err = remotesync.ErrBusy
	assert.Equal(t, http.StatusConflict, do(t, h, http.MethodPost, "/remote-sync/run", "").Code)
	syncer.err = remotesync.ErrDisabled
	assert.Equal(t, http.StatusConflict, do(t, h, http.MethodPost, "/remote-sync/run", "").Code)
	syncer.err = nil
	assert.Equal(t, http.StatusAccepted, do(t, h, http.MethodPost, "/remote-sync/run", "").Code)

	assert.Equal(t, http.StatusNoContent, do(t, h, http.MethodDelete, "/remote-sync", "").Code)
	rec := do(t, h, http.MethodGet, "/remote-sync", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"configured":false`)
}

func TestUnauthenticatedRequestsAreRefused(t *testing.T) {
	h, _, _, _, _ := newTestHandler()
	r := mux.NewRouter()
	h.Register(r.PathPrefix("/remote-sync").Subrouter())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/remote-sync", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
