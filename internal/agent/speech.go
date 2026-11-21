package agent

// Speech (ADR 0x052): which client and endpoint a speech or transcription model is
// reached through, and dictation, which transcribes one clip on the account's
// dictation model.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

// buildSpeechEndpoints is where each built-in provider's speech calls go: its
// configured base URL through the credentialed client, as its chat calls do.
// Z.AI's transcription is on its OpenAI-style root beside the Anthropic one.
func buildSpeechEndpoints(cfg AgentConfig) map[models.ModelProvider]provider.SpeechEndpoint {
	orDefault := func(v, fallback string) string {
		if v != "" {
			return v
		}
		return fallback
	}
	ep := func(p models.ModelProvider, base string) provider.SpeechEndpoint {
		return provider.SpeechEndpoint{Provider: p, BaseURL: base, HTTP: cfg.HTTPClient}
	}
	return map[models.ModelProvider]provider.SpeechEndpoint{
		models.ModelProviderOpenAI:  ep(models.ModelProviderOpenAI, provider.DefaultOpenAIBaseURL),
		models.ModelProviderGoogle:  ep(models.ModelProviderGoogle, orDefault(cfg.GeminiBaseURL, provider.DefaultGeminiBaseURL)),
		models.ModelProviderQwen:    ep(models.ModelProviderQwen, orDefault(cfg.QwenBaseURL, provider.DefaultQwenBaseURL)),
		models.ModelProviderMistral: ep(models.ModelProviderMistral, orDefault(cfg.MistralBaseURL, provider.DefaultMistralBaseURL)),
		models.ModelProviderZAI:     ep(models.ModelProviderZAI, provider.ZAIPaaSBaseURL(cfg.ZAIBaseURL)),
	}
}

// speechTarget is a speech or transcription model as its client reaches it.
type speechTarget struct {
	model    string
	provider string // a built-in provider, or a connection's preset
	spec     models.KnownSpeechModel
	endpoint provider.SpeechEndpoint
}

// maxDictationSeconds bounds a clip whatever the model takes (ADR 0x052 §10), and
// maxDictationBytes bounds its upload: five minutes of 16 kHz mono WAV is 9.6 MB.
const (
	maxDictationSeconds = 5 * 60
	maxDictationBytes   = 12 << 20
)

// openEndedTranscriptionFormats are what a transcription model the known table
// doesn't know is assumed to take: what OpenAI's audio route takes.
var openEndedTranscriptionFormats = []models.AudioFormat{
	models.AudioFormatWebM, models.AudioFormatMP4, models.AudioFormatOgg, models.AudioFormatWAV, models.AudioFormatMP3, models.AudioFormatFLAC,
}

// speechTargetFor is how the model t names is reached.
func (a *Agent) speechTargetFor(t utilityTarget) (speechTarget, error) {
	if t.connection != nil {
		// An OpenRouter, Ollama or Custom connection: OpenAI's audio routes, at the
		// connection's address with its key.
		spec, ok := models.LookupKnownSpeechModel(models.ModelProvider(t.connection.Preset), t.name)
		if !ok {
			spec = models.KnownSpeechModel{ID: t.name, Kind: t.kind, Adapter: models.SpeechAdapterOpenAIAudio, Formats: openEndedTranscriptionFormats}
		}
		return speechTarget{model: t.name, provider: t.connection.Preset, spec: spec, endpoint: provider.SpeechEndpoint{
			Provider: models.ModelProvider(t.connection.Preset), BaseURL: t.connection.BaseURL, Key: t.connection.Key, HTTP: a.connectionClient,
		}}, nil
	}
	p := models.ProviderForModel(t.provider, t.name)
	ep, ok := a.speechEndpoints[p]
	if !ok {
		return speechTarget{}, fmt.Errorf("%w: %s offers no speech models", ErrModelDisabled, p.DisplayName())
	}
	spec, ok := models.LookupKnownSpeechModel(p, t.name)
	if !ok {
		// A catalog model the table doesn't know: the provider's own adapter.
		spec = models.KnownSpeechModel{ID: t.name, Kind: t.kind, Adapter: builtinSpeechAdapter(p, t.kind), Formats: []models.AudioFormat{models.AudioFormatWAV, models.AudioFormatMP3}}
	}
	return speechTarget{model: t.name, provider: string(p), spec: spec, endpoint: ep}, nil
}

// builtinSpeechAdapter is the client for a built-in provider's speech model.
func builtinSpeechAdapter(p models.ModelProvider, kind models.ModelKind) models.SpeechAdapter {
	switch p {
	case models.ModelProviderGoogle:
		return models.SpeechAdapterGemini
	case models.ModelProviderQwen:
		return models.SpeechAdapterQwen
	case models.ModelProviderMistral:
		if kind == models.ModelKindSpeech {
			return models.SpeechAdapterMistral
		}
	}
	return models.SpeechAdapterOpenAIAudio
}

// DictationModel is what the composer needs to know about the account's dictation
// model to record for it.
type DictationModel struct {
	Model       string               `json:"model"`
	DisplayName string               `json:"display_name"`
	Formats     []models.AudioFormat `json:"formats"`
	// MaxSeconds is the longest clip it takes, at most five minutes.
	MaxSeconds int `json:"max_seconds"`
}

// Dictation is the account's dictation model, or a *models.UtilityRoleUnavailableError
// when it has none (off, not set, or unusable): then the composer has no microphone.
func (a *Agent) Dictation(ctx context.Context) (DictationModel, error) {
	t, err := a.resolveUtilityTarget(ctx, models.UtilityRoleTranscription)
	if err != nil {
		return DictationModel{}, err
	}
	st, err := a.speechTargetFor(t)
	if err != nil {
		return DictationModel{}, &models.UtilityRoleUnavailableError{Role: models.UtilityRoleTranscription, Err: fmt.Errorf("%w: %w", models.ErrUtilityModelUnavailable, err)}
	}
	return dictationModel(st), nil
}

func dictationModel(st speechTarget) DictationModel {
	maxSeconds := maxDictationSeconds
	if st.spec.MaxClipSeconds > 0 && st.spec.MaxClipSeconds < maxSeconds {
		maxSeconds = st.spec.MaxClipSeconds
	}
	name := st.spec.DisplayName
	if name == "" {
		name = st.model
	}
	return DictationModel{Model: st.model, DisplayName: name, Formats: st.spec.Formats, MaxSeconds: maxSeconds}
}

// DictationClip is one clip the person recorded in the composer.
type DictationClip struct {
	Audio  []byte
	Format models.AudioFormat
	// ClientSeconds is the length the browser measured. It is trusted only as far as
	// the clip's size allows (clipSeconds).
	ClientSeconds float64
	// Language is an optional BCP-47 hint, from the account's locale.
	Language string
	// ChatID is the chat the clip is for, for usage records; empty for none.
	ChatID string
}

// ErrAudioFormatUnsupported and ErrClipTooLong are a clip the dictation model can't take.
var (
	ErrAudioFormatUnsupported = errors.New("the dictation model doesn't take this audio format")
	ErrClipTooLong            = errors.New("the clip is longer than the dictation model takes")
)

// Transcribe transcribes clip on the account's dictation model, metered as one
// transcription. The clip is never stored or logged; only its length is recorded.
func (a *Agent) Transcribe(ctx context.Context, userID uuid.UUID, clip DictationClip) (string, error) {
	t, err := a.resolveUtilityTarget(ctx, models.UtilityRoleTranscription)
	if err != nil {
		return "", err
	}
	st, err := a.speechTargetFor(t)
	if err != nil {
		return "", &models.UtilityRoleUnavailableError{Role: models.UtilityRoleTranscription, Err: fmt.Errorf("%w: %w", models.ErrUtilityModelUnavailable, err)}
	}
	limits := dictationModel(st)
	if !st.spec.AcceptsFormat(clip.Format) {
		return "", fmt.Errorf("%w: %s takes %v, not %s", ErrAudioFormatUnsupported, limits.DisplayName, limits.Formats, clip.Format)
	}
	seconds := clipSeconds(clip)
	maxBytes := int64(maxDictationBytes)
	if st.spec.MaxClipBytes > 0 && st.spec.MaxClipBytes < maxBytes {
		maxBytes = st.spec.MaxClipBytes
	}
	// A second's grace: the browser stops a moment after the limit.
	if seconds > float64(limits.MaxSeconds)+1 || int64(len(clip.Audio)) > maxBytes {
		return "", fmt.Errorf("%w: %s takes at most %d seconds", ErrClipTooLong, limits.DisplayName, limits.MaxSeconds)
	}

	decision := metering.Decision{Allowed: true}
	if a.meter != nil {
		// Like images, speech isn't priced by the chat model's tier.
		decision = a.meter.Check(ctx, userID, "", models.ActionTypeTranscription)
		if !decision.Allowed {
			if decision.Err != nil {
				return "", fmt.Errorf("check transcription quota: %w", decision.Err)
			}
			a.recordQuotaRejection(ctx)
			return "", fmt.Errorf("%w for user %s", ErrQuotaExceeded, userID)
		}
	}

	client, err := provider.NewSpeechClient(st.spec.Adapter, st.endpoint)
	if err != nil {
		return "", err
	}
	start := time.Now()
	text, _, err := client.Transcribe(ctx, provider.TranscriptionRequest{Model: st.model, Audio: clip.Audio, Format: clip.Format, Language: clip.Language})
	attrs := []attribute.KeyValue{telemetry.AttrGenAIProvider.String(st.provider), telemetry.AttrGenAIModel.String(st.model)}
	a.metrics().Record(ctx, telemetry.SpeechDuration, time.Since(start).Seconds(), append(attrs, telemetry.AttrOperation.String("transcription"))...)
	if err != nil {
		var missing *providerkeys.NotConfiguredError
		if errors.As(err, &missing) {
			return "", &models.UtilityRoleUnavailableError{Role: models.UtilityRoleTranscription, Err: fmt.Errorf("%w: %w", models.ErrUtilityModelUnavailable, err)}
		}
		a.metrics().Add(ctx, telemetry.SpeechFailures, 1, append(attrs, telemetry.AttrOperation.String("transcription"))...)
		a.logger.Warn("dictation failed", zap.String("provider", st.provider), zap.String("model", st.model), zap.Error(err))
		return "", err
	}
	a.metrics().Record(ctx, telemetry.SpeechClipDuration, seconds, attrs...)
	if a.meter != nil {
		a.meter.Record(ctx, decision, metering.Usage{
			UserID:         userID,
			ActionType:     models.ActionTypeTranscription,
			Model:          st.model,
			ChatID:         clip.ChatID,
			SpeechProvider: st.provider,
			AudioSeconds:   seconds,
			Source:         metering.TurnSourceApp,
		})
	}
	return text, nil
}

// compressedBytesPerSecond is the most a browser's compressed recording spends per
// second (Opus or AAC at 128 kbit/s); a clip can't be shorter than its size at it.
const compressedBytesPerSecond = 16000

// clipSeconds is how long a clip is: exactly, from a WAV header; otherwise what
// the browser measured, but never less than the clip's size at the highest
// bitrate a browser records at, so a short claimed length can't hide a long clip.
func clipSeconds(clip DictationClip) float64 {
	if clip.Format == models.AudioFormatWAV {
		if s, ok := wavSeconds(clip.Audio); ok {
			return s
		}
	}
	floor := float64(len(clip.Audio)) / compressedBytesPerSecond
	return max(clip.ClientSeconds, floor)
}

// wavSeconds reads a WAV file's length from its fmt and data chunks.
func wavSeconds(b []byte) (float64, bool) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, false
	}
	var byteRate uint32
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		switch id {
		case "fmt ":
			if body+12 <= len(b) {
				byteRate = binary.LittleEndian.Uint32(b[body+8 : body+12])
			}
		case "data":
			if byteRate == 0 {
				return 0, false
			}
			// A streamed recording may leave the size unset; what arrived counts.
			if size == 0 || body+size > len(b) {
				size = len(b) - body
			}
			return float64(size) / float64(byteRate), true
		}
		off = body + size + size%2
	}
	return 0, false
}
