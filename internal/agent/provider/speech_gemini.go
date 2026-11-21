package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// geminiSpeechClient speaks the Gemini API's native v1beta Interactions API:
// Gemini 3.8 TTS treats its text strictly as a transcript and takes the
// delivery in a separate speech_metadata style, and Gemini 3.5 Transcribe reads
// a clip uploaded through the Files API.
//
// The key travels as x-goog-api-key: set here for a connection's own key, else
// by the credential transport's path-scoped Gemini rules (see
// GeminiSpeechCredentialPaths).
type geminiSpeechClient struct {
	speechHTTP
	nativeBase string // ".../v1beta"
	key        string
}

// GeminiSpeechCredentialPaths are the native API paths speech uses under the
// root for a configured OpenAI-compatible Gemini base URL, each needing the
// x-goog-api-key credential rule: the Interactions API, file metadata, and the
// Files API upload, which lives under /upload on the same host.
func GeminiSpeechCredentialPaths(openAICompatBase string) []string {
	native := GeminiNativeBaseURL(openAICompatBase)
	return []string{native + "/interactions", native + "/files", geminiUploadRoot(native) + "/files"}
}

// geminiUploadRoot is the Files API upload root for a native root:
// "https://host/v1beta" becomes "https://host/upload/v1beta".
func geminiUploadRoot(native string) string {
	u, err := url.Parse(native)
	if err != nil || u.Host == "" {
		return strings.Replace(native, "/v1beta", "/upload/v1beta", 1)
	}
	return u.Scheme + "://" + u.Host + "/upload" + strings.TrimRight(u.Path, "/")
}

func (c *geminiSpeechClient) headers() map[string]string {
	if c.key == "" {
		return nil
	}
	return map[string]string{"x-goog-api-key": c.key}
}

type geminiInteractionRequest struct {
	Model            string                      `json:"model"`
	Input            []geminiInteractionInput    `json:"input"`
	ResponseFormat   *geminiResponseFormat       `json:"response_format,omitempty"`
	GenerationConfig *geminiInteractionGenConfig `json:"generation_config,omitempty"`
}

type geminiInteractionInput struct {
	Type    string                     `json:"type"`
	Content []geminiInteractionContent `json:"content,omitempty"`
	FileURI string                     `json:"file_uri,omitempty"`
}

type geminiInteractionContent struct {
	Type        string                   `json:"type"`
	Text        string                   `json:"text,omitempty"`
	Annotations []geminiSpeechAnnotation `json:"annotations,omitempty"`
}

type geminiSpeechAnnotation struct {
	Type  string `json:"type"`
	Style string `json:"style,omitempty"`
}

type geminiResponseFormat struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type,omitempty"`
}

type geminiInteractionGenConfig struct {
	// SpeechConfig is a list for one speaker; multi-speaker takes an object.
	SpeechConfig []geminiVoice `json:"speech_config,omitempty"`
}

type geminiVoice struct {
	Voice string `json:"voice"`
}

type geminiInteraction struct {
	Steps []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Data string `json:"data"`
		} `json:"content"`
	} `json:"steps"`
	Usage struct {
		InputTokens  int64 `json:"total_input_tokens"`
		OutputTokens int64 `json:"total_output_tokens"`
	} `json:"usage"`
}

// Synthesize asks for WAV, the only container the TTS models return whole.
func (c *geminiSpeechClient) Synthesize(ctx context.Context, req SpeechRequest) (SpeechAudio, SpeechUsage, error) {
	content := geminiInteractionContent{Type: "text", Text: req.Text}
	if style := strings.TrimSpace(req.Style); style != "" {
		content.Annotations = []geminiSpeechAnnotation{{Type: "speech_metadata", Style: style}}
	}
	body := geminiInteractionRequest{
		Model:            req.Model,
		Input:            []geminiInteractionInput{{Type: "user_input", Content: []geminiInteractionContent{content}}},
		ResponseFormat:   &geminiResponseFormat{Type: "audio", MIMEType: "audio/wav"},
		GenerationConfig: &geminiInteractionGenConfig{SpeechConfig: []geminiVoice{{Voice: req.Voice}}},
	}
	var out geminiInteraction
	if err := c.postJSON(ctx, c.nativeBase+"/interactions", c.headers(), body, &out); err != nil {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", err)
	}
	var encoded string
	for _, step := range out.Steps {
		if step.Type != "model_output" {
			continue
		}
		for _, part := range step.Content {
			if part.Type == "audio" && part.Data != "" {
				encoded = part.Data // the last audio part is the answer
			}
		}
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", errNoAudio)
	}
	return SpeechAudio{Data: data, Format: sniffAudioFormat(data, models.AudioFormatWAV)},
		SpeechUsage{Characters: utf8.RuneCountInString(req.Text), InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}

type geminiFile struct {
	Name  string `json:"name"`
	URI   string `json:"uri"`
	State string `json:"state"`
}

// geminiFileStatePolls bounds the wait for an uploaded clip to become usable.
var geminiFileStatePolls = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

// Transcribe uploads the clip, transcribes it, and deletes the upload, which
// Google would otherwise keep for two days.
func (c *geminiSpeechClient) Transcribe(ctx context.Context, req TranscriptionRequest) (string, TranscriptionUsage, error) {
	file, err := c.upload(ctx, req.Audio, req.Format.MIMEType())
	if err != nil {
		return "", TranscriptionUsage{}, c.speechError("transcription upload", err)
	}
	defer c.deleteFile(ctx, file.Name)
	if file, err = c.waitActive(ctx, file); err != nil {
		return "", TranscriptionUsage{}, c.speechError("transcription upload", err)
	}
	body := geminiInteractionRequest{
		Model: req.Model,
		Input: []geminiInteractionInput{{Type: "file", FileURI: file.URI}},
	}
	var out geminiInteraction
	if err := c.postJSON(ctx, c.nativeBase+"/interactions", c.headers(), body, &out); err != nil {
		return "", TranscriptionUsage{}, c.speechError("transcription", err)
	}
	var text strings.Builder
	for _, step := range out.Steps {
		if step.Type != "model_output" && step.Type != "" {
			continue
		}
		for _, part := range step.Content {
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
	}
	return strings.TrimSpace(text.String()), TranscriptionUsage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}

// upload sends a clip with the Files API's resumable protocol in one chunk.
func (c *geminiSpeechClient) upload(ctx context.Context, data []byte, mimeType string) (geminiFile, error) {
	headers := map[string]string{
		"X-Goog-Upload-Protocol":              "resumable",
		"X-Goog-Upload-Command":               "start",
		"X-Goog-Upload-Header-Content-Length": strconv.Itoa(len(data)),
		"X-Goog-Upload-Header-Content-Type":   mimeType,
	}
	for k, v := range c.headers() {
		headers[k] = v
	}
	start, err := c.sendFull(ctx, http.MethodPost, geminiUploadRoot(c.nativeBase)+"/files", "application/json", headers,
		strings.NewReader(`{"file":{"display_name":"dictation"}}`))
	if err != nil {
		return geminiFile{}, err
	}
	uploadURL := start.header.Get("X-Goog-Upload-URL")
	if uploadURL == "" {
		return geminiFile{}, errors.New("no upload URL in the answer")
	}
	headers = map[string]string{
		"X-Goog-Upload-Offset":  "0",
		"X-Goog-Upload-Command": "upload, finalize",
	}
	for k, v := range c.headers() {
		headers[k] = v
	}
	raw, err := c.send(ctx, http.MethodPost, uploadURL, mimeType, headers, bytes.NewReader(data))
	if err != nil {
		return geminiFile{}, err
	}
	var out struct {
		File geminiFile `json:"file"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.File.Name == "" || out.File.URI == "" {
		return geminiFile{}, fmt.Errorf("unreadable upload answer: %w", err)
	}
	return out.File, nil
}

// waitActive waits for a file still processing to become active.
func (c *geminiSpeechClient) waitActive(ctx context.Context, f geminiFile) (geminiFile, error) {
	for _, wait := range geminiFileStatePolls {
		if f.State == "" || f.State == "ACTIVE" {
			return f, nil
		}
		if f.State == "FAILED" {
			return f, errors.New("the uploaded clip could not be processed")
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return f, ctx.Err()
		case <-timer.C:
		}
		raw, err := c.send(ctx, http.MethodGet, c.nativeBase+"/"+f.Name, "", c.headers(), nil)
		if err != nil {
			return f, err
		}
		var next geminiFile
		if err := json.Unmarshal(raw, &next); err != nil {
			return f, err
		}
		f = next
	}
	if f.State == "ACTIVE" {
		return f, nil
	}
	return f, errors.New("the uploaded clip was still processing")
}

// deleteFile removes an upload, best effort, on its own short deadline so a
// cancelled request still cleans up. It keeps the request's values: the
// credential transport finds the account's key there.
func (c *geminiSpeechClient) deleteFile(parent context.Context, name string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	_, _ = c.send(ctx, http.MethodDelete, c.nativeBase+"/"+name, "", c.headers(), nil)
}
