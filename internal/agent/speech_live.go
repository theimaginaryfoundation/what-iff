package agent

// Autoplay, and speaking while the reply streams (ADR 0x052 §8–§9).
//
// When a chat reads replies aloud as they arrive, the turn that writes a reply makes its
// speech track at the start, pending, and puts it on the turn's job. Nothing is synthesised
// until a client claims the track, which means someone is there to hear it. Claimed while
// the turn runs, the track follows the turn's streaming text: a round is held until it
// commits (200 characters, or it ends without a tool call), then its complete sentences are
// read as they arrive; a committed round that ends in a tool call after all is withdrawn.
// Claimed after the turn, the track is made from the saved reply, as Listen does.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/speechtext"
)

// commitChars is how much of a round is held before it is read aloud: narration before a
// tool call is almost always one short sentence (ADR 0x052 §8).
const commitChars = 200

// livePoll is how often a turn's track looks for a claim and for new text.
var livePoll = 250 * time.Millisecond

// liveSpeech is one turn's reply read aloud as it streams.
type liveSpeech struct {
	a       *Agent
	userID  uuid.UUID
	chatID  uuid.UUID
	trackID uuid.UUID
	v       voice

	mu    sync.Mutex
	buf   *jobDraftDeltaBuffer
	ended bool
	msg   *models.ChatMessage
	done  chan struct{}
}

type liveSpeechKey struct{}

func withLiveSpeech(ctx context.Context, l *liveSpeech) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, liveSpeechKey{}, l)
}

func liveSpeechFrom(ctx context.Context) *liveSpeech {
	l, _ := ctx.Value(liveSpeechKey{}).(*liveSpeech)
	return l
}

// chatAutoplay is whether a chat reads replies aloud as they arrive: its override, else
// its personality's default.
func chatAutoplay(chat *models.Chat, p *models.Personality) bool {
	if p == nil || p.Voice == nil {
		return false
	}
	if chat != nil && chat.VoiceAutoplay != nil {
		return *chat.VoiceAutoplay
	}
	return p.Voice.Autoplay
}

// beginTurnSpeech makes the turn's reply's track when its chat reads replies aloud, and
// starts following the turn; nil when it doesn't, or the voice can't be used.
func (a *Agent) beginTurnSpeech(ctx context.Context, userID uuid.UUID, chatJob *models.Job, chatCtx *chatContext) *liveSpeech {
	if chatJob == nil || chatCtx == nil || chatCtx.chat == nil || a.ds == nil || a.fileStore == nil {
		return nil
	}
	chat := chatCtx.chat
	if chat.PersonalityID == uuid.Nil || (chat.VoiceAutoplay != nil && !*chat.VoiceAutoplay) {
		return nil
	}
	p, err := a.ds.GetPersonality(ctx, userID, chat.PersonalityID)
	if err != nil || !chatAutoplay(chat, p) {
		return nil
	}
	v, err := a.resolveVoice(ctx, userID, *p.Voice)
	if err != nil {
		a.logger.Info("not reading this reply aloud: its voice can't be used", zap.String("chat_id", chat.ID.String()), zap.Error(err))
		return nil
	}
	track, err := a.ds.CreateTurnSpeechTrack(ctx, datastore.NewTurnSpeechTrack{
		UserID: userID, ChatID: chat.ID, JobID: chatJob.ID, ModelID: v.model.ID, Voice: v.voice, Style: v.style, VoiceKey: v.key(),
	})
	if err != nil {
		a.logger.Warn("couldn't make a turn's speech track", zap.Error(err))
		return nil
	}
	chatJob.SpeechTrackID = &track.ID
	l := &liveSpeech{a: a, userID: userID, chatID: chat.ID, trackID: track.ID, v: v, done: make(chan struct{})}
	base := a.lifecycleCtx
	if base == nil {
		base = context.Background()
	}
	go l.run(apicontext.WithUserID(base, userID))
	return l
}

// attach gives the speaker the turn's streaming text.
func (l *liveSpeech) attach(buf *jobDraftDeltaBuffer) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.buf = buf
	l.mu.Unlock()
}

// end tells the speaker the turn is over, with the reply it saved, or nil when it failed.
func (l *liveSpeech) end(ctx context.Context, msg *models.ChatMessage) {
	if l == nil {
		return
	}
	if msg != nil {
		if err := l.a.ds.LinkSpeechTrack(ctx, l.trackID, msg.ID); err != nil {
			l.a.logger.Warn("couldn't tie a speech track to its reply", zap.Error(err))
		}
	}
	l.mu.Lock()
	l.ended, l.msg = true, msg
	l.mu.Unlock()
}

func (l *liveSpeech) state() (buf *jobDraftDeltaBuffer, ended bool, msg *models.ChatMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf, l.ended, l.msg
}

func (l *liveSpeech) sleep(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(livePoll):
		return true
	}
}

// run waits for a claim while the turn runs, then reads the reply as it streams.
func (l *liveSpeech) run(ctx context.Context) {
	defer close(l.done)
	defer func() {
		if r := recover(); r != nil {
			l.a.logger.Error("live speech panicked", zap.Any("panic", r))
		}
	}()
	for {
		autoplay, _, err := l.a.ds.SpeechTrackState(ctx, l.trackID)
		if err != nil {
			return
		}
		_, ended, msg := l.state()
		switch {
		case autoplay == models.SpeechAutoplayPlayed:
			l.speak(ctx)
			return
		case autoplay == models.SpeechAutoplaySkipped || ended:
			if ended && msg == nil {
				// The turn failed: there is no reply to read.
				_ = l.a.ds.DeleteSpeechTrack(context.WithoutCancel(ctx), l.trackID)
			}
			// Unclaimed with a reply: a later claim makes it from the saved text.
			return
		}
		if !l.sleep(ctx) {
			return
		}
	}
}

// speak reads the reply as it streams, and the rest once it is saved.
func (l *liveSpeech) speak(ctx context.Context) {
	a := l.a
	ok, err := a.ds.AcquireSpeechWorker(ctx, l.trackID, time.Now().Add(speechWorkerLease))
	if err != nil || !ok {
		return
	}
	client, err := provider.NewSpeechClient(l.v.target.spec.Adapter, l.v.target.endpoint)
	if err != nil {
		_ = a.ds.FinishSpeechTrack(ctx, l.trackID, models.SpeechTrackFailed, "internal_error")
		return
	}
	s := &liveSegmenter{l: l, client: client, rounds: -1, format: preferredSpeechFormat(l.v.target.spec)}
	for {
		buf, ended, msg := l.state()
		if ended {
			status, code := s.finish(ctx, msg)
			if err := a.ds.FinishSpeechTrack(context.WithoutCancel(ctx), l.trackID, status, code); err != nil {
				a.logger.Warn("couldn't finish a speech track", zap.Error(err))
			}
			return
		}
		if buf != nil {
			if status, code, stop := s.step(ctx, buf); stop {
				_ = a.ds.FinishSpeechTrack(context.WithoutCancel(ctx), l.trackID, status, code)
				return
			}
		}
		if err := a.ds.RenewSpeechWorker(ctx, l.trackID, time.Now().Add(speechWorkerLease)); err != nil || !l.sleep(ctx) {
			return
		}
	}
}

// liveSegmenter turns streaming text into a track's segments.
type liveSegmenter struct {
	l      *liveSpeech
	client provider.SpeechClient
	format models.AudioFormat

	seq int
	// rounds is the round being followed; roundSeq the first segment of it; committed
	// whether it is being read; spoken how much of its prepared text has been read.
	rounds    int
	roundSeq  int
	committed bool
	spoken    string
}

// step reads what the streaming text allows now. stop is set when the track has to end
// early (out of credits, or a provider failure).
func (s *liveSegmenter) step(ctx context.Context, buf *jobDraftDeltaBuffer) (models.SpeechTrackStatus, string, bool) {
	text, start, rounds := buf.liveText()
	if rounds != s.rounds {
		// A new round: the last one ended in a tool call, so it was narration.
		if s.committed && s.seq > s.roundSeq {
			s.withdraw(ctx)
		}
		s.rounds, s.roundSeq, s.committed, s.spoken = rounds, s.seq, false, ""
	}
	if start > len(text) {
		start = len(text)
	}
	raw := text[start:]
	if !s.committed && len(strings.TrimSpace(raw)) < commitChars {
		return "", "", false
	}
	s.committed = true
	for {
		prepared := speechtext.Prepare(raw)
		if !strings.HasPrefix(prepared, s.spoken) {
			// The markdown read differently once more of it arrived; wait for the end,
			// which reconciles from the saved reply.
			return "", "", false
		}
		piece, n := speechtext.NextSpeakable(prepared[len(s.spoken):], s.seq == s.roundSeq, s.l.v.maxSegmentChars())
		if piece == "" {
			return "", "", false
		}
		if status, code, stop := s.say(ctx, piece); stop {
			return status, code, true
		}
		s.spoken = prepared[:len(s.spoken)+n]
	}
}

// finish reads the rest of the saved reply's final round.
func (s *liveSegmenter) finish(ctx context.Context, msg *models.ChatMessage) (models.SpeechTrackStatus, string) {
	if msg == nil {
		s.withdrawAll(ctx)
		return models.SpeechTrackFailed, "turn_failed"
	}
	final := speechtext.Spoken(msg.Message, msg.FinalRoundOffset)
	rest := final
	if s.committed && strings.HasPrefix(final, s.spoken) {
		rest = final[len(s.spoken):]
	} else if s.seq > s.roundSeq || !s.committed {
		// What was read doesn't match the saved reply (or nothing was): start its final
		// round again.
		s.withdraw(ctx)
	}
	for _, piece := range speechtext.Segment(rest, s.l.v.maxSegmentChars()) {
		if status, code, stop := s.say(ctx, piece); stop {
			return status, code
		}
	}
	return models.SpeechTrackComplete, ""
}

// say synthesises one piece as the next segment.
func (s *liveSegmenter) say(ctx context.Context, piece string) (models.SpeechTrackStatus, string, bool) {
	a, l := s.l.a, s.l
	decision, allowed, err := a.checkSpeechQuota(ctx, l.userID)
	if err != nil {
		return models.SpeechTrackFailed, "internal_error", true
	}
	if !allowed {
		return models.SpeechTrackStopped, models.ErrCodeQuotaExceeded, true
	}
	audio, usage, err := a.synthesize(ctx, s.client, l.v, piece, s.format)
	if err != nil {
		a.logger.Warn("live speech synthesis failed", zap.String("track_id", l.trackID.String()), zap.Error(err))
		return models.SpeechTrackFailed, speechFailureCode(err), true
	}
	key := fmt.Sprintf("users/%s/speech/%s/%04d-%s.%s", l.userID, l.trackID, s.seq, uuid.NewString()[:8], audio.Format)
	if err := a.fileStore.UploadFile(ctx, key, audio.Data, audio.Format.MIMEType()); err != nil {
		return models.SpeechTrackFailed, "storage_error", true
	}
	seg := models.SpeechSegment{Seq: s.seq, Text: piece, FileKey: key, Format: audio.Format, DurationMS: audioDurationMS(audio), SizeBytes: len(audio.Data)}
	if err := a.ds.AddSpeechSegment(ctx, l.trackID, seg); err != nil {
		return models.SpeechTrackFailed, "internal_error", true
	}
	a.recordSpeech(ctx, decision, l.userID, l.chatID, l.v, usage)
	s.seq++
	return "", "", false
}

// withdraw takes back the current round's segments: it wasn't the last round.
func (s *liveSegmenter) withdraw(ctx context.Context) {
	s.withdrawFrom(ctx, s.roundSeq)
	s.spoken = ""
}

func (s *liveSegmenter) withdrawAll(ctx context.Context) { s.withdrawFrom(ctx, 0) }

func (s *liveSegmenter) withdrawFrom(ctx context.Context, seq int) {
	keys, err := s.l.a.ds.WithdrawSpeechSegments(ctx, s.l.trackID, seq)
	if err != nil {
		s.l.a.logger.Warn("couldn't withdraw speech", zap.Error(err))
		return
	}
	for _, k := range keys {
		_ = s.l.a.fileStore.DeleteFile(ctx, k)
	}
	s.seq = seq
}

// ClaimSpeech claims a pending track for this client to play (ADR 0x052 §9), and answers it.
// A track whose turn is over is made from the saved reply; one whose turn still runs is
// read by the turn as it streams. claimed is false when another client got it first, or the
// track is more than a day old.
func (a *Agent) ClaimSpeech(ctx context.Context, userID, trackID uuid.UUID) (*models.SpeechTrack, bool, error) {
	claimed, err := a.ds.ClaimSpeechTrack(ctx, userID, trackID)
	if err != nil {
		return nil, false, err
	}
	track, err := a.ds.GetSpeechTrack(ctx, userID, trackID)
	if err != nil {
		return nil, false, err
	}
	if !claimed || track.MessageID == nil || track.Status != models.SpeechTrackOpen || len(track.Segments) > 0 {
		return track, claimed, nil
	}
	msg, err := a.ds.GetChatMessage(ctx, userID, *track.MessageID)
	if err != nil {
		return nil, false, err
	}
	v, err := a.resolveVoice(ctx, userID, models.PersonalityVoice{ModelID: track.ModelID, Voice: track.Voice, Style: track.Style})
	if err != nil {
		_ = a.ds.FinishSpeechTrack(ctx, track.ID, models.SpeechTrackFailed, models.ErrCodeVoiceUnavailable)
		track.Status, track.ErrorCode = models.SpeechTrackFailed, models.ErrCodeVoiceUnavailable
		return track, true, nil
	}
	a.startSpeechWorker(userID, track, v, speechtext.Segment(speechtext.Spoken(msg.Message, msg.FinalRoundOffset), v.maxSegmentChars()))
	return track, true, nil
}

// SkipSpeech marks a pending track skipped: it won't be read out on arrival.
func (a *Agent) SkipSpeech(ctx context.Context, userID, trackID uuid.UUID) error {
	return a.ds.SkipSpeechTrack(ctx, userID, trackID)
}

// PendingSpeech lists the chat's tracks waiting to be read out on arrival, newest first.
func (a *Agent) PendingSpeech(ctx context.Context, userID, chatID uuid.UUID) ([]*models.SpeechTrack, error) {
	return a.ds.PendingSpeechTracks(ctx, userID, chatID)
}

// SetChatAutoplay sets the chat's autoplay override (nil follows the personality). When the
// chat no longer reads replies aloud, its pending tracks are skipped.
func (a *Agent) SetChatAutoplay(ctx context.Context, userID, chatID uuid.UUID, autoplay *bool) (*models.Chat, error) {
	if err := a.ds.SetChatVoiceAutoplay(ctx, userID, chatID, autoplay); err != nil {
		return nil, err
	}
	chat, err := a.ds.GetChat(ctx, userID, chatID)
	if err != nil {
		return nil, err
	}
	var p *models.Personality
	if chat.PersonalityID != uuid.Nil {
		if p, err = a.ds.GetPersonality(ctx, userID, chat.PersonalityID); err != nil {
			p = nil
		}
	}
	if !chatAutoplay(chat, p) {
		if err := a.ds.SkipPendingSpeechTracks(ctx, userID, chatID); err != nil {
			return nil, err
		}
	}
	return chat, nil
}
