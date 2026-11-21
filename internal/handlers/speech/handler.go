// Package speech serves voice replies and dictation (ADR 0x052).
package speech

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent"
	"github.com/theimaginaryfoundation/what-iff/internal/featuregate"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Speech is what the handler needs from the agent.
type Speech interface {
	Dictation(ctx context.Context) (agent.DictationModel, error)
	Transcribe(ctx context.Context, userID uuid.UUID, clip agent.DictationClip) (string, error)
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
// its "format" (wav: 16 kHz mono PCM, whose length is read from its data), and
// optionally "chat_id" and "language". The clip is never stored.
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
	clip := agent.DictationClip{
		Audio: audio, Format: format,
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
