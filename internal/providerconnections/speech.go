package providerconnections

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Speech and transcription models on a connection (ADR 0x052 §2).
//
// Provider listings say nothing about voices, styles or limits, so a built-in
// provider's speech models come from the known speech models table, listed
// beside its chat models whether or not the provider's own listing has them.
// On OpenRouter, Ollama and Custom connections, whose ids are open-ended, the
// account adds a speech or transcription model by id, typing its voices.

var (
	// ErrInvalidVoices and ErrInvalidModelKind are the models package's, named
	// here for the routes that answer them.
	ErrInvalidVoices    = models.ErrInvalidSpeechVoices
	ErrInvalidModelKind = models.ErrInvalidModelKind
)

// withKnownSpeechModels marks and adds a built-in provider's known speech and
// transcription models in its listing. A listed id the table knows takes the
// table's kind and voices; a known model the provider doesn't list is added.
func withKnownSpeechModels(preset Preset, listed []ListedModel) []ListedModel {
	if !preset.Builtin {
		return listed
	}
	for _, known := range models.KnownSpeechModels(models.ModelProvider(preset.ID)) {
		found := false
		for i := range listed {
			if listed[i].ID == known.ID {
				markSpeech(&listed[i], known)
				found = true
				break
			}
		}
		if !found {
			m := ListedModel{ID: known.ID, DisplayName: known.DisplayName}
			markSpeech(&m, known)
			listed = append(listed, m)
		}
	}
	return listed
}

func markSpeech(m *ListedModel, known models.KnownSpeechModel) {
	m.Kind = known.Kind
	if m.DisplayName == "" {
		m.DisplayName = known.DisplayName
	}
	if known.Kind == models.ModelKindSpeech {
		m.Voices = append([]models.SpeechVoice(nil), known.Voices...)
		style := known.Style
		m.Style = &style
	}
	// A speech model takes no tools or images.
	no := false
	m.ToolSupport, m.VisionSupport = &no, &no
}

// needsListing reports whether enabling c for the first time must find it in
// the provider's listing:
//
//   - a built-in provider's known speech model (Qwen's TTS, which its
//     compatible-mode listing lacks): no, the table vouches for it;
//   - a speech or transcription model added by id on OpenRouter, Ollama or
//     Custom: no, such servers seldom list them;
//   - any chat model, on any connection: yes, as before.
//
// Known speech models are added to a built-in provider's listing after its own
// models (withKnownSpeechModels); the chooser groups by kind, not position.
func needsListing(preset Preset, c ModelChoice) bool {
	if _, ok := models.LookupKnownSpeechModel(models.ModelProvider(preset.ID), strings.TrimSpace(c.ID)); ok && preset.Builtin {
		return false
	}
	return preset.Builtin || !c.Kind.IsAudio()
}

// listedChoices keeps the choices whose first enabling must be in the listing.
func listedChoices(preset Preset, choices []ModelChoice) []ModelChoice {
	out := make([]ModelChoice, 0, len(choices))
	for _, c := range choices {
		if needsListing(preset, c) {
			out = append(out, c)
		}
	}
	return out
}
