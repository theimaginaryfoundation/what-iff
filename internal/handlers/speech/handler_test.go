package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type fakeSpeech struct {
	dictation agent.DictationModel
	err       error
	text      string
	got       []agent.DictationClip

	track    *models.SpeechTrack
	audio    []byte
	format   models.AudioFormat
	previews []string
}

func (f *fakeSpeech) Listen(context.Context, uuid.UUID, uuid.UUID) (*models.SpeechTrack, error) {
	return f.track, f.err
}

func (f *fakeSpeech) SpeechTrack(context.Context, uuid.UUID, uuid.UUID) (*models.SpeechTrack, error) {
	return f.track, f.err
}

func (f *fakeSpeech) SpeechSegmentAudio(_ context.Context, _, _ uuid.UUID, seq int) ([]byte, models.AudioFormat, error) {
	if f.err != nil || seq > 0 {
		return nil, "", datastore.ErrSpeechTrackNotFound
	}
	return f.audio, f.format, nil
}

func (f *fakeSpeech) PreviewVoice(_ context.Context, _ uuid.UUID, _ models.PersonalityVoice, text string) (provider.SpeechAudio, error) {
	f.previews = append(f.previews, text)
	return provider.SpeechAudio{Data: f.audio, Format: f.format}, f.err
}

func (f *fakeSpeech) Dictation(context.Context) (agent.DictationModel, error) {
	return f.dictation, f.err
}

func (f *fakeSpeech) Transcribe(_ context.Context, _ uuid.UUID, clip agent.DictationClip) (string, error) {
	f.got = append(f.got, clip)
	return f.text, f.err
}

func serve(t *testing.T, s *fakeSpeech, req *http.Request, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	NewHandler(s, zap.NewNop()).RegisterRoutes(router)
	if signedIn {
		req = req.WithContext(apicontext.WithUserID(req.Context(), uuid.New()))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func clipRequest(t *testing.T, fields map[string]string, audio []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(t, w.WriteField(k, v))
	}
	if audio != nil {
		part, err := w.CreateFormFile("audio", "clip")
		require.NoError(t, err)
		_, _ = part.Write(audio)
	}
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/speech/transcriptions", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestGetDictation(t *testing.T) {
	t.Parallel()
	s := &fakeSpeech{dictation: agent.DictationModel{Model: "gpt-transcribe", DisplayName: "GPT Transcribe", Formats: []models.AudioFormat{models.AudioFormatWebM}, MaxSeconds: 300}}
	rec := serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/dictation", nil), true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"available":true,"model":"gpt-transcribe","display_name":"GPT Transcribe","formats":["webm"],"max_seconds":300}`, rec.Body.String())

	s.err = &models.UtilityRoleUnavailableError{Role: models.UtilityRoleTranscription, Err: models.ErrUtilityRoleOff}
	rec = serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/dictation", nil), true)
	require.Equal(t, http.StatusOK, rec.Code, "no microphone is not an error")
	require.JSONEq(t, `{"available":false,"code":"utility_role_off"}`, rec.Body.String())

	rec = serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/dictation", nil), false)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTranscribe(t *testing.T) {
	t.Parallel()
	s := &fakeSpeech{text: "Hello there."}
	chat := uuid.NewString()
	rec := serve(t, s, clipRequest(t, map[string]string{"format": "WEBM", "duration_seconds": "4.5", "chat_id": chat, "language": "en-GB"}, []byte("opus")), true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out Transcription
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, "Hello there.", out.Text)
	require.Equal(t, []agent.DictationClip{{Audio: []byte("opus"), Format: models.AudioFormatWebM, ClientSeconds: 4.5, Language: "en-GB", ChatID: chat}}, s.got)

	rec = serve(t, s, clipRequest(t, map[string]string{"format": "webm", "chat_id": "not-a-uuid", "duration_seconds": "-3"}, []byte("opus")), true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, s.got[1].ChatID, "a bad chat id is dropped, not trusted")
	require.Zero(t, s.got[1].ClientSeconds)
}

func TestTranscribe_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields map[string]string
		audio  []byte
		err    error
		status int
		code   string
	}{
		{"no clip", map[string]string{"format": "webm"}, nil, nil, http.StatusBadRequest, ""},
		{"unknown format", map[string]string{"format": "aiff"}, []byte("x"), nil, http.StatusBadRequest, models.ErrCodeAudioFormatUnsupported},
		{"out of credits", map[string]string{"format": "wav"}, []byte("x"), fmt.Errorf("%w for user", agent.ErrQuotaExceeded), http.StatusTooManyRequests, models.ErrCodeQuotaExceeded},
		{"format the model refuses", map[string]string{"format": "webm"}, []byte("x"), agent.ErrAudioFormatUnsupported, http.StatusUnsupportedMediaType, models.ErrCodeAudioFormatUnsupported},
		{"too long", map[string]string{"format": "wav"}, []byte("x"), agent.ErrClipTooLong, http.StatusRequestEntityTooLarge, models.ErrCodeClipTooLong},
		{"no dictation model", map[string]string{"format": "wav"}, []byte("x"), &models.UtilityRoleUnavailableError{Role: models.UtilityRoleTranscription, Err: models.ErrUtilityRoleNotSet}, http.StatusConflict, models.ErrCodeUtilityModelNotSet},
		{"provider failure", map[string]string{"format": "wav"}, []byte("x"), errors.New("Mistral transcription: status 500"), http.StatusBadGateway, models.ErrCodeBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := serve(t, &fakeSpeech{err: tc.err}, clipRequest(t, tc.fields, tc.audio), true)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.code != "" {
				require.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
			}
			require.NotContains(t, rec.Body.String(), "status 500", "a provider's own error isn't shown")
		})
	}
}

func TestTranscribe_TooLargeAnUpload(t *testing.T) {
	t.Parallel()
	rec := serve(t, &fakeSpeech{}, clipRequest(t, map[string]string{"format": "wav"}, make([]byte, maxUploadBytes+1)), true)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), models.ErrCodeClipTooLong)
}

func TestListenAndTracks(t *testing.T) {
	t.Parallel()
	trackID, msgID := uuid.New(), uuid.New()
	s := &fakeSpeech{track: &models.SpeechTrack{ID: trackID, Status: models.SpeechTrackOpen, Segments: []models.SpeechSegment{{Seq: 0, Format: models.AudioFormatMP3, FileKey: "secret/key"}}},
		audio: []byte("ID3audio"), format: models.AudioFormatMP3}

	rec := serve(t, s, httptest.NewRequest(http.MethodPost, "/speech/messages/"+msgID.String(), nil), true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"status":"open"`)
	require.NotContains(t, rec.Body.String(), "secret/key", "file keys stay on the server")

	rec = serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/tracks/"+trackID.String(), nil), true)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/tracks/"+trackID.String()+"/segments/0", nil), true)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "audio/mpeg", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Cache-Control"), "private")
	require.Equal(t, "ID3audio", rec.Body.String())

	rec = serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/tracks/"+trackID.String()+"/segments/1", nil), true)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = serve(t, s, httptest.NewRequest(http.MethodGet, "/speech/tracks/not-a-uuid", nil), true)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListen_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{agent.ErrNoVoice, http.StatusConflict, models.ErrCodeVoiceUnavailable},
		{fmt.Errorf("%w: gone", agent.ErrVoiceUnavailable), http.StatusConflict, models.ErrCodeVoiceUnavailable},
		{datastore.ErrChatMessageNotFound, http.StatusNotFound, ""},
	} {
		rec := serve(t, &fakeSpeech{err: tc.err}, httptest.NewRequest(http.MethodPost, "/speech/messages/"+uuid.NewString(), nil), true)
		require.Equal(t, tc.status, rec.Code, tc.err.Error())
		if tc.code != "" {
			require.Contains(t, rec.Body.String(), tc.code)
		}
	}
}

func TestPreview(t *testing.T) {
	t.Parallel()
	s := &fakeSpeech{audio: []byte("RIFFwav"), format: models.AudioFormatWAV}
	body := `{"model_id":"` + uuid.NewString() + `","voice":"Kore","style":"warm","text":"Hi, I'm Ada."}`
	rec := serve(t, s, httptest.NewRequest(http.MethodPost, "/speech/preview", strings.NewReader(body)), true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "audio/wav", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, []string{"Hi, I'm Ada."}, s.previews)

	rec = serve(t, s, httptest.NewRequest(http.MethodPost, "/speech/preview", strings.NewReader(`{"voice":"Kore","text":"x"}`)), true)
	require.Equal(t, http.StatusBadRequest, rec.Code, "no model")
	s.err = fmt.Errorf("%w for user", agent.ErrQuotaExceeded)
	rec = serve(t, s, httptest.NewRequest(http.MethodPost, "/speech/preview", strings.NewReader(body)), true)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
}
