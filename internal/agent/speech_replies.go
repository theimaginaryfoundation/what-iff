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
	// The checks a personality's voice passed when it was set, again now: a voice model the
	// account can use (not another account's, not retired, opted into if experimental).
	if err := a.ds.CheckUserCanUseModelOfKind(ctx, userID, pv.ModelID, models.ModelKindSpeech); err != nil {
		if errors.Is(err, datastore.ErrModelNotFound) || errors.Is(err, datastore.ErrExperimentalModelNotAllowed) {
			return voice{}, fmt.Errorf("%w: its model is turned off or gone", ErrVoiceUnavailable)
		}
		return voice{}, err
	}
	m, err := a.ds.GetModel(ctx, pv.ModelID)
	if errors.Is(err, datastore.ErrModelNotFound) {
		return voice{}, fmt.Errorf("%w: its model is turned off or gone", ErrVoiceUnavailable)
	}
	if err != nil {
		return voice{}, err
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
		a.startSpeechWorker(userID, track.ID, v, speechtext.Spoken(msg.Message, msg.FinalRoundOffset))
	}
	return track, nil
}

// startSpeechWorker synthesises the rest of the track's text in the background, if it
// can take the track's lease; another worker holding it is left to finish. spoken is
// the prepared text the whole track reads (speechtext.Spoken).
func (a *Agent) startSpeechWorker(userID, trackID uuid.UUID, v voice, spoken string) {
	base := a.lifecycleCtx
	if base == nil {
		base = context.Background()
	}
	// The account's context, so the credential transport finds its keys.
	ctx := apicontext.WithUserID(base, userID)
	token, ok, err := a.ds.AcquireSpeechWorker(ctx, trackID, time.Now().Add(speechWorkerLease))
	if err != nil {
		a.logger.Warn("couldn't take a speech track", zap.String("track_id", trackID.String()), zap.Error(err))
		return
	}
	if !ok {
		return
	}
	go a.runSpeechWorker(ctx, userID, speechLease{trackID: trackID, token: token}, v, spoken)
}

// speechLease is a worker's hold on a track: every write it makes names its token.
type speechLease struct {
	trackID, token uuid.UUID
}

// holdSpeechLease renews the lease until ctx ends, so a synthesis call longer than the
// lease doesn't lose it; it cancels ctx when the lease goes to another worker.
func (a *Agent) holdSpeechLease(ctx context.Context, cancel context.CancelFunc, l speechLease) {
	t := time.NewTicker(speechWorkerLease / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		err := a.ds.RenewSpeechWorker(ctx, l.trackID, l.token, time.Now().Add(speechWorkerLease))
		if errors.Is(err, datastore.ErrSpeechLeaseLost) {
			cancel()
			return
		}
		if err != nil && ctx.Err() == nil {
			a.logger.Warn("couldn't renew a speech track's lease", zap.String("track_id", l.trackID.String()), zap.Error(err))
		}
	}
}

func (a *Agent) runSpeechWorker(ctx context.Context, userID uuid.UUID, l speechLease, v voice, spoken string) {
	finish := func(status models.SpeechTrackStatus, code string) {
		if err := a.ds.FinishSpeechTrack(context.WithoutCancel(ctx), l.trackID, l.token, status, code); err != nil {
			a.logger.Warn("couldn't finish a speech track", zap.String("track_id", l.trackID.String()), zap.Error(err))
		}
	}
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("speech worker panicked", zap.String("track_id", l.trackID.String()), zap.Any("panic", r))
			finish(models.SpeechTrackFailed, "internal_error")
		}
	}()
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.holdSpeechLease(work, cancel, l)
	status, code, err := a.synthesizeTrack(work, userID, l, v, spoken)
	if errors.Is(err, datastore.ErrSpeechLeaseLost) {
		return // another worker has the track now, or it's gone
	}
	if code == speechInterrupted {
		// Shutting down is not the track's end: leave it open for the next request to resume.
		if err := a.ds.ReleaseSpeechWorker(context.WithoutCancel(ctx), l.trackID, l.token); err != nil {
			a.logger.Warn("couldn't release a speech track", zap.String("track_id", l.trackID.String()), zap.Error(err))
		}
		return
	}
	finish(status, code)
}

// speechInterrupted is the code synthesizeTrack gives a track whose worker's context ended
// with the lease still held: the instance is shutting down. The track isn't finished with it.
const speechInterrupted = "cancelled"

// synthesizeTrack makes the segments for the text the track hasn't read yet, in order,
// and says how the track ends. It resumes from the text its segments read, not their
// number, so a track cut differently before (by the live speaker, or by older rules)
// carries on where it stopped; segments the text no longer starts with are withdrawn.
// ErrSpeechLeaseLost means another worker took the track: it writes nothing more.
func (a *Agent) synthesizeTrack(ctx context.Context, userID uuid.UUID, l speechLease, v voice, spoken string) (models.SpeechTrackStatus, string, error) {
	track, err := a.ds.GetSpeechTrack(ctx, userID, l.trackID)
	if errors.Is(err, datastore.ErrSpeechTrackNotFound) {
		return "", "", datastore.ErrSpeechLeaseLost
	}
	if err != nil {
		return models.SpeechTrackFailed, "internal_error", nil
	}
	said := make([]string, len(track.Segments))
	for i, seg := range track.Segments {
		said[i] = seg.Text
	}
	rest, ok := speechtext.Remaining(spoken, said)
	if !ok {
		keys, err := a.ds.WithdrawSpeechSegments(ctx, l.trackID, l.token, 0)
		if err != nil {
			return models.SpeechTrackFailed, "internal_error", err
		}
		a.deleteSpeechFiles(ctx, keys)
		said, rest = nil, spoken
	}
	client, err := provider.NewSpeechClient(v.target.spec.Adapter, v.target.endpoint)
	if err != nil {
		return models.SpeechTrackFailed, "internal_error", nil
	}
	format := preferredSpeechFormat(v.target.spec)
	// The characters already synthesised for this track (a worker taking over one whose
	// worker went away), so billing rounds the track's total once.
	trackChars := 0
	for _, t := range said {
		trackChars += utf8.RuneCountInString(t)
	}
	for i, text := range speechtext.Segment(rest, v.maxSegmentChars()) {
		seq := len(said) + i
		if err := ctx.Err(); err != nil {
			return models.SpeechTrackFailed, speechInterrupted, a.leaseErr(ctx, l)
		}
		decision, allowed, err := a.checkSpeechQuota(ctx, userID)
		if err != nil {
			return models.SpeechTrackFailed, "internal_error", nil
		}
		if !allowed {
			return models.SpeechTrackStopped, models.ErrCodeQuotaExceeded, nil
		}
		audio, usage, err := a.synthesize(ctx, client, v, text, format)
		if err != nil {
			if ctx.Err() != nil {
				return models.SpeechTrackFailed, speechInterrupted, a.leaseErr(ctx, l)
			}
			a.logger.Warn("speech synthesis failed", zap.String("track_id", l.trackID.String()), zap.Int("seq", seq), zap.Error(err))
			return models.SpeechTrackFailed, speechFailureCode(err), nil
		}
		key := speechSegmentKey(userID, l.trackID, seq, audio.Format, l.token)
		if err := a.fileStore.UploadFile(ctx, key, audio.Data, audio.Format.MIMEType()); err != nil {
			a.logger.Warn("couldn't store speech", zap.String("track_id", l.trackID.String()), zap.Error(err))
			return models.SpeechTrackFailed, "storage_error", nil
		}
		seg := models.SpeechSegment{Seq: seq, Text: text, FileKey: key, Format: audio.Format, DurationMS: audioDurationMS(audio), SizeBytes: len(audio.Data)}
		switch err := a.ds.AddSpeechSegment(ctx, l.trackID, l.token, seg); {
		case errors.Is(err, datastore.ErrSpeechLeaseLost), errors.Is(err, datastore.ErrSpeechSegmentExists):
			a.deleteSpeechFiles(context.WithoutCancel(ctx), []string{key})
			return "", "", datastore.ErrSpeechLeaseLost
		case err != nil:
			a.deleteSpeechFiles(context.WithoutCancel(ctx), []string{key})
			return models.SpeechTrackFailed, "internal_error", nil
		}
		trackChars += usage.Characters
		a.recordSpeech(ctx, decision, userID, track.ChatID, v, usage, trackChars)
	}
	return models.SpeechTrackComplete, "", nil
}

// leaseErr is ErrSpeechLeaseLost when ctx ended because the lease went to another
// worker, else nil: the worker was stopped (shutdown) and fails the track.
func (a *Agent) leaseErr(ctx context.Context, l speechLease) error {
	if err := a.ds.RenewSpeechWorker(context.WithoutCancel(ctx), l.trackID, l.token, time.Now().Add(speechWorkerLease)); errors.Is(err, datastore.ErrSpeechLeaseLost) {
		return err
	}
	return nil
}

// speechSegmentKey is where a segment's audio is stored. The worker's token is in it, so
// a worker that lost its lease never writes over its successor's file.
func speechSegmentKey(userID, trackID uuid.UUID, seq int, format models.AudioFormat, token uuid.UUID) string {
	return path.Join("users", userID.String(), "speech", trackID.String(), fmt.Sprintf("%04d-%s.%s", seq, token.String()[:8], format))
}

// deleteSpeechFiles deletes audio files nothing points at any more.
func (a *Agent) deleteSpeechFiles(ctx context.Context, keys []string) {
	for _, key := range keys {
		if err := a.fileStore.DeleteFile(ctx, key); err != nil {
			a.logger.Warn("couldn't delete speech audio", zap.String("key", key), zap.Error(err))
		}
	}
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

// recordSpeech records one synthesis. trackChars is the track's characters so far, this
// synthesis included (metering.Usage.SpeechTrackCharacters).
func (a *Agent) recordSpeech(ctx context.Context, d metering.Decision, userID, chatID uuid.UUID, v voice, usage provider.SpeechUsage, trackChars int) {
	if a.meter == nil {
		return
	}
	u := metering.Usage{
		UserID: userID, ActionType: models.ActionTypeSpeechSynthesis, Model: v.target.model,
		SpeechProvider: v.target.provider, SpeechCharacters: usage.Characters, SpeechTrackCharacters: trackChars,
		Source: metering.TurnSourceApp,
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

// SegmentAudio is one segment's audio as the client asked for it.
type SegmentAudio struct {
	// ETag names this version of the segment. A segment can be withdrawn and made again at
	// the same seq (a resumed track, a withdrawn round), so its URL alone doesn't name its audio.
	ETag   string
	Format models.AudioFormat
	// Data is nil when NotModified: the client already has this version.
	Data        []byte
	NotModified bool
}

// segmentETag names a segment's version by its file key, which a worker's lease token makes
// different every time the segment is made, without giving the key away.
func segmentETag(fileKey string) string {
	sum := sha256.Sum256([]byte(fileKey))
	return `"` + hex.EncodeToString(sum[:12]) + `"`
}

// SpeechSegmentAudio is one segment's stored audio, for the account's own track. When
// ifNoneMatch names the version stored, the audio isn't read at all.
func (a *Agent) SpeechSegmentAudio(ctx context.Context, userID, trackID uuid.UUID, seq int, ifNoneMatch string) (SegmentAudio, error) {
	track, err := a.ds.GetSpeechTrack(ctx, userID, trackID)
	if err != nil {
		return SegmentAudio{}, err
	}
	for _, s := range track.Segments {
		if s.Seq != seq {
			continue
		}
		out := SegmentAudio{ETag: segmentETag(s.FileKey), Format: s.Format}
		if ifNoneMatch == out.ETag {
			out.NotModified = true
			return out, nil
		}
		data, err := a.fileStore.DownloadFile(ctx, s.FileKey)
		if err != nil {
			return SegmentAudio{}, err
		}
		if data == nil {
			return SegmentAudio{}, datastore.ErrSpeechTrackNotFound
		}
		out.Data = data
		return out, nil
	}
	return SegmentAudio{}, datastore.ErrSpeechTrackNotFound
}

// SpeechTrack is the account's track, for the client polling it. A saved reply's track
// still open with no worker (its instance went away mid-track) gets one, so a client
// polling it doesn't wait forever.
func (a *Agent) SpeechTrack(ctx context.Context, userID, trackID uuid.UUID) (*models.SpeechTrack, error) {
	track, err := a.ds.GetSpeechTrack(ctx, userID, trackID)
	if err != nil {
		return nil, err
	}
	if track.Status == models.SpeechTrackOpen && track.MessageID != nil && !track.HasWorker(time.Now()) {
		a.resumeSpeechTrack(ctx, userID, track)
	}
	return track, nil
}

// resumeSpeechTrack starts a worker on an open track that has none, in the track's own
// voice; a track that can't go on (its reply or voice gone) is failed instead.
func (a *Agent) resumeSpeechTrack(ctx context.Context, userID uuid.UUID, track *models.SpeechTrack) {
	fail := func(code string) {
		if err := a.ds.FinishSpeechTrack(ctx, track.ID, uuid.Nil, models.SpeechTrackFailed, code); err == nil {
			track.Status, track.ErrorCode = models.SpeechTrackFailed, code
		}
	}
	msg, err := a.ds.GetChatMessage(ctx, userID, *track.MessageID)
	if err != nil {
		fail("message_gone")
		return
	}
	v, err := a.resolveVoice(ctx, userID, models.PersonalityVoice{ModelID: track.ModelID, Voice: track.Voice, Style: track.Style})
	if err != nil {
		fail(models.ErrCodeVoiceUnavailable)
		return
	}
	a.startSpeechWorker(userID, track.ID, v, speechtext.Spoken(msg.Message, msg.FinalRoundOffset))
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
	a.recordSpeech(ctx, decision, userID, uuid.Nil, v, usage, usage.Characters)
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
	cutoff := time.Now().Add(-speechRetention)
	tracks, err := a.ds.SweepableSpeechTracks(ctx, cutoff, 200)
	if err != nil {
		a.logger.Warn("couldn't list speech to sweep", zap.Error(err))
		return
	}
	for _, t := range tracks {
		// The row goes first, and only if it is still sweepable, so a track played since it
		// was listed keeps its audio; its files go after, from what was deleted.
		keys, deleted, err := a.ds.SweepSpeechTrack(ctx, t.ID, cutoff)
		if err != nil {
			a.logger.Warn("couldn't delete a speech track", zap.String("track_id", t.ID.String()), zap.Error(err))
			continue
		}
		if deleted {
			a.deleteSpeechFiles(ctx, keys)
		}
	}
}
