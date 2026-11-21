package agent

// Voice replies (ADR 0x052 §5–§7): a personality's voice, a message's speech track,
// the worker that synthesises a track's segments, voice previews, and the sweeper
// that deletes tracks nobody plays.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/metering"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/providerkeys"
	"github.com/theimaginaryfoundation/what-iff/internal/speechtext"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"
)

var (
	// ErrNoVoice is a reply whose personality has no voice.
	ErrNoVoice = errors.New("this personality has no voice")
	// ErrVoiceUnavailable is a voice that can't be used: its model turned off or gone,
	// its key removed, or the voice no longer offered.
	ErrVoiceUnavailable = errors.New("this personality's voice isn't available")
	// ErrNotAReply is a message that isn't an assistant reply.
	ErrNotAReply = errors.New("only replies are read aloud")
	// ErrPreviewTooLong is preview text past models.MaxPreviewChars.
	ErrPreviewTooLong = fmt.Errorf("a preview is at most %d characters", models.MaxPreviewChars)
)

// speechWorkerLease is how long a worker holds a track between segments; a track still
// open past it has lost its worker (the instance went away), and the next request for it
// starts another.
const speechWorkerLease = 90 * time.Second

// speechRetention is how long a track's audio is kept after it was last played.
const speechRetention = 30 * 24 * time.Hour

// voice is a personality's voice as its client reaches it.
type voice struct {
	target speechTarget
	model  *models.Model
	voice  string
	// style is the personality's style when the model takes one; empty otherwise.
	style string
}

// voiceKey identifies a voice for a track: a message has one track per voice.
func (v voice) key() string {
	sum := sha256.Sum256([]byte(v.model.ID.String() + "\x00" + v.voice + "\x00" + v.style))
	return hex.EncodeToString(sum[:16])
}

// resolveVoice is pv as it can be used now, or ErrVoiceUnavailable saying why not.
func (a *Agent) resolveVoice(ctx context.Context, userID uuid.UUID, pv models.PersonalityVoice) (voice, error) {
	m, err := a.ds.GetModel(ctx, pv.ModelID)
	if errors.Is(err, datastore.ErrModelNotFound) {
		return voice{}, fmt.Errorf("%w: its model is turned off or gone", ErrVoiceUnavailable)
	}
	if err != nil {
		return voice{}, err
	}
	if m.EffectiveKind() != models.ModelKindSpeech {
		return voice{}, fmt.Errorf("%w: %s is not a voice model", ErrVoiceUnavailable, m.DisplayName)
	}
	if !slices.ContainsFunc(m.SpeechVoices, func(o models.SpeechVoice) bool { return o.ID == pv.Voice }) {
		return voice{}, fmt.Errorf("%w: %s no longer offers that voice", ErrVoiceUnavailable, m.DisplayName)
	}
	conn, err := a.connectionFor(ctx, userID, m)
	if err != nil {
		return voice{}, fmt.Errorf("%w: %w", ErrVoiceUnavailable, err)
	}
	if conn == nil && a.keyResolver != nil {
		p := models.ProviderForModel(m.Provider, m.Name)
		if a.keyResolver.Key(ctx, p) == "" {
			return voice{}, fmt.Errorf("%w: %w", ErrVoiceUnavailable, &providerkeys.NotConfiguredError{Provider: p})
		}
	}
	t := utilityTarget{name: m.Name, provider: m.Provider, connection: conn, kind: models.ModelKindSpeech}
	if conn != nil {
		t.provider = conn.Preset
	}
	st, err := a.speechTargetFor(t)
	if err != nil {
		return voice{}, fmt.Errorf("%w: %w", ErrVoiceUnavailable, err)
	}
	v := voice{target: st, model: m, voice: pv.Voice}
	if st.spec.Style || t.connection != nil && !isKnownSpeech(st) {
		// A model the table doesn't know may take a style; OpenAI's route sends it as
		// instructions, which servers that don't take one ignore.
		v.style = pv.Style
	}
	return v, nil
}

func isKnownSpeech(st speechTarget) bool {
	_, ok := models.LookupKnownSpeechModel(models.ModelProvider(st.provider), st.model)
	return ok
}

// maxSegmentChars is the most text in one segment: the model's limit, or 1,000.
func (v voice) maxSegmentChars() int {
	if v.target.spec.MaxInputChars > 0 {
		return v.target.spec.MaxInputChars
	}
	return 1000
}

// Listen is the message's speech track in its chat's personality's voice, started if
// it isn't there yet (ADR 0x052 §7). A track still being made, with no worker on it
// (its instance went away), gets one.
func (a *Agent) Listen(ctx context.Context, userID, messageID uuid.UUID) (*models.SpeechTrack, error) {
	msg, err := a.ds.GetChatMessage(ctx, userID, messageID)
	if err != nil {
		return nil, err
	}
	if msg.Origin != models.MessageOriginAssistant {
		return nil, ErrNotAReply
	}
	chat, err := a.ds.GetChat(ctx, userID, msg.ChatID)
	if err != nil {
		return nil, err
	}
	if chat.PersonalityID == uuid.Nil {
		return nil, ErrNoVoice
	}
	p, err := a.ds.GetPersonality(ctx, userID, chat.PersonalityID)
	if err != nil {
		return nil, err
	}
	if p.Voice == nil {
		return nil, ErrNoVoice
	}
	v, err := a.resolveVoice(ctx, userID, *p.Voice)
	if err != nil {
		return nil, err
	}
	track, _, err := a.ds.FindOrCreateSpeechTrack(ctx, datastore.NewSpeechTrack{
		UserID: userID, ChatID: msg.ChatID, MessageID: msg.ID, ModelID: v.model.ID,
		Voice: v.voice, Style: v.style, VoiceKey: v.key(),
	})
	if err != nil {
		return nil, err
	}
	if track.Status == models.SpeechTrackFailed || track.Status == models.SpeechTrackStopped {
		// Asked again: try the rest. Out of credits still, the meter stops it again.
		reopened, err := a.ds.ReopenSpeechTrack(ctx, track.ID)
		if err != nil {
			return nil, err
		}
		if reopened {
			track.Status, track.ErrorCode = models.SpeechTrackOpen, ""
		}
	}
	if track.Status == models.SpeechTrackOpen {
		a.startSpeechWorker(userID, track, v, speechtext.Segment(speechtext.Spoken(msg.Message, msg.FinalRoundOffset), v.maxSegmentChars()))
	}
	return track, nil
}

// startSpeechWorker synthesises the track's remaining segments in the background, if
// it can take the track's lease; another worker holding it is left to finish.
func (a *Agent) startSpeechWorker(userID uuid.UUID, track *models.SpeechTrack, v voice, segments []string) {
	base := a.lifecycleCtx
	if base == nil {
		base = context.Background()
	}
	// The account's context, so the credential transport finds its keys.
	ctx := apicontext.WithUserID(base, userID)
	ok, err := a.ds.AcquireSpeechWorker(ctx, track.ID, time.Now().Add(speechWorkerLease))
	if err != nil {
		a.logger.Warn("couldn't take a speech track", zap.String("track_id", track.ID.String()), zap.Error(err))
		return
	}
	if !ok {
		return
	}
	go a.runSpeechWorker(ctx, userID, track, v, segments)
}

func (a *Agent) runSpeechWorker(ctx context.Context, userID uuid.UUID, track *models.SpeechTrack, v voice, segments []string) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("speech worker panicked", zap.String("track_id", track.ID.String()), zap.Any("panic", r))
			_ = a.ds.FinishSpeechTrack(context.WithoutCancel(ctx), track.ID, models.SpeechTrackFailed, "internal_error")
		}
	}()
	status, code := a.synthesizeTrack(ctx, userID, track, v, segments)
	if err := a.ds.FinishSpeechTrack(context.WithoutCancel(ctx), track.ID, status, code); err != nil {
		a.logger.Warn("couldn't finish a speech track", zap.String("track_id", track.ID.String()), zap.Error(err))
	}
}

// synthesizeTrack makes each segment the track doesn't have yet, in order, and says how
// the track ends.
func (a *Agent) synthesizeTrack(ctx context.Context, userID uuid.UUID, track *models.SpeechTrack, v voice, segments []string) (models.SpeechTrackStatus, string) {
	client, err := provider.NewSpeechClient(v.target.spec.Adapter, v.target.endpoint)
	if err != nil {
		return models.SpeechTrackFailed, "internal_error"
	}
	format := preferredSpeechFormat(v.target.spec)
	for seq := len(track.Segments); seq < len(segments); seq++ {
		if err := ctx.Err(); err != nil {
			return models.SpeechTrackFailed, "cancelled"
		}
		if err := a.ds.RenewSpeechWorker(ctx, track.ID, time.Now().Add(speechWorkerLease)); err != nil {
			return models.SpeechTrackFailed, "internal_error"
		}
		text := segments[seq]
		decision, allowed, err := a.checkSpeechQuota(ctx, userID)
		if err != nil {
			return models.SpeechTrackFailed, "internal_error"
		}
		if !allowed {
			return models.SpeechTrackStopped, models.ErrCodeQuotaExceeded
		}
		audio, usage, err := a.synthesize(ctx, client, v, text, format)
		if err != nil {
			a.logger.Warn("speech synthesis failed", zap.String("track_id", track.ID.String()), zap.Int("seq", seq), zap.Error(err))
			return models.SpeechTrackFailed, speechFailureCode(err)
		}
		key := path.Join("users", userID.String(), "speech", track.ID.String(), fmt.Sprintf("%04d.%s", seq, audio.Format))
		if err := a.fileStore.UploadFile(ctx, key, audio.Data, audio.Format.MIMEType()); err != nil {
			a.logger.Warn("couldn't store speech", zap.String("track_id", track.ID.String()), zap.Error(err))
			return models.SpeechTrackFailed, "storage_error"
		}
		seg := models.SpeechSegment{Seq: seq, Text: text, FileKey: key, Format: audio.Format, DurationMS: audioDurationMS(audio), SizeBytes: len(audio.Data)}
		if err := a.ds.AddSpeechSegment(ctx, track.ID, seg); err != nil {
			return models.SpeechTrackFailed, "internal_error"
		}
		a.recordSpeech(ctx, decision, userID, track.ChatID, v, usage)
	}
	return models.SpeechTrackComplete, ""
}

// preferredSpeechFormat is MP3 where the model returns it (small, and every browser
// plays it), else the model's own.
func preferredSpeechFormat(spec models.KnownSpeechModel) models.AudioFormat {
	if len(spec.Formats) == 0 || slices.Contains(spec.Formats, models.AudioFormatMP3) {
		return models.AudioFormatMP3
	}
	return spec.Formats[0]
}

func (a *Agent) synthesize(ctx context.Context, client provider.SpeechClient, v voice, text string, format models.AudioFormat) (provider.SpeechAudio, provider.SpeechUsage, error) {
	start := time.Now()
	audio, usage, err := client.Synthesize(ctx, provider.SpeechRequest{Model: v.target.model, Text: text, Voice: v.voice, Style: v.style, Format: format})
	attrs := []attribute.KeyValue{telemetry.AttrGenAIProvider.String(v.target.provider), telemetry.AttrGenAIModel.String(v.target.model), telemetry.AttrOperation.String("synthesis")}
	a.metrics().Record(ctx, telemetry.SpeechDuration, time.Since(start).Seconds(), attrs...)
	if err != nil {
		a.metrics().Add(ctx, telemetry.SpeechFailures, 1, attrs...)
	}
	return audio, usage, err
}

// checkSpeechQuota asks the meter about one synthesis; like images, speech isn't priced by
// the chat model's tier.
func (a *Agent) checkSpeechQuota(ctx context.Context, userID uuid.UUID) (metering.Decision, bool, error) {
	if a.meter == nil {
		return metering.Decision{Allowed: true}, true, nil
	}
	d := a.meter.Check(ctx, userID, "", models.ActionTypeSpeechSynthesis)
	if !d.Allowed && d.Err != nil {
		return d, false, d.Err
	}
	if !d.Allowed {
		a.recordQuotaRejection(ctx)
	}
	return d, d.Allowed, nil
}

func (a *Agent) recordSpeech(ctx context.Context, d metering.Decision, userID, chatID uuid.UUID, v voice, usage provider.SpeechUsage) {
	if a.meter == nil {
		return
	}
	u := metering.Usage{
		UserID: userID, ActionType: models.ActionTypeSpeechSynthesis, Model: v.target.model,
		SpeechProvider: v.target.provider, SpeechCharacters: usage.Characters, Source: metering.TurnSourceApp,
	}
	if chatID != uuid.Nil {
		u.ChatID = chatID.String()
	}
	a.meter.Record(ctx, d, u)
}

// speechFailureCode is the stable code a failed track carries.
func speechFailureCode(err error) string {
	var missing *providerkeys.NotConfiguredError
	if errors.As(err, &missing) {
		return models.ErrCodeProviderNotConfigured
	}
	return "provider_error"
}

// audioDurationMS is a segment's length where its container says (WAV), else zero: the
// player measures the rest as it decodes them.
func audioDurationMS(audio provider.SpeechAudio) int {
	if audio.Format != models.AudioFormatWAV {
		return 0
	}
	s, ok := wavSeconds(audio.Data)
	if !ok {
		return 0
	}
	return int(s * 1000)
}

// SpeechSegmentAudio is one segment's stored audio, for the account's own track.
func (a *Agent) SpeechSegmentAudio(ctx context.Context, userID, trackID uuid.UUID, seq int) ([]byte, models.AudioFormat, error) {
	track, err := a.ds.GetSpeechTrack(ctx, userID, trackID)
	if err != nil {
		return nil, "", err
	}
	for _, s := range track.Segments {
		if s.Seq != seq {
			continue
		}
		data, err := a.fileStore.DownloadFile(ctx, s.FileKey)
		if err != nil {
			return nil, "", err
		}
		if data == nil {
			return nil, "", datastore.ErrSpeechTrackNotFound
		}
		return data, s.Format, nil
	}
	return nil, "", datastore.ErrSpeechTrackNotFound
}

// SpeechTrack is the account's track, for the client polling it.
func (a *Agent) SpeechTrack(ctx context.Context, userID, trackID uuid.UUID) (*models.SpeechTrack, error) {
	return a.ds.GetSpeechTrack(ctx, userID, trackID)
}

// PreviewVoice reads text aloud in pv, for the personality page to try a voice before
// keeping it. It is billed like any synthesis, and not stored.
func (a *Agent) PreviewVoice(ctx context.Context, userID uuid.UUID, pv models.PersonalityVoice, text string) (provider.SpeechAudio, error) {
	if utf8.RuneCountInString(text) > models.MaxPreviewChars {
		return provider.SpeechAudio{}, ErrPreviewTooLong
	}
	if err := pv.ValidateStyle(); err != nil {
		return provider.SpeechAudio{}, err
	}
	text = speechtext.Prepare(text)
	if text == "" {
		return provider.SpeechAudio{}, fmt.Errorf("%w: nothing to read", ErrPreviewTooLong)
	}
	v, err := a.resolveVoice(ctx, userID, pv)
	if err != nil {
		return provider.SpeechAudio{}, err
	}
	decision, allowed, err := a.checkSpeechQuota(ctx, userID)
	if err != nil {
		return provider.SpeechAudio{}, err
	}
	if !allowed {
		return provider.SpeechAudio{}, fmt.Errorf("%w for user %s", ErrQuotaExceeded, userID)
	}
	client, err := provider.NewSpeechClient(v.target.spec.Adapter, v.target.endpoint)
	if err != nil {
		return provider.SpeechAudio{}, err
	}
	audio, usage, err := a.synthesize(ctx, client, v, text, preferredSpeechFormat(v.target.spec))
	if err != nil {
		return provider.SpeechAudio{}, err
	}
	a.recordSpeech(ctx, decision, userID, uuid.Nil, v, usage)
	return audio, nil
}

// SweepSpeech deletes the audio nobody plays (ADR 0x052 §7): tracks unplayed for 30
// days, and tracks whose message is gone, files first. It runs until ctx ends.
func (a *Agent) SweepSpeech(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		a.sweepSpeechOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Agent) sweepSpeechOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("speech sweeper panicked", zap.Any("panic", r))
		}
	}()
	tracks, err := a.ds.SweepableSpeechTracks(ctx, time.Now().Add(-speechRetention), 200)
	if err != nil {
		a.logger.Warn("couldn't list speech to sweep", zap.Error(err))
		return
	}
	for _, t := range tracks {
		kept := false
		for _, key := range t.Files {
			if err := a.fileStore.DeleteFile(ctx, key); err != nil {
				a.logger.Warn("couldn't delete speech audio", zap.String("key", key), zap.Error(err))
				kept = true
			}
		}
		if kept {
			continue // try again next sweep, rather than strand the file
		}
		if err := a.ds.DeleteSpeechTrack(ctx, t.ID); err != nil {
			a.logger.Warn("couldn't delete a speech track", zap.String("track_id", t.ID.String()), zap.Error(err))
		}
	}
}
