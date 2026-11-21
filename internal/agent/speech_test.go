package agent

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// wavClip is a 16 kHz mono 16-bit WAV of the given length.
func wavClip(seconds float64) []byte {
	const byteRate = 16000 * 2
	n := int(seconds * byteRate)
	b := make([]byte, 44+n)
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], uint32(36+n))
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)
	binary.LittleEndian.PutUint16(b[20:22], 1)
	binary.LittleEndian.PutUint16(b[22:24], 1)
	binary.LittleEndian.PutUint32(b[24:28], 16000)
	binary.LittleEndian.PutUint32(b[28:32], byteRate)
	binary.LittleEndian.PutUint16(b[32:34], 2)
	binary.LittleEndian.PutUint16(b[34:36], 16)
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], uint32(n))
	return b
}

// dictationModel adds a transcription model on alice's Ollama connection,
// served at the returned test server, which answers text.
func (f *connectionFixture) dictationModel(t *testing.T, text string) (*ent.Model, *atomic.Int32) {
	t.Helper()
	ctx := context.Background()
	conn, err := f.ent.ProviderConnection.Create().SetUserID(f.alice).SetPreset("custom").SetName("Speaches").Save(ctx)
	require.NoError(t, err)
	f.mu.Lock()
	f.owners[conn.ID] = f.alice
	f.mu.Unlock()
	m, err := f.ent.Model.Create().SetName("whisper-large").SetDisplayName("Whisper").SetDescription("").
		SetProvider("custom").SetConnectionID(conn.ID).SetKind("transcription").Save(ctx)
	require.NoError(t, err)
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		require.Equal(t, "Bearer conn-key", r.Header.Get("Authorization"))
		require.NoError(t, r.ParseMultipartForm(1<<24))
		file, _, err := r.FormFile("file")
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, file)
		_, _ = w.Write([]byte(`{"text":"` + text + `"}`))
	}))
	t.Cleanup(srv.Close)
	f.serve(m, ConnectionEndpoint{Preset: "custom", Flavor: ConnectionFlavorOpenAI, BaseURL: srv.URL + "/v1", Key: "conn-key"})
	f.a.connectionClient = srv.Client()
	return m, calls
}

func TestTranscribe_OnTheAccountsDictationModel(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	m, calls := f.dictationModel(t, " Remind me at five. ")
	meter := &recordingMeter{allow: true}
	f.a.meter = meter
	alice := apicontext.WithUserID(context.Background(), f.alice)

	_, err := f.a.Transcribe(alice, f.alice, DictationClip{Audio: wavClip(1), Format: models.AudioFormatWAV})
	require.ErrorIs(t, err, ErrUtilityRoleNotSet, "no microphone until a dictation model is chosen")
	require.Empty(t, meter.checks)

	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleTranscription, m.ID)
	d, err := f.a.Dictation(alice)
	require.NoError(t, err)
	require.Equal(t, "whisper-large", d.Model)
	require.Equal(t, maxDictationSeconds, d.MaxSeconds)
	require.Contains(t, d.Formats, models.AudioFormatWebM, "a server the table doesn't know takes what OpenAI's route does")

	chatID := uuid.NewString()
	text, err := f.a.Transcribe(alice, f.alice, DictationClip{Audio: wavClip(2.5), Format: models.AudioFormatWAV, ClientSeconds: 0.1, ChatID: chatID})
	require.NoError(t, err)
	require.Equal(t, "Remind me at five.", text)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, []string{models.ActionTypeTranscription}, meter.checks)
	require.Len(t, meter.records, 1)
	rec := meter.records[0]
	require.Equal(t, models.ActionTypeTranscription, rec.ActionType)
	require.Equal(t, "whisper-large", rec.Model)
	require.Equal(t, "custom", rec.SpeechProvider)
	require.Equal(t, chatID, rec.ChatID)
	require.InDelta(t, 2.5, rec.AudioSeconds, 0.001, "a WAV's length comes from its header, not the browser")
}

func TestTranscribe_OutOfCreditsSendsNothing(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	m, calls := f.dictationModel(t, "x")
	f.choose(t, models.UtilityScopeAccount, f.alice, models.UtilityRoleTranscription, m.ID)
	meter := &recordingMeter{allow: false}
	f.a.meter = meter
	alice := apicontext.WithUserID(context.Background(), f.alice)

	_, err := f.a.Transcribe(alice, f.alice, DictationClip{Audio: wavClip(1), Format: models.AudioFormatWAV})
	require.ErrorIs(t, err, ErrQuotaExceeded)
	require.Zero(t, calls.Load(), "nothing is sent to the provider")
	require.Empty(t, meter.records)
}

// A role runs only on a model of its kind (ADR 0x052): dictation on a
// transcription model, the rest on chat models.
func TestDictationRole_TakesOnlyTranscriptionModels(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	m, _ := f.dictationModel(t, "x")
	ctx := context.Background()

	err := f.a.ds.SetUtilityModel(ctx, f.alice, models.UtilityModelSetting{Scope: models.UtilityScopeAccount, Role: models.UtilityRoleTranscription, ModelID: &f.aliceModel.ID})
	require.ErrorIs(t, err, datastore.ErrModelNotFound, "a chat model can't dictate")
	err = f.a.ds.SetUtilityModel(ctx, f.alice, models.UtilityModelSetting{Scope: models.UtilityScopeAccount, Role: models.UtilityRoleSimple, ModelID: &m.ID})
	require.ErrorIs(t, err, datastore.ErrModelNotFound, "a transcription model can't name chats")
	err = f.a.ds.SetUtilityModel(ctx, uuid.Nil, models.UtilityModelSetting{Scope: models.UtilityScopeDeployment, Role: models.UtilityRoleTranscription, ModelID: &f.catalog.ID})
	require.ErrorIs(t, err, datastore.ErrModelNotFound, "nor for the deployment")

	// A setting that names the wrong kind anyway (written around the checks) is unusable.
	_, err = f.ent.UtilityModelSetting.Create().SetScope("account").SetUserID(f.alice).SetRole("transcription").SetModelID(f.aliceModel.ID).Save(ctx)
	require.NoError(t, err)
	_, err = f.a.Dictation(apicontext.WithUserID(ctx, f.alice))
	require.ErrorIs(t, err, ErrModelDisabled)
}

// A clip the model can't take is refused before the meter or the provider.
func TestTranscribe_RefusesClipsTheModelCantTake(t *testing.T) {
	t.Parallel()
	f := newConnectionFixture(t)
	ctx := context.Background()
	qwen, err := f.ent.Model.Create().SetName("qwen3-asr-flash").SetDisplayName("Qwen ASR").SetDescription("").
		SetProvider("qwen").SetKind("transcription").Save(ctx)
	require.NoError(t, err)
	f.choose(t, models.UtilityScopeDeployment, uuid.Nil, models.UtilityRoleTranscription, qwen.ID)
	f.a.speechEndpoints = buildSpeechEndpoints(AgentConfig{})
	meter := &recordingMeter{allow: true}
	f.a.meter = meter
	alice := apicontext.WithUserID(ctx, f.alice)

	d, err := f.a.Dictation(alice)
	require.NoError(t, err)
	require.Equal(t, []models.AudioFormat{models.AudioFormatWAV, models.AudioFormatMP3}, d.Formats, "the composer records WAV for it")

	_, err = f.a.Transcribe(alice, f.alice, DictationClip{Audio: []byte("webm"), Format: models.AudioFormatWebM})
	require.ErrorIs(t, err, ErrAudioFormatUnsupported)
	_, err = f.a.Transcribe(alice, f.alice, DictationClip{Audio: wavClip(302), Format: models.AudioFormatWAV})
	require.ErrorIs(t, err, ErrClipTooLong, "five minutes is Qwen's limit")
	require.Empty(t, meter.checks)
}

func TestClipSeconds(t *testing.T) {
	t.Parallel()
	require.InDelta(t, 3.0, clipSeconds(DictationClip{Audio: wavClip(3), Format: models.AudioFormatWAV, ClientSeconds: 99}), 0.001, "a WAV header wins")
	require.InDelta(t, 12.0, clipSeconds(DictationClip{Audio: make([]byte, 16000), Format: models.AudioFormatWebM, ClientSeconds: 12}), 0.001)
	require.InDelta(t, 10.0, clipSeconds(DictationClip{Audio: make([]byte, 160000), Format: models.AudioFormatWebM, ClientSeconds: 0}), 0.001,
		"a claimed length can't be shorter than the clip's size allows")

	streamed := wavClip(2)
	binary.LittleEndian.PutUint32(streamed[40:44], 0) // a recorder that never filled in the size
	s, ok := wavSeconds(streamed)
	require.True(t, ok)
	require.InDelta(t, 2.0, s, 0.001)
	_, ok = wavSeconds([]byte("not a wav"))
	require.False(t, ok)
}
