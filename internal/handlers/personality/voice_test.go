package personality

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func voiceRequest(t *testing.T, store *fakeStore, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	NewHandler(store, zap.NewNop(), nil).RegisterRoutes(router)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// A personality's voice has routes of its own, so no personality PUT can clear it (ADR 0x052 §5).
func TestVoiceRoutes(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	id, model := uuid.New(), uuid.New()
	path := "/personality/" + id.String() + "/voice"

	rec := voiceRequest(t, store, http.MethodPut, path, `{"model_id":"`+model.String()+`","voice":"Kore","style":"warm"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []*models.PersonalityVoice{{ModelID: model, Voice: "Kore", Style: "warm"}}, store.voices)
	require.Contains(t, rec.Body.String(), `"voice":{"model_id"`)

	rec = voiceRequest(t, store, http.MethodDelete, path, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, store.voices[1], "DELETE clears it")

	rec = voiceRequest(t, store, http.MethodPut, path, `{"voice":"Kore"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, "no model")

	store.setVoiceErr = fmt.Errorf("%w: no such voice", models.ErrInvalidPersonalityVoice)
	rec = voiceRequest(t, store, http.MethodPut, path, `{"model_id":"`+model.String()+`","voice":"Nobody"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "no such voice")

	store.setVoiceErr = datastore.ErrPersonalityNotFound
	rec = voiceRequest(t, store, http.MethodDelete, path, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}
