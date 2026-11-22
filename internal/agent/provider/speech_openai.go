package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// openAIAudioClient speaks OpenAI's audio routes: POST {base}/audio/speech for
// synthesis and multipart POST {base}/audio/transcriptions for transcription.
// OpenAI itself (transcription only: its synthesis models shut down 2027-01-06),
// Mistral and Z.AI transcription, OpenRouter, Ollama and Custom connections.
type openAIAudioClient struct {
	speechHTTP
	baseURL string
	key     string
}

type openAISpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format,omitempty"`
	Instructions   string `json:"instructions,omitempty"`
}

func (c *openAIAudioClient) Synthesize(ctx context.Context, req SpeechRequest) (SpeechAudio, SpeechUsage, error) {
	format := req.Format
	if format == "" {
		format = models.AudioFormatMP3
	}
	body := openAISpeechRequest{
		Model: req.Model, Input: req.Text, Voice: req.Voice,
		ResponseFormat: string(format), Instructions: strings.TrimSpace(req.Style),
	}
	data, err := c.postForAudio(ctx, joinURL(c.baseURL, "audio/speech"), bearer(c.key), body)
	if err != nil {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", err)
	}
	return SpeechAudio{Data: data, Format: sniffAudioFormat(data, format)},
		SpeechUsage{Characters: utf8.RuneCountInString(req.Text)}, nil
}

type openAITranscription struct {
	Text  string `json:"text"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Transcribe sends the clip with no language hint: OpenAI's newest model takes
// a list of languages where others take one, and detection is good enough that
// the hint isn't worth a field per server.
func (c *openAIAudioClient) Transcribe(ctx context.Context, req TranscriptionRequest) (string, TranscriptionUsage, error) {
	raw, err := c.postMultipart(ctx, joinURL(c.baseURL, "audio/transcriptions"), bearer(c.key),
		map[string]string{"model": req.Model},
		"file", "clip."+fileExtension(req.Format), req.Format.MIMEType(), req.Audio)
	if err != nil {
		return "", TranscriptionUsage{}, c.speechError("transcription", err)
	}
	var out openAITranscription
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", TranscriptionUsage{}, c.speechError("transcription", err)
	}
	return strings.TrimSpace(out.Text), TranscriptionUsage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}

// mistralSpeechClient is Mistral: its /audio/speech takes voice_id instead of
// voice and answers JSON with the audio in base64; transcription is OpenAI's
// multipart route.
type mistralSpeechClient struct {
	openAIAudioClient
}

type mistralSpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	VoiceID        string `json:"voice_id"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type mistralSpeechResponse struct {
	AudioData string `json:"audio_data"`
}

// Synthesize ignores the style: Voxtral steers delivery with inline tags, not
// an instruction.
func (c *mistralSpeechClient) Synthesize(ctx context.Context, req SpeechRequest) (SpeechAudio, SpeechUsage, error) {
	format := req.Format
	if format == "" {
		format = models.AudioFormatMP3
	}
	var out mistralSpeechResponse
	body := mistralSpeechRequest{Model: req.Model, Input: req.Text, VoiceID: req.Voice, ResponseFormat: string(format)}
	if err := c.postJSON(ctx, joinURL(c.baseURL, "audio/speech"), bearer(c.key), body, &out); err != nil {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", err)
	}
	data, err := base64.StdEncoding.DecodeString(out.AudioData)
	if err != nil || len(data) == 0 {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", errNoAudio)
	}
	return SpeechAudio{Data: data, Format: sniffAudioFormat(data, format)},
		SpeechUsage{Characters: utf8.RuneCountInString(req.Text)}, nil
}
