package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// fakeWAV and fakeMP3 are enough of each container for sniffAudioFormat.
var (
	fakeWAV = append([]byte("RIFF\x00\x00\x00\x00WAVEfmt "), make([]byte, 16)...)
	fakeMP3 = append([]byte("ID3\x04\x00"), make([]byte, 16)...)
)

// recorded is one request a fake provider saw.
type recorded struct {
	method, path, query string
	header              http.Header
	body                []byte
}

type fakeProvider struct {
	mu   sync.Mutex
	seen []recorded
}

func (f *fakeProvider) record(r *http.Request) recorded {
	body, _ := io.ReadAll(r.Body)
	rec := recorded{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body}
	f.mu.Lock()
	f.seen = append(f.seen, rec)
	f.mu.Unlock()
	return rec
}

func (f *fakeProvider) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.seen...)
}

func newSpeechClient(t *testing.T, adapter models.SpeechAdapter, ep SpeechEndpoint) SpeechClient {
	t.Helper()
	c, err := NewSpeechClient(adapter, ep)
	require.NoError(t, err)
	// No real waits in tests.
	switch v := c.(type) {
	case *openAIAudioClient:
		v.retryDelays = []time.Duration{time.Millisecond}
	case *mistralSpeechClient:
		v.retryDelays = []time.Duration{time.Millisecond}
	case *geminiSpeechClient:
		v.retryDelays = []time.Duration{time.Millisecond}
	case *qwenSpeechClient:
		v.retryDelays = []time.Duration{time.Millisecond}
	}
	return c
}

func TestOpenAIAudio_Synthesize(t *testing.T) {
	fake := &fakeProvider{}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.record(r)
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // retried
			return
		}
		_, _ = w.Write(fakeMP3)
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterOpenAIAudio, SpeechEndpoint{Provider: models.ModelProviderOpenAI, BaseURL: srv.URL + "/v1", Key: "sk-conn", HTTP: srv.Client()})

	audio, usage, err := c.Synthesize(context.Background(), SpeechRequest{Model: "kokoro", Text: "Héllo there.", Voice: "af_bella", Style: "  warm  "})
	require.NoError(t, err)
	require.Equal(t, models.AudioFormatMP3, audio.Format)
	require.Equal(t, fakeMP3, audio.Data)
	require.Equal(t, 12, usage.Characters, "runes, not bytes")

	seen := fake.requests()
	require.Len(t, seen, 2)
	require.Equal(t, "/v1/audio/speech", seen[1].path)
	require.Equal(t, "Bearer sk-conn", seen[1].header.Get("Authorization"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(seen[1].body, &body))
	require.Equal(t, map[string]any{"model": "kokoro", "input": "Héllo there.", "voice": "af_bella", "response_format": "mp3", "instructions": "warm"}, body)

	// No style, no instructions field; no key, no header (the transport's job).
	c = newSpeechClient(t, models.SpeechAdapterOpenAIAudio, SpeechEndpoint{Provider: models.ModelProviderOpenAI, BaseURL: srv.URL + "/v1", HTTP: srv.Client()})
	_, _, err = c.Synthesize(context.Background(), SpeechRequest{Model: "kokoro", Text: "Hi", Voice: "af_bella"})
	require.NoError(t, err)
	last := fake.requests()[2]
	require.NotContains(t, string(last.body), "instructions")
	require.Empty(t, last.header.Get("Authorization"))
}

func TestOpenAIAudio_Transcribe(t *testing.T) {
	fake := &fakeProvider{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1<<20))
		require.Equal(t, "gpt-transcribe", r.FormValue("model"))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		data, _ := io.ReadAll(file)
		require.Equal(t, []byte("clip-bytes"), data)
		require.Equal(t, "clip.m4a", header.Filename)
		require.Equal(t, "audio/mp4", header.Header.Get("Content-Type"))
		fake.mu.Lock()
		fake.seen = append(fake.seen, recorded{path: r.URL.Path})
		fake.mu.Unlock()
		_, _ = w.Write([]byte(`{"text":" Buy milk. ","usage":{"input_tokens":12,"output_tokens":3}}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterOpenAIAudio, SpeechEndpoint{Provider: models.ModelProviderOpenAI, BaseURL: srv.URL + "/v1", HTTP: srv.Client()})

	text, usage, err := c.Transcribe(context.Background(), TranscriptionRequest{Model: "gpt-transcribe", Audio: []byte("clip-bytes"), Format: models.AudioFormatMP4})
	require.NoError(t, err)
	require.Equal(t, "Buy milk.", text)
	require.Equal(t, TranscriptionUsage{InputTokens: 12, OutputTokens: 3}, usage)
	require.Equal(t, "/v1/audio/transcriptions", fake.requests()[0].path)
}

func TestSpeech_FailuresNameTheProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"no such voice"}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterOpenAIAudio, SpeechEndpoint{Provider: models.ModelProviderMistral, BaseURL: srv.URL, HTTP: srv.Client()})
	_, _, err := c.Synthesize(context.Background(), SpeechRequest{Model: "m", Text: "x", Voice: "nope"})
	require.ErrorContains(t, err, "Mistral speech: status 400")
	require.ErrorContains(t, err, "no such voice")
}

func TestMistral_Synthesize(t *testing.T) {
	fake := &fakeProvider{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.record(r)
		_ = json.NewEncoder(w).Encode(map[string]string{"audio_data": base64.StdEncoding.EncodeToString(fakeMP3)})
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterMistral, SpeechEndpoint{Provider: models.ModelProviderMistral, BaseURL: srv.URL + "/v1", HTTP: srv.Client()})

	audio, _, err := c.Synthesize(context.Background(), SpeechRequest{Model: "voxtral-mini-tts-3-0", Text: "Hi", Voice: "gb_jane_neutral", Style: "ignored"})
	require.NoError(t, err)
	require.Equal(t, fakeMP3, audio.Data)
	var body map[string]any
	require.NoError(t, json.Unmarshal(fake.requests()[0].body, &body))
	require.Equal(t, map[string]any{"model": "voxtral-mini-tts-3-0", "input": "Hi", "voice_id": "gb_jane_neutral", "response_format": "mp3"}, body)
}

func TestGemini_Synthesize(t *testing.T) {
	fake := &fakeProvider{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.record(r)
		_, _ = w.Write([]byte(`{"steps":[
			{"type":"user_input","content":[{"type":"text","text":"Hello."}]},
			{"type":"model_output","content":[{"type":"audio","data":"` + base64.StdEncoding.EncodeToString(fakeWAV) + `"}]}
		],"usage":{"total_input_tokens":5,"total_output_tokens":40}}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterGemini, SpeechEndpoint{Provider: models.ModelProviderGoogle, BaseURL: srv.URL + "/v1beta/openai/", Key: "g-key", HTTP: srv.Client()})

	audio, usage, err := c.Synthesize(context.Background(), SpeechRequest{Model: "gemini-3.8-flash-tts", Text: "Hello.", Voice: "Kore", Style: "warm, unhurried"})
	require.NoError(t, err)
	require.Equal(t, models.AudioFormatWAV, audio.Format)
	require.Equal(t, fakeWAV, audio.Data)
	require.Equal(t, SpeechUsage{Characters: 6, InputTokens: 5, OutputTokens: 40}, usage)

	req := fake.requests()[0]
	require.Equal(t, "/v1beta/interactions", req.path)
	require.Equal(t, "g-key", req.header.Get("x-goog-api-key"))
	require.Empty(t, req.header.Get("Authorization"), "Google refuses a native request that also carries a Bearer token")
	require.JSONEq(t, `{
		"model":"gemini-3.8-flash-tts",
		"input":[{"type":"user_input","content":[{"type":"text","text":"Hello.","annotations":[{"type":"speech_metadata","style":"warm, unhurried"}]}]}],
		"response_format":{"type":"audio","mime_type":"audio/wav"},
		"generation_config":{"speech_config":[{"voice":"Kore"}]}
	}`, string(req.body))
}

func TestGemini_SynthesizeWithoutAudioFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"steps":[{"type":"model_output","content":[{"type":"text","text":"I can't do that."}]}]}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterGemini, SpeechEndpoint{Provider: models.ModelProviderGoogle, BaseURL: srv.URL + "/v1beta/openai/", HTTP: srv.Client()})
	_, _, err := c.Synthesize(context.Background(), SpeechRequest{Model: "m", Text: "x", Voice: "Kore"})
	require.ErrorContains(t, err, "no audio")
}

// A clip is uploaded through the Files API, transcribed, and the upload deleted.
func TestGemini_Transcribe(t *testing.T) {
	fake := &fakeProvider{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := fake.record(r)
		switch {
		case r.URL.Path == "/upload/v1beta/files" && r.Header.Get("X-Goog-Upload-Command") == "start":
			require.Equal(t, "resumable", r.Header.Get("X-Goog-Upload-Protocol"))
			require.Equal(t, "10", r.Header.Get("X-Goog-Upload-Header-Content-Length"))
			require.Equal(t, "audio/webm", r.Header.Get("X-Goog-Upload-Header-Content-Type"))
			w.Header().Set("X-Goog-Upload-URL", srv.URL+"/upload/v1beta/files?upload_id=u1")
		case r.URL.Path == "/upload/v1beta/files" && r.URL.Query().Get("upload_id") == "u1":
			require.Equal(t, "upload, finalize", r.Header.Get("X-Goog-Upload-Command"))
			require.Equal(t, []byte("clip-bytes"), rec.body)
			_, _ = w.Write([]byte(`{"file":{"name":"files/abc","uri":"` + srv.URL + `/v1beta/files/abc","state":"PROCESSING"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1beta/files/abc":
			_, _ = w.Write([]byte(`{"name":"files/abc","uri":"` + srv.URL + `/v1beta/files/abc","state":"ACTIVE"}`))
		case r.URL.Path == "/v1beta/interactions":
			_, _ = w.Write([]byte(`{"steps":[{"type":"model_output","content":[{"type":"text","text":" Call mum. "}]}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1beta/files/abc":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	saved := geminiFileStatePolls
	geminiFileStatePolls = []time.Duration{time.Millisecond}
	defer func() { geminiFileStatePolls = saved }()
	c := newSpeechClient(t, models.SpeechAdapterGemini, SpeechEndpoint{Provider: models.ModelProviderGoogle, BaseURL: srv.URL + "/v1beta/openai/", Key: "g-key", HTTP: srv.Client()})

	text, _, err := c.Transcribe(context.Background(), TranscriptionRequest{Model: "gemini-3.5-transcribe", Audio: []byte("clip-bytes"), Format: models.AudioFormatWebM})
	require.NoError(t, err)
	require.Equal(t, "Call mum.", text)

	seen := fake.requests()
	require.Len(t, seen, 5)
	var interaction map[string]any
	require.NoError(t, json.Unmarshal(seen[3].body, &interaction))
	require.Equal(t, "gemini-3.5-transcribe", interaction["model"])
	require.Equal(t, []any{map[string]any{"type": "file", "file_uri": srv.URL + "/v1beta/files/abc"}}, interaction["input"])
	require.Equal(t, http.MethodDelete, seen[4].method, "the upload is deleted")
	for _, r := range seen {
		require.Equal(t, "g-key", r.header.Get("x-goog-api-key"), r.path)
	}
}

// An upload URL on another host is refused: the key would go with the clip.
func TestGemini_TranscribeRefusesUploadURLOnAnotherHost(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Goog-Upload-URL", other.URL+"/upload/v1beta/files?upload_id=u1")
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterGemini, SpeechEndpoint{Provider: models.ModelProviderGoogle, BaseURL: srv.URL + "/v1beta/openai/", Key: "g-key", HTTP: srv.Client()})

	_, _, err := c.Transcribe(context.Background(), TranscriptionRequest{Model: "gemini-3.5-transcribe", Audio: []byte("clip-bytes"), Format: models.AudioFormatWebM})
	require.Error(t, err)
	require.Zero(t, elsewhere.Load(), "nothing is sent to the other host")
}

func TestGeminiSpeechCredentialPaths(t *testing.T) {
	require.Equal(t, []string{
		"https://generativelanguage.googleapis.com/v1beta/interactions",
		"https://generativelanguage.googleapis.com/v1beta/files",
		"https://generativelanguage.googleapis.com/upload/v1beta/files",
	}, GeminiSpeechCredentialPaths(""))
}

func TestQwen_Synthesize(t *testing.T) {
	fake := &fakeProvider{}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.record(r)
		if r.URL.Path == "/audio/result.mp3" {
			_, _ = w.Write(fakeMP3)
			return
		}
		_, _ = w.Write([]byte(`{"output":{"audio":{"data":"","url":"` + srv.URL + `/audio/result.mp3"},"finish_reason":"stop"},
			"usage":{"input_tokens":9,"output_tokens":120,"characters":11}}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterQwen, SpeechEndpoint{Provider: models.ModelProviderQwen, BaseURL: srv.URL + "/compatible-mode/v1", Key: "q-key", HTTP: srv.Client()})

	audio, usage, err := c.Synthesize(context.Background(), SpeechRequest{Model: "qwen-audio-3.1-tts-flash", Text: "Good night.", Voice: "Emily_v3.1", Style: "a whisper", Format: models.AudioFormatMP3})
	require.NoError(t, err)
	require.Equal(t, fakeMP3, audio.Data)
	require.Equal(t, models.AudioFormatMP3, audio.Format)
	require.Equal(t, SpeechUsage{Characters: 11, InputTokens: 9, OutputTokens: 120}, usage)

	seen := fake.requests()
	require.Equal(t, "/api/v1/services/audio/tts/SpeechSynthesizer", seen[0].path, "the native API on the connection's host")
	require.Equal(t, "Bearer q-key", seen[0].header.Get("Authorization"))
	require.JSONEq(t, `{"model":"qwen-audio-3.1-tts-flash","input":{"text":"Good night.","voice":"Emily_v3.1","format":"mp3","sample_rate":24000,"instruction":"a whisper"}}`, string(seen[0].body))
	require.Equal(t, "/audio/result.mp3", seen[1].path)
}

func TestQwen_SynthesizeRefusesAPlainHTTPLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"audio":{"url":"http://example.com/a.mp3"}}}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterQwen, SpeechEndpoint{Provider: models.ModelProviderQwen, BaseURL: srv.URL + "/compatible-mode/v1", HTTP: srv.Client()})
	_, _, err := c.Synthesize(context.Background(), SpeechRequest{Model: "m", Text: "x", Voice: "v"})
	require.ErrorContains(t, err, "non-https")
}

func TestQwen_Transcribe(t *testing.T) {
	fake := &fakeProvider{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.record(r)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Pick up the kids."}}],"usage":{"prompt_tokens":50,"completion_tokens":6}}`))
	}))
	defer srv.Close()
	c := newSpeechClient(t, models.SpeechAdapterQwen, SpeechEndpoint{Provider: models.ModelProviderQwen, BaseURL: srv.URL + "/compatible-mode/v1", HTTP: srv.Client()})

	text, usage, err := c.Transcribe(context.Background(), TranscriptionRequest{Model: "qwen3-asr-flash", Audio: []byte("wav!"), Format: models.AudioFormatWAV, Language: "en-GB"})
	require.NoError(t, err)
	require.Equal(t, "Pick up the kids.", text)
	require.Equal(t, TranscriptionUsage{InputTokens: 50, OutputTokens: 6}, usage)
	req := fake.requests()[0]
	require.Equal(t, "/compatible-mode/v1/chat/completions", req.path)
	require.JSONEq(t, `{"model":"qwen3-asr-flash","stream":false,"asr_options":{"language":"en"},
		"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"data:audio/wav;base64,`+base64.StdEncoding.EncodeToString([]byte("wav!"))+`"}}]}]}`, string(req.body))
}

func TestNewSpeechClient_UnknownAdapter(t *testing.T) {
	_, err := NewSpeechClient("carrier-pigeon", SpeechEndpoint{})
	require.Error(t, err)
}

func TestSniffAudioFormat(t *testing.T) {
	require.Equal(t, models.AudioFormatWAV, sniffAudioFormat(fakeWAV, models.AudioFormatMP3))
	require.Equal(t, models.AudioFormatMP3, sniffAudioFormat([]byte{0xFF, 0xFB, 0x90, 0x00}, models.AudioFormatWAV))
	require.Equal(t, models.AudioFormatOgg, sniffAudioFormat([]byte("OggS...."), models.AudioFormatMP3))
	require.Equal(t, models.AudioFormatMP3, sniffAudioFormat([]byte("????"), models.AudioFormatMP3), "falls back")
	require.True(t, strings.HasPrefix(fileExtension(models.AudioFormatMP4), "m4a"))
}
