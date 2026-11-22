package provider

import (
	"context"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// qwenSpeechClient is Alibaba Model Studio. Synthesis is the native DashScope
// SpeechSynthesizer, which takes the delivery as a separate instruction and
// answers with a link to the audio (valid 24 hours), fetched before returning.
// Transcription (Qwen3 ASR) is compatible-mode chat completions with the clip
// as an input_audio part.
//
// The key is the connection's, or set by the transport's Qwen host rule (Bearer).
type qwenSpeechClient struct {
	speechHTTP
	compatBase string // ".../compatible-mode/v1"
	nativeBase string // ".../api/v1", same host
	key        string
}

// qwenSampleRate is asked for explicitly: the API's default is 22.05 kHz.
const qwenSampleRate = 24000

type qwenSpeechRequest struct {
	Model string          `json:"model"`
	Input qwenSpeechInput `json:"input"`
}

type qwenSpeechInput struct {
	Text        string `json:"text"`
	Voice       string `json:"voice"`
	Format      string `json:"format,omitempty"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

type qwenSpeechResponse struct {
	Output struct {
		Audio struct {
			URL  string `json:"url"`
			Data string `json:"data"`
		} `json:"audio"`
	} `json:"output"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		Characters   int   `json:"characters"`
	} `json:"usage"`
}

func (c *qwenSpeechClient) Synthesize(ctx context.Context, req SpeechRequest) (SpeechAudio, SpeechUsage, error) {
	format := req.Format
	if format != models.AudioFormatWAV {
		format = models.AudioFormatMP3
	}
	body := qwenSpeechRequest{Model: req.Model, Input: qwenSpeechInput{
		Text: req.Text, Voice: req.Voice, Format: string(format), SampleRate: qwenSampleRate,
		Instruction: strings.TrimSpace(req.Style),
	}}
	var out qwenSpeechResponse
	if err := c.postJSON(ctx, c.nativeBase+"/services/audio/tts/SpeechSynthesizer", bearer(c.key), body, &out); err != nil {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", err)
	}
	var data []byte
	switch {
	case out.Output.Audio.Data != "":
		decoded, err := base64.StdEncoding.DecodeString(out.Output.Audio.Data)
		if err != nil {
			return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", err)
		}
		data = decoded
	case out.Output.Audio.URL != "":
		downloaded, err := c.downloadAudio(ctx, out.Output.Audio.URL)
		if err != nil {
			return SpeechAudio{}, SpeechUsage{}, c.speechError("speech download", err)
		}
		data = downloaded
	}
	if len(data) == 0 {
		return SpeechAudio{}, SpeechUsage{}, c.speechError("speech", errNoAudio)
	}
	chars := out.Usage.Characters
	if chars == 0 {
		chars = utf8.RuneCountInString(req.Text)
	}
	return SpeechAudio{Data: data, Format: sniffAudioFormat(data, format)},
		SpeechUsage{Characters: chars, InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}

type qwenASRRequest struct {
	Model      string           `json:"model"`
	Messages   []qwenASRMessage `json:"messages"`
	Stream     bool             `json:"stream"`
	ASROptions *qwenASROptions  `json:"asr_options,omitempty"`
}

type qwenASRMessage struct {
	Role    string          `json:"role"`
	Content []qwenAudioPart `json:"content"`
}

type qwenAudioPart struct {
	Type       string         `json:"type"`
	InputAudio qwenInputAudio `json:"input_audio"`
}

type qwenInputAudio struct {
	Data string `json:"data"`
}

type qwenASROptions struct {
	Language string `json:"language,omitempty"`
}

type qwenASRResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *qwenSpeechClient) Transcribe(ctx context.Context, req TranscriptionRequest) (string, TranscriptionUsage, error) {
	body := qwenASRRequest{
		Model: req.Model,
		Messages: []qwenASRMessage{{Role: "user", Content: []qwenAudioPart{{
			Type:       "input_audio",
			InputAudio: qwenInputAudio{Data: "data:" + req.Format.MIMEType() + ";base64," + base64.StdEncoding.EncodeToString(req.Audio)},
		}}}},
	}
	if lang := qwenLanguage(req.Language); lang != "" {
		body.ASROptions = &qwenASROptions{Language: lang}
	}
	var out qwenASRResponse
	if err := c.postJSON(ctx, joinURL(c.compatBase, "chat/completions"), bearer(c.key), body, &out); err != nil {
		return "", TranscriptionUsage{}, c.speechError("transcription", err)
	}
	if len(out.Choices) == 0 {
		return "", TranscriptionUsage{}, c.speechError("transcription", errNoTranscript)
	}
	return strings.TrimSpace(out.Choices[0].Message.Content),
		TranscriptionUsage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}, nil
}

// qwenLanguage is the primary subtag of a BCP-47 hint ("en-US" becomes "en"),
// which is what Qwen ASR's language option takes.
func qwenLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		tag = tag[:i]
	}
	return tag
}
