package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The known speech models table is kept by hand, so check what every entry
// must have for the adapters and pickers to use it.
func TestKnownSpeechModels_AreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range knownSpeechModels {
		key := string(m.Provider) + "/" + m.ID
		require.False(t, seen[key], "%s listed twice", key)
		seen[key] = true
		require.True(t, m.Kind.IsAudio(), "%s: a known speech model is speech or transcription", key)
		require.NotEmpty(t, m.Adapter, key)
		require.NotEmpty(t, m.DisplayName, key)
		require.NotEmpty(t, m.Formats, key)
		if m.Kind == ModelKindSpeech {
			require.NotEmpty(t, m.Voices, "%s: a speech model offers voices", key)
			require.Positive(t, m.MaxInputChars, key)
			ids := map[string]bool{}
			for _, v := range m.Voices {
				require.NotEmpty(t, v.ID, key)
				require.NotEmpty(t, v.Name, key)
				require.False(t, ids[v.ID], "%s: voice %s twice", key, v.ID)
				ids[v.ID] = true
			}
		} else {
			require.Empty(t, m.Voices, "%s: only speech models have voices", key)
		}
	}
}

// OpenAI's synthesis models shut down 2027-01-06 with no verbatim successor
// (ADR 0x052), so none is offered.
func TestKnownSpeechModels_NoOpenAISynthesis(t *testing.T) {
	for _, m := range KnownSpeechModels(ModelProviderOpenAI) {
		require.NotEqual(t, ModelKindSpeech, m.Kind, m.ID)
	}
}

func TestLookupKnownSpeechModel(t *testing.T) {
	m, ok := LookupKnownSpeechModel(ModelProviderGoogle, "gemini-3.8-flash-tts")
	require.True(t, ok)
	require.Equal(t, ModelKindSpeech, m.Kind)
	require.True(t, m.Style)

	_, ok = LookupKnownSpeechModel(ModelProviderOpenAI, "gemini-3.8-flash-tts")
	require.False(t, ok, "a model is known only under its own provider")

	q, ok := LookupKnownSpeechModel(ModelProviderQwen, "qwen3-asr-flash")
	require.True(t, ok)
	require.True(t, q.AcceptsFormat(AudioFormatWAV))
	require.False(t, q.AcceptsFormat(AudioFormatWebM), "browsers' WebM needs WAV capture for Qwen")
}

func TestModelKind(t *testing.T) {
	require.True(t, ModelKindChat.Valid())
	require.False(t, ModelKind("image").Valid())
	require.False(t, ModelKindChat.IsAudio())
	require.True(t, ModelKindTranscription.IsAudio())
	var m *Model
	require.Equal(t, ModelKindChat, m.EffectiveKind())
	require.Equal(t, ModelKindChat, (&Model{}).EffectiveKind())
	require.Equal(t, ModelKindSpeech, (&Model{Kind: ModelKindSpeech}).EffectiveKind())
}
