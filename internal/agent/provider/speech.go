package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Speech synthesis and transcription (ADR 0x052 §3).
//
// Neither fits the utility call, which is text in and text or JSON out, so each
// has its own small interface, the way image generation has ImageGenerator.
// One client per adapter (models.SpeechAdapter) implements both; a direction a
// provider doesn't offer answers ErrSpeechUnsupported.

// SpeechRequest asks for one piece of text read aloud, verbatim.
type SpeechRequest struct {
	Model string
	Text  string
	Voice string
	// Style is the free-text delivery instruction, sent only where the model
	// takes one (the caller checks the model's Style); empty sends none.
	Style string
	// Format is the audio format wanted; a model that can't return it returns
	// its own, and SpeechAudio says which.
	Format models.AudioFormat
}

// SpeechAudio is one complete audio file.
type SpeechAudio struct {
	Data   []byte
	Format models.AudioFormat
}

// SpeechUsage is what one synthesis cost, as far as the provider says.
type SpeechUsage struct {
	// Characters is the text sent, counted in runes.
	Characters   int
	InputTokens  int64
	OutputTokens int64
}

// TranscriptionRequest asks for one clip's transcript.
type TranscriptionRequest struct {
	Model  string
	Audio  []byte
	Format models.AudioFormat
	// Language is an optional BCP-47 hint; empty lets the model detect it.
	Language string
}

// TranscriptionUsage is what one transcription cost, as far as the provider says.
type TranscriptionUsage struct {
	InputTokens  int64
	OutputTokens int64
}

// SpeechClient synthesises speech and transcribes clips for one adapter.
type SpeechClient interface {
	Synthesize(ctx context.Context, req SpeechRequest) (SpeechAudio, SpeechUsage, error)
	Transcribe(ctx context.Context, req TranscriptionRequest) (string, TranscriptionUsage, error)
}

// ErrSpeechUnsupported is returned for a direction an adapter doesn't offer.
var ErrSpeechUnsupported = errors.New("this provider doesn't offer that kind of speech model")

// SpeechEndpoint is where a speech client's requests go.
type SpeechEndpoint struct {
	Provider models.ModelProvider
	// BaseURL is the provider's OpenAI-compatible base, as its chat client uses
	// (".../v1", Gemini's ".../v1beta/openai/", Qwen's ".../compatible-mode/v1").
	// The Gemini and Qwen adapters derive their native roots from it.
	BaseURL string
	// Key is sent as a Bearer token (x-goog-api-key for Gemini) when set. A
	// built-in provider's client leaves it empty: the credential transport
	// attaches the account's or deployment's key by host.
	Key  string
	HTTP *http.Client
}

// NewSpeechClient returns the client for adapter at ep.
func NewSpeechClient(adapter models.SpeechAdapter, ep SpeechEndpoint) (SpeechClient, error) {
	base := speechHTTP{imageHTTPClient: imageHTTPClient{client: ep.HTTP}, provider: ep.Provider}
	switch adapter {
	case models.SpeechAdapterOpenAIAudio:
		return &openAIAudioClient{speechHTTP: base, baseURL: ep.BaseURL, key: ep.Key}, nil
	case models.SpeechAdapterMistral:
		return &mistralSpeechClient{openAIAudioClient{speechHTTP: base, baseURL: ep.BaseURL, key: ep.Key}}, nil
	case models.SpeechAdapterGemini:
		return &geminiSpeechClient{speechHTTP: base, nativeBase: GeminiNativeBaseURL(ep.BaseURL), key: ep.Key}, nil
	case models.SpeechAdapterQwen:
		return &qwenSpeechClient{speechHTTP: base, compatBase: orDefault(ep.BaseURL, DefaultQwenBaseURL), nativeBase: QwenNativeBaseURL(ep.BaseURL), key: ep.Key}, nil
	}
	return nil, fmt.Errorf("unknown speech adapter %q", adapter)
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// maxSpeechAudioBytes caps an audio answer or download. A segment is at most a
// model's MaxInputChars: about two minutes of speech, under 6 MB even as Gemini's
// 24 kHz WAV, and base64 in a JSON answer adds a third.
const maxSpeechAudioBytes = 16 << 20

// speechHTTP is the HTTP plumbing the speech clients share, on top of the image
// adapters' JSON POST with its 429/503 backoff.
type speechHTTP struct {
	imageHTTPClient
	provider models.ModelProvider
}

// speechError names the provider in a failed call and keeps the body short.
func (h speechHTTP) speechError(what string, err error) error {
	var apiErr *imageAPIError
	if errors.As(err, &apiErr) {
		body := strings.TrimSpace(apiErr.Body)
		if len(body) > 300 {
			body = body[:300] + "…"
		}
		return fmt.Errorf("%s %s: status %d: %s", h.provider.DisplayName(), what, apiErr.Status, body)
	}
	return fmt.Errorf("%s %s: %w", h.provider.DisplayName(), what, err)
}

// postForAudio POSTs a JSON body and returns the raw audio answer, retrying
// 429 and 503 the way postJSON does.
func (h speechHTTP) postForAudio(ctx context.Context, endpoint string, headers map[string]string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	delays := h.delays()
	for attempt := 0; ; attempt++ {
		resp, err := h.postOnce(ctx, endpoint, headers, payload)
		if err != nil {
			return nil, err
		}
		if resp.status >= 200 && resp.status <= 299 {
			if len(resp.body) == 0 {
				return nil, errNoAudio
			}
			return resp.body, nil
		}
		apiErr := &imageAPIError{Status: resp.status, Body: string(resp.body)}
		retryable := resp.status == http.StatusTooManyRequests || resp.status == http.StatusServiceUnavailable
		if !retryable || attempt >= len(delays) {
			return nil, apiErr
		}
		wait := delays[attempt]
		if resp.retryAfter > 0 {
			wait = min(resp.retryAfter, maxImageRetryAfter)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("cancelled while retrying after %w: %w", apiErr, ctx.Err())
		case <-timer.C:
		}
	}
}

var (
	// errNoAudio is an answer that should have carried audio and didn't.
	errNoAudio = errors.New("the answer had no audio")
	// errNoTranscript is an answer that should have carried text and didn't.
	errNoTranscript = errors.New("the answer had no transcript")
)

// postMultipart sends one multipart form, the file last, and returns the 2xx
// body; a non-2xx answer is an *imageAPIError. Uploads are not retried: a clip
// is the person's, and a second copy is not sent on a guess.
func (h speechHTTP) postMultipart(ctx context.Context, endpoint string, headers map[string]string, fields map[string]string, fileField, fileName, fileType string, file []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	part, err := w.CreatePart(map[string][]string{
		"Content-Disposition": {fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, fileName)},
		"Content-Type":        {fileType},
	})
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(file); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return h.send(ctx, http.MethodPost, endpoint, w.FormDataContentType(), headers, &buf)
}

// send makes one request and returns the 2xx body.
func (h speechHTTP) send(ctx context.Context, method, endpoint, contentType string, headers map[string]string, body io.Reader) ([]byte, error) {
	resp, err := h.sendFull(ctx, method, endpoint, contentType, headers, body)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

type speechResponse struct {
	body   []byte
	header http.Header
}

func (h speechHTTP) sendFull(ctx context.Context, method, endpoint, contentType string, headers map[string]string, body io.Reader) (speechResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return speechResponse{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.httpClient().Do(req)
	if err != nil {
		return speechResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSpeechAudioBytes+1))
	if err != nil {
		return speechResponse{}, err
	}
	if len(raw) > maxSpeechAudioBytes {
		return speechResponse{}, fmt.Errorf("answer larger than %d bytes", maxSpeechAudioBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return speechResponse{}, &imageAPIError{Status: resp.StatusCode, Body: string(raw)}
	}
	return speechResponse{body: raw, header: resp.Header}, nil
}

// downloadAudio fetches an audio file a provider answered with a link to. Only
// https is followed, as for images.
func (h speechHTTP) downloadAudio(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("refusing a non-https audio link")
	}
	return h.send(ctx, http.MethodGet, u.String(), "", nil, nil)
}

func bearer(key string) map[string]string {
	if key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + key}
}

// sniffAudioFormat names the container of data, for answers that don't say.
func sniffAudioFormat(data []byte, fallback models.AudioFormat) models.AudioFormat {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return models.AudioFormatWAV
	case len(data) >= 3 && string(data[0:3]) == "ID3",
		len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0:
		return models.AudioFormatMP3
	case len(data) >= 4 && string(data[0:4]) == "OggS":
		return models.AudioFormatOgg
	case len(data) >= 4 && string(data[0:4]) == "fLaC":
		return models.AudioFormatFLAC
	}
	return fallback
}

// fileExtension is the extension a clip of format f is uploaded under; some
// servers go by it.
func fileExtension(f models.AudioFormat) string {
	if f == models.AudioFormatMP4 {
		return "m4a"
	}
	return string(f)
}
