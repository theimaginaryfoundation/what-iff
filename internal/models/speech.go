package models

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// ModelKind is what a model does (ADR 0x052 §2). Every model before speech is a
// chat model; speech and transcription models are never offered for chat, and a
// chat model is never offered as a voice or for dictation.
type ModelKind string

const (
	ModelKindChat          ModelKind = "chat"
	ModelKindSpeech        ModelKind = "speech"
	ModelKindTranscription ModelKind = "transcription"
)

// Valid reports whether k is a known kind.
func (k ModelKind) Valid() bool {
	switch k {
	case ModelKindChat, ModelKindSpeech, ModelKindTranscription:
		return true
	}
	return false
}

// IsAudio reports whether k is a speech or transcription kind.
func (k ModelKind) IsAudio() bool {
	return k == ModelKindSpeech || k == ModelKindTranscription
}

// SpeechVoice is one base voice a speech model offers.
type SpeechVoice = modeltypes.SpeechVoice

// SpeechAdapter names the client that talks to a speech or transcription model
// (ADR 0x052 §3).
type SpeechAdapter string

const (
	// SpeechAdapterOpenAIAudio speaks OpenAI's audio routes: /audio/speech and
	// multipart /audio/transcriptions. OpenAI (transcription), Mistral and Z.AI
	// (transcription), OpenRouter, Ollama and Custom connections.
	SpeechAdapterOpenAIAudio SpeechAdapter = "openai_audio"
	// SpeechAdapterGemini speaks the Gemini API's native v1beta Interactions API.
	SpeechAdapterGemini SpeechAdapter = "gemini"
	// SpeechAdapterQwen speaks DashScope's native speech synthesis, and Qwen ASR
	// through compatible-mode chat completions.
	SpeechAdapterQwen SpeechAdapter = "qwen"
	// SpeechAdapterMistral speaks Mistral's /audio/speech, which takes voice_id
	// and answers with base64 JSON rather than audio bytes.
	SpeechAdapterMistral SpeechAdapter = "mistral"
)

// AudioFormat is an audio container a speech model returns or a transcription
// model accepts.
type AudioFormat string

const (
	AudioFormatMP3  AudioFormat = "mp3"
	AudioFormatWAV  AudioFormat = "wav"
	AudioFormatWebM AudioFormat = "webm" // Opus in WebM: Chromium and Firefox recordings
	AudioFormatMP4  AudioFormat = "mp4"  // AAC in MP4/M4A: Safari recordings
	AudioFormatOgg  AudioFormat = "ogg"
	AudioFormatFLAC AudioFormat = "flac"
)

// MIMEType is the format's media type.
func (f AudioFormat) MIMEType() string {
	switch f {
	case AudioFormatMP3:
		return "audio/mpeg"
	case AudioFormatWAV:
		return "audio/wav"
	case AudioFormatWebM:
		return "audio/webm"
	case AudioFormatMP4:
		return "audio/mp4"
	case AudioFormatOgg:
		return "audio/ogg"
	case AudioFormatFLAC:
		return "audio/flac"
	}
	return "application/octet-stream"
}

// KnownSpeechModel is what the app knows about one speech or transcription
// model that a provider's listing does not say (ADR 0x052 §2).
type KnownSpeechModel struct {
	Provider    ModelProvider
	ID          string
	DisplayName string
	Kind        ModelKind
	Adapter     SpeechAdapter
	// Voices are the model's base voices (speech models only).
	Voices []SpeechVoice
	// Style is whether the model takes a free-text style instruction.
	Style bool
	// MaxInputChars is the most text one synthesis request carries (speech).
	MaxInputChars int
	// MaxClipSeconds and MaxClipBytes bound one transcription clip; zero is no
	// limit the provider documents.
	MaxClipSeconds int
	MaxClipBytes   int64
	// Formats are the formats a speech model returns, preferred first, or the
	// formats a transcription model accepts.
	Formats []AudioFormat
}

// AcceptsFormat reports whether a transcription model takes f.
func (m KnownSpeechModel) AcceptsFormat(f AudioFormat) bool {
	return slices.Contains(m.Formats, f)
}

func voices(names ...string) []SpeechVoice {
	out := make([]SpeechVoice, len(names))
	for i, n := range names {
		out[i] = SpeechVoice{ID: n, Name: n}
	}
	return out
}

// geminiVoices are the Gemini 3.8 TTS models' 30 prebuilt voices.
var geminiVoices = voices(
	"Zephyr", "Puck", "Charon", "Kore", "Fenrir", "Leda", "Orus", "Aoede", "Callirrhoe", "Autonoe",
	"Enceladus", "Iapetus", "Umbriel", "Algieba", "Despina", "Erinome", "Algenib", "Rasalgethi", "Laomedeia", "Achernar",
	"Alnilam", "Schedar", "Gacrux", "Pulcherrima", "Achird", "Zubenelgenubi", "Vindemiatrix", "Sadachbia", "Sadaltager", "Sulafat",
)

// qwenAudio31Voices are qwen-audio-3.1-tts-flash system voices for English and
// the multilingual set. The model has many more (mostly Mandarin and dialect
// voices); a deployment that wants them adds them by id.
var qwenAudio31Voices = []SpeechVoice{
	{ID: "Emily_v3.1", Name: "Emily"}, {ID: "Luna_v3.1", Name: "Luna"}, {ID: "Eric_v3.1", Name: "Eric"},
	{ID: "Luca_v3.1", Name: "Luca"}, {ID: "Abby_v3.1", Name: "Abby"}, {ID: "Annie_v3.1", Name: "Annie"},
	{ID: "Ava_v3.1", Name: "Ava"}, {ID: "Beth_v3.1", Name: "Beth"}, {ID: "Betty_v3.1", Name: "Betty"},
	{ID: "Cally_v3.1", Name: "Cally"}, {ID: "Cindy_v3.1", Name: "Cindy"}, {ID: "Donna_v3.1", Name: "Donna"},
	{ID: "Andy_v3.1", Name: "Andy"}, {ID: "Brian_v3.1", Name: "Brian"}, {ID: "David_v3.1", Name: "David"},
	{ID: "loongbella_v3.1", Name: "Bella (Mandarin and English)"},
	{ID: "longanhuan_v3.1", Name: "Anhuan (multilingual)"}, {ID: "longanlingxin_v3.1", Name: "Anlingxin (multilingual)"},
	{ID: "longanfengyue_v3.1", Name: "Anfengyue (multilingual)"}, {ID: "xunanchuan_v3.1", Name: "Nanchuan (multilingual)"},
}

// broadTranscriptionFormats are what browsers record in natively, plus the common files.
var broadTranscriptionFormats = []AudioFormat{
	AudioFormatWebM, AudioFormatMP4, AudioFormatOgg, AudioFormatWAV, AudioFormatMP3, AudioFormatFLAC,
}

// knownSpeechModels is the known speech models table (ADR 0x052 §2). It is kept
// by hand: voice catalogues and model ids change faster than chat models do.
// OpenAI's synthesis models are absent on purpose: they shut down 2027-01-06.
var knownSpeechModels = []KnownSpeechModel{
	{
		Provider: ModelProviderGoogle, ID: "gemini-3.8-flash-tts", DisplayName: "Gemini 3.8 Flash TTS",
		Kind: ModelKindSpeech, Adapter: SpeechAdapterGemini, Voices: geminiVoices, Style: true,
		MaxInputChars: 4000, Formats: []AudioFormat{AudioFormatWAV},
	},
	{
		Provider: ModelProviderGoogle, ID: "gemini-3.8-flash-lite-tts", DisplayName: "Gemini 3.8 Flash-Lite TTS",
		Kind: ModelKindSpeech, Adapter: SpeechAdapterGemini, Voices: geminiVoices, Style: true,
		MaxInputChars: 4000, Formats: []AudioFormat{AudioFormatWAV},
	},
	{
		Provider: ModelProviderQwen, ID: "qwen-audio-3.1-tts-flash", DisplayName: "Qwen Audio 3.1 TTS Flash",
		Kind: ModelKindSpeech, Adapter: SpeechAdapterQwen, Voices: qwenAudio31Voices, Style: true,
		MaxInputChars: 600, Formats: []AudioFormat{AudioFormatMP3, AudioFormatWAV},
	},
	{
		Provider: ModelProviderMistral, ID: "voxtral-mini-tts-3-0", DisplayName: "Voxtral Mini TTS 3",
		Kind: ModelKindSpeech, Adapter: SpeechAdapterMistral, Voices: voices("gb_jane_neutral"),
		MaxInputChars: 2000, Formats: []AudioFormat{AudioFormatMP3, AudioFormatWAV},
	},
	{
		Provider: ModelProviderOpenAI, ID: "gpt-transcribe", DisplayName: "GPT Transcribe",
		Kind: ModelKindTranscription, Adapter: SpeechAdapterOpenAIAudio,
		MaxClipBytes: 25 << 20, Formats: broadTranscriptionFormats,
	},
	{
		Provider: ModelProviderGoogle, ID: "gemini-3.5-transcribe", DisplayName: "Gemini 3.5 Transcribe",
		Kind: ModelKindTranscription, Adapter: SpeechAdapterGemini,
		MaxClipSeconds: 3600, Formats: broadTranscriptionFormats,
	},
	{
		Provider: ModelProviderQwen, ID: "qwen3-asr-flash", DisplayName: "Qwen3 ASR Flash",
		Kind: ModelKindTranscription, Adapter: SpeechAdapterQwen,
		// 10 MB of base64 is about 7.5 MB of audio.
		MaxClipSeconds: 300, MaxClipBytes: 7 << 20, Formats: []AudioFormat{AudioFormatWAV, AudioFormatMP3},
	},
	{
		Provider: ModelProviderMistral, ID: "voxtral-mini-transcribe-3-0", DisplayName: "Voxtral Mini Transcribe 3",
		Kind: ModelKindTranscription, Adapter: SpeechAdapterOpenAIAudio,
		Formats: []AudioFormat{AudioFormatWAV, AudioFormatMP3},
	},
	{
		Provider: ModelProviderZAI, ID: "glm-asr-2512", DisplayName: "GLM ASR",
		Kind: ModelKindTranscription, Adapter: SpeechAdapterOpenAIAudio,
		MaxClipSeconds: 30, MaxClipBytes: 25 << 20, Formats: []AudioFormat{AudioFormatWAV, AudioFormatMP3},
	},
}

// KnownSpeechModels returns the known speech and transcription models a
// built-in provider offers, in table order.
func KnownSpeechModels(provider ModelProvider) []KnownSpeechModel {
	var out []KnownSpeechModel
	for _, m := range knownSpeechModels {
		if m.Provider == provider {
			out = append(out, m)
		}
	}
	return out
}

// LookupKnownSpeechModel returns the table's entry for a built-in provider's
// model id.
func LookupKnownSpeechModel(provider ModelProvider, id string) (KnownSpeechModel, bool) {
	for _, m := range knownSpeechModels {
		if m.Provider == provider && m.ID == id {
			return m, true
		}
	}
	return KnownSpeechModel{}, false
}

// IsKnownSpeechModelID reports whether id is in the table for any provider,
// so a listing filter that drops audio models can keep the ones we can use.
func IsKnownSpeechModelID(id string) bool {
	return slices.ContainsFunc(knownSpeechModels, func(m KnownSpeechModel) bool { return m.ID == id })
}

const (
	// MaxSpeechVoices bounds the voices one speech model offers.
	MaxSpeechVoices     = 200
	maxSpeechVoiceIDLen = 200
	maxSpeechVoiceName  = 100
)

var (
	// ErrInvalidSpeechVoices is returned for a speech model with no voices, too
	// many, or a voice with a missing or overlong id or name.
	ErrInvalidSpeechVoices = fmt.Errorf("a voice model needs 1 to %d voices, each with an id of at most %d characters and a name of at most %d", MaxSpeechVoices, maxSpeechVoiceIDLen, maxSpeechVoiceName)
	// ErrInvalidModelKind is returned for a kind other than chat, speech or transcription.
	ErrInvalidModelKind = errors.New("a model's kind must be chat, speech or transcription")
)

// NormalizeSpeechVoices trims each voice, names an unnamed one after its id,
// drops repeated ids, and refuses an empty, overlong or oversized list.
func NormalizeSpeechVoices(in []SpeechVoice) ([]SpeechVoice, error) {
	if len(in) == 0 || len(in) > MaxSpeechVoices {
		return nil, ErrInvalidSpeechVoices
	}
	seen := make(map[string]bool, len(in))
	out := make([]SpeechVoice, 0, len(in))
	for _, v := range in {
		id, name := strings.TrimSpace(v.ID), strings.TrimSpace(v.Name)
		if name == "" {
			name = id
		}
		if id == "" || len(id) > maxSpeechVoiceIDLen || len(name) > maxSpeechVoiceName {
			return nil, ErrInvalidSpeechVoices
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, SpeechVoice{ID: id, Name: name})
	}
	return out, nil
}

// ResolveSpeechModel decides a new model's kind and voices: the known table's
// kind for a model it knows, else the requested kind, else chat; for a speech
// model, the requested voices, else the table's. A speech model must end up
// with at least one voice.
func ResolveSpeechModel(provider ModelProvider, id string, kind ModelKind, requested []SpeechVoice) (ModelKind, []SpeechVoice, error) {
	known, isKnown := LookupKnownSpeechModel(provider, id)
	switch {
	case isKnown:
		kind = known.Kind
	case kind == "":
		kind = ModelKindChat
	case !kind.Valid():
		return "", nil, ErrInvalidModelKind
	}
	if kind != ModelKindSpeech {
		return kind, nil, nil
	}
	if requested != nil {
		v, err := NormalizeSpeechVoices(requested)
		return kind, v, err
	}
	if isKnown && len(known.Voices) > 0 {
		return kind, append([]SpeechVoice(nil), known.Voices...), nil
	}
	return "", nil, ErrInvalidSpeechVoices
}
