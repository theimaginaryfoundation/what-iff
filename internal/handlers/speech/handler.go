// Package speech serves voice replies and dictation (ADR 0x052).
package speech

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Speech is what the handler needs from the agent.
type Speech interface {
	Dictation(ctx context.Context) (agent.DictationModel, error)
	Transcribe(ctx context.Context, userID uuid.UUID, clip agent.DictationClip) (string, error)
	Listen(ctx context.Context, userID, messageID uuid.UUID) (*models.SpeechTrack, error)
	SpeechTrack(ctx context.Context, userID, trackID uuid.UUID) (*models.SpeechTrack, error)
	SpeechSegmentAudio(ctx context.Context, userID, trackID uuid.UUID, seq int) ([]byte, models.AudioFormat, error)
	PreviewVoice(ctx context.Context, userID uuid.UUID, voice models.PersonalityVoice, text string) (provider.SpeechAudio, error)
}

// Handler serves the speech routes.
type Handler struct {
	speech Speech
	logger *zap.Logger
}

// NewHandler builds the handler.
func NewHandler(speech Speech, logger *zap.Logger) *Handler {
	return &Handler{speech: speech, logger: logger}
}

// RegisterRoutes mounts the routes on an authenticated router.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/speech/dictation", h.GetDictation).Methods(http.MethodGet)
	router.HandleFunc("/speech/transcriptions", h.Transcribe).Methods(http.MethodPost)
	router.HandleFunc("/speech/messages/{id}", h.Listen).Methods(http.MethodPost)
	router.HandleFunc("/speech/tracks/{id}", h.GetTrack).Methods(http.MethodGet)
	router.HandleFunc("/speech/tracks/{id}/segments/{seq}", h.GetSegment).Methods(http.MethodGet)
	router.HandleFunc("/speech/preview", h.Preview).Methods(http.MethodPost)
}

// maxUploadBytes bounds a transcription request, the clip and a few form fields:
// it is both the whole body's cap (MaxBytesReader) and what ParseMultipartForm
// keeps in memory, so a request never spills to disk.
const maxUploadBytes = 13 << 20

// Dictation is what GET /speech/dictation answers: whether the composer has a
// microphone, and if so what to record.
type Dictation struct {
	Available bool `json:"available"`
	// Code says why it isn't: utility_model_not_set, utility_role_off or
	// utility_model_unavailable.
	Code string `json:"code,omitempty"`
	*agent.DictationModel
}

// GetDictation answers whether the account can dictate, and on what model.
func (h *Handler) GetDictation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if !featuregate.IsEntitled(r.Context(), userID) {
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, Dictation{Available: false, Code: models.ErrCodeForbidden})
		return
	}
	m, err := h.speech.Dictation(r.Context())
	var unavailable *models.UtilityRoleUnavailableError
	switch {
	case errors.As(err, &unavailable):
		code := models.ErrCodeUtilityModelNotSet
		switch {
		case errors.Is(err, models.ErrUtilityRoleOff):
			code = models.ErrCodeUtilityRoleOff
		case errors.Is(err, models.ErrUtilityModelUnavailable):
			code = models.ErrCodeUtilityModelUnavailable
		}
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, Dictation{Available: false, Code: code})
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the dictation model", err)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, Dictation{Available: true, DictationModel: &m})
	}
}

// Transcription is what POST /speech/transcriptions answers.
type Transcription struct {
	Text string `json:"text"`
}

// Transcribe transcribes one clip: a multipart form with the clip as "audio",
// its "format" (webm, mp4, ogg, wav, mp3 or flac), "duration_seconds" as the
// browser measured it, and optionally "chat_id" and "language". The clip is
// never stored.
func (h *Handler) Transcribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}
	if !featuregate.IsEntitled(r.Context(), userID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Dictation isn't available on this account", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, models.ErrCodeClipTooLong, "That recording is too long to transcribe", nil)
			return
		}
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Send the recording as multipart form data", err)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, _, err := r.FormFile("audio")
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Send the recording as the audio field", err)
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(file)
	if err != nil || len(audio) == 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "The recording is empty", err)
		return
	}
	format := models.AudioFormat(strings.ToLower(strings.TrimSpace(r.FormValue("format"))))
	if format.MIMEType() == "application/octet-stream" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, models.ErrCodeAudioFormatUnsupported, "Say which format the recording is in", nil)
		return
	}
	seconds, _ := strconv.ParseFloat(r.FormValue("duration_seconds"), 64)
	if seconds < 0 {
		seconds = 0
	}
	clip := agent.DictationClip{
		Audio: audio, Format: format, ClientSeconds: seconds,
		Language: strings.TrimSpace(r.FormValue("language")), ChatID: strings.TrimSpace(r.FormValue("chat_id")),
	}
	if clip.ChatID != "" {
		if _, err := uuid.Parse(clip.ChatID); err != nil {
			clip.ChatID = ""
		}
	}

	text, err := h.speech.Transcribe(r.Context(), userID, clip)
	switch {
	case err == nil:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, Transcription{Text: text})
	case errors.Is(err, agent.ErrQuotaExceeded):
		handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, models.ErrCodeQuotaExceeded, "You're out of credits for dictation", nil)
	case errors.Is(err, agent.ErrAudioFormatUnsupported):
		handlerutils.RespondWithError(w, h.logger, http.StatusUnsupportedMediaType, models.ErrCodeAudioFormatUnsupported, err.Error(), nil)
	case errors.Is(err, agent.ErrClipTooLong):
		handlerutils.RespondWithError(w, h.logger, http.StatusRequestEntityTooLarge, models.ErrCodeClipTooLong, err.Error(), nil)
	case handlerutils.RespondIfUtilityRoleUnavailable(w, h.logger, err):
	case handlerutils.RespondIfProviderNotConfigured(w, h.logger, err):
	case errors.Is(err, r.Context().Err()):
		// The person cancelled; nobody is waiting for an answer.
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusBadGateway, models.ErrCodeBadGateway, "Couldn't transcribe that recording. Try again.", err)
	}
}

// account reads the signed-in account, answering 401 without one, and 403 where speech
// isn't available on it.
func (h *Handler) account(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return uuid.Nil, false
	}
	if !featuregate.IsEntitled(r.Context(), userID) {
		handlerutils.RespondWithError(w, h.logger, http.StatusForbidden, handlerutils.CodeNotSet, "Voice replies aren't available on this account", nil)
		return uuid.Nil, false
	}
	return userID, true
}

func pathID(w http.ResponseWriter, r *http.Request, h *Handler) (uuid.UUID, bool) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Not found", nil)
		return uuid.Nil, false
	}
	return id, true
}

// Listen answers the reply's speech track in its personality's voice, started if
// it isn't there yet (ADR 0x052 §7). The client then polls the track.
func (h *Handler) Listen(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.account(w, r)
	if !ok {
		return
	}
	messageID, ok := pathID(w, r, h)
	if !ok {
		return
	}
	track, err := h.speech.Listen(r.Context(), userID, messageID)
	switch {
	case err == nil:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, track)
	case errors.Is(err, datastore.ErrChatMessageNotFound), errors.Is(err, datastore.ErrChatNotFound), errors.Is(err, datastore.ErrPersonalityNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Message not found", nil)
	case errors.Is(err, agent.ErrNotAReply), errors.Is(err, agent.ErrNoVoice):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, models.ErrCodeVoiceUnavailable, err.Error(), nil)
	case errors.Is(err, agent.ErrVoiceUnavailable):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, models.ErrCodeVoiceUnavailable, err.Error(), nil)
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Couldn't read that reply aloud", err)
	}
}

// GetTrack answers a track's status and the segments made so far.
func (h *Handler) GetTrack(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.account(w, r)
	if !ok {
		return
	}
	trackID, ok := pathID(w, r, h)
	if !ok {
		return
	}
	track, err := h.speech.SpeechTrack(r.Context(), userID, trackID)
	switch {
	case errors.Is(err, datastore.ErrSpeechTrackNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Track not found", nil)
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the track", err)
	default:
		handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, track)
	}
}

// GetSegment answers one segment's audio. It never changes, so the browser may keep it.
func (h *Handler) GetSegment(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.account(w, r)
	if !ok {
		return
	}
	trackID, ok := pathID(w, r, h)
	if !ok {
		return
	}
	seq, err := strconv.Atoi(mux.Vars(r)["seq"])
	if err != nil || seq < 0 {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Segment not found", nil)
		return
	}
	data, format, err := h.speech.SpeechSegmentAudio(r.Context(), userID, trackID, seq)
	switch {
	case errors.Is(err, datastore.ErrSpeechTrackNotFound):
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Segment not found", nil)
	case err != nil:
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to read the segment", err)
	default:
		writeAudio(w, data, format, "private, max-age=86400, immutable")
	}
}

func writeAudio(w http.ResponseWriter, data []byte, format models.AudioFormat, cache string) {
	w.Header().Set("Content-Type", format.MIMEType())
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", cache)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// PreviewRequest is a voice to try, and what to say in it.
type PreviewRequest struct {
	models.PersonalityVoice
	Text string `json:"text"`
}

// Preview reads text aloud in a voice not yet saved, for the personality page. It is
// billed like any synthesis, and not stored.
func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.account(w, r)
	if !ok {
		return
	}
	var req PreviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || req.ModelID == uuid.Nil || req.Voice == "" || strings.TrimSpace(req.Text) == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Send a model_id, a voice and some text", err)
		return
	}
	audio, err := h.speech.PreviewVoice(r.Context(), userID, req.PersonalityVoice, req.Text)
	switch {
	case err == nil:
		writeAudio(w, audio.Data, audio.Format, "no-store")
	case errors.Is(err, agent.ErrQuotaExceeded):
		handlerutils.RespondWithError(w, h.logger, http.StatusTooManyRequests, models.ErrCodeQuotaExceeded, "You're out of credits for voice", nil)
	case errors.Is(err, agent.ErrPreviewTooLong), errors.Is(err, models.ErrInvalidPersonalityVoice):
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, err.Error(), nil)
	case errors.Is(err, agent.ErrVoiceUnavailable):
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, models.ErrCodeVoiceUnavailable, err.Error(), nil)
	case errors.Is(err, r.Context().Err()):
	default:
		handlerutils.RespondWithError(w, h.logger, http.StatusBadGateway, models.ErrCodeBadGateway, "Couldn't make the preview. Try again.", err)
	}
}
