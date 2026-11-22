package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func init() { livePoll = 5 * time.Millisecond }

// turn is a chat turn in the speech fixture's chat: its job, its streaming text, and
// the context it runs with.
type turn struct {
	f    *speechFixture
	ctx  context.Context
	job  *models.Job
	buf  *jobDraftDeltaBuffer
	live *liveSpeech
}

func (f *speechFixture) autoplayOn(t *testing.T) {
	t.Helper()
	_, err := f.a.ds.SetPersonalityVoice(context.Background(), f.alice, f.personality,
		&models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella", Autoplay: true})
	require.NoError(t, err)
}

func (f *speechFixture) startTurn(t *testing.T) *turn {
	t.Helper()
	ctx := apicontext.WithUserID(context.Background(), f.alice)
	j, err := f.ent.Job.Create().SetOwnerID(f.alice).SetJobType("chat_message").SetReference(f.chat.String()).SetStatus("processing").Save(ctx)
	require.NoError(t, err)
	job := &models.Job{ID: j.ID, UserID: f.alice}
	chat, err := f.a.ds.GetChat(ctx, f.alice, f.chat)
	require.NoError(t, err)
	tr := &turn{f: f, ctx: ctx, job: job}
	tr.live = f.a.beginTurnSpeech(ctx, f.alice, job, &chatContext{chat: chat})
	tr.buf = newJobDraftDeltaBuffer(ctx, f.a.ds, zap.NewNop(), job, 1, time.Millisecond)
	tr.live.attach(tr.buf)
	return tr
}

func (tr *turn) write(text string) { tr.buf.HandleDelta(text) }

func (tr *turn) toolRound() { tr.buf.Flush(); tr.buf.MarkRoundBoundary() }

// finish saves the reply as a turn does, and tells the speaker.
func (tr *turn) finish(t *testing.T) *models.ChatMessage {
	t.Helper()
	text, offset := tr.buf.streamed()
	m, err := tr.f.ent.ChatMessage.Create().SetChatID(tr.f.chat).SetMessage(text).SetOrigin("Assistant").
		SetSentAt(time.Now()).SetFinalRoundOffset(offset).Save(tr.ctx)
	require.NoError(t, err)
	msg := &models.ChatMessage{ID: m.ID, ChatID: tr.f.chat, Message: text, FinalRoundOffset: offset}
	tr.live.end(tr.ctx, msg)
	return msg
}

func (f *speechFixture) spokenSoFar() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.spoken...)
}

func longRound(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("Sentence number ")
		b.WriteString(string(rune('A' + i)))
		b.WriteString(" is here to be read. ")
	}
	return b.String()
}

// Claimed while the turn runs, the reply is read as it streams, after the narration of a
// tool round, which is never read.
func TestLiveSpeech_ReadsTheFinalRoundAsItStreams(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	require.NotNil(t, tr.live, "the chat reads replies aloud")
	require.NotNil(t, tr.job.SpeechTrackID, "the job carries the track for the client watching it")

	track, claimed, err := f.a.ClaimSpeech(tr.ctx, f.alice, *tr.job.SpeechTrackID)
	require.NoError(t, err)
	require.True(t, claimed)
	_, again, err := f.a.ClaimSpeech(tr.ctx, f.alice, track.ID)
	require.NoError(t, err)
	require.False(t, again, "only one client gets it")

	tr.write("Let me check the forecast.")
	tr.toolRound()
	final := longRound(12)
	tr.write(final[:len(final)/2])
	require.Eventually(t, func() bool { return len(f.spokenSoFar()) >= 1 }, 3*time.Second, 5*time.Millisecond,
		"speech starts before the reply is done")
	tr.write(final[len(final)/2:])
	tr.finish(t)

	done := f.waitTrack(t, track.ID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	spoken := f.spokenSoFar()
	require.Equal(t, "Sentence number A is here to be read.", spoken[0], "the first sentence alone")
	joined := strings.Join(spoken, " ")
	require.NotContains(t, joined, "forecast", "narration isn't read")
	require.Equal(t, strings.Join(strings.Fields(final), " "), strings.Join(strings.Fields(joined), " "), "the whole final round, once")
}

// A round long enough to start reading that then ends in a tool call was narration after
// all: its audio is withdrawn.
func TestLiveSpeech_WithdrawsARoundThatWasNarration(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	_, _, err := f.a.ClaimSpeech(tr.ctx, f.alice, *tr.job.SpeechTrackID)
	require.NoError(t, err)

	tr.write(longRound(10))
	require.Eventually(t, func() bool { return len(f.spokenSoFar()) >= 1 }, 3*time.Second, 5*time.Millisecond)
	tr.toolRound()
	tr.write("It will rain on Tuesday. Take a coat. ")
	tr.finish(t)

	done := f.waitTrack(t, *tr.job.SpeechTrackID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	var texts []string
	for _, s := range done.Segments {
		texts = append(texts, s.Text)
	}
	require.Equal(t, []string{"It will rain on Tuesday.", "Take a coat."}, texts, "only the final round's segments remain")
	require.Equal(t, len(done.Segments), f.files.count(), "the withdrawn audio is deleted")
}

// Nobody watching: nothing is synthesised while the turn runs, and a claim after it reads
// the saved reply.
func TestLiveSpeech_UnclaimedTurnIsReadOnALaterClaim(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	tr.write("Good morning. Here is your summary. ")
	msg := tr.finish(t)
	<-tr.live.done
	require.Empty(t, f.spokenSoFar(), "no audio nobody hears")

	pending, err := f.a.PendingSpeech(tr.ctx, f.alice, f.chat)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, msg.ID, *pending[0].MessageID)

	_, claimed, err := f.a.ClaimSpeech(tr.ctx, f.alice, pending[0].ID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, models.SpeechTrackComplete, f.waitTrack(t, pending[0].ID).Status)
	require.Equal(t, []string{"Good morning.", "Here is your summary."}, f.spokenSoFar())
}

func TestLiveSpeech_AFailedTurnLeavesNoTrack(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	tr.live.end(tr.ctx, nil)
	<-tr.live.done
	_, err := f.a.SpeechTrack(tr.ctx, f.alice, *tr.job.SpeechTrackID)
	require.Error(t, err)
}

func TestAutoplay_SettingsAndClaims(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.giveVoice(t, "")
	tr := f.startTurn(t)
	require.Nil(t, tr.live, "the personality doesn't read aloud by default")

	on := true
	_, err := f.a.SetChatAutoplay(tr.ctx, f.alice, f.chat, &on)
	require.NoError(t, err)
	tr = f.startTurn(t)
	require.NotNil(t, tr.live, "the chat's override turns it on")
	tr.write("Hello. ")
	tr.finish(t)
	<-tr.live.done

	off := false
	chat, err := f.a.SetChatAutoplay(tr.ctx, f.alice, f.chat, &off)
	require.NoError(t, err)
	require.False(t, *chat.VoiceAutoplay)
	pending, err := f.a.PendingSpeech(tr.ctx, f.alice, f.chat)
	require.NoError(t, err)
	require.Empty(t, pending, "turning autoplay off skips what was waiting")

	// A pending reply more than a day old is skipped, not played.
	_, err = f.a.SetChatAutoplay(tr.ctx, f.alice, f.chat, &on)
	require.NoError(t, err)
	tr = f.startTurn(t)
	tr.write("Old news. ")
	tr.finish(t)
	<-tr.live.done
	_, err = f.db.Exec(`UPDATE speech_tracks SET created_at = ? WHERE id = ?`, time.Now().Add(-25*time.Hour), *tr.job.SpeechTrackID)
	require.NoError(t, err)
	track, claimed, err := f.a.ClaimSpeech(tr.ctx, f.alice, *tr.job.SpeechTrackID)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, models.SpeechAutoplaySkipped, track.Autoplay)

	_, _, err = f.a.ClaimSpeech(apicontext.WithUserID(context.Background(), f.bob), f.bob, *tr.job.SpeechTrackID)
	require.Error(t, err, "not bob's track")
}

// Markup still open while it streams isn't read with its markers, doesn't stop the
// speaker, and nothing is read twice once it closes.
func TestLiveSpeech_OpenMarkupWaitsAndIsReadOnce(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	_, _, err := f.a.ClaimSpeech(tr.ctx, f.alice, *tr.job.SpeechTrackID)
	require.NoError(t, err)

	tr.write(longRound(6) + "**Note. Then more")
	require.Eventually(t, func() bool { return len(f.spokenSoFar()) >= 2 }, 3*time.Second, 5*time.Millisecond)
	tr.write(" words.** And that is the end of it. ")
	tr.finish(t)

	done := f.waitTrack(t, *tr.job.SpeechTrackID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	joined := strings.Join(f.spokenSoFar(), " ")
	require.NotContains(t, joined, "*", "no markers read aloud")
	require.Equal(t, 1, strings.Count(joined, "Sentence number A"), "the opening isn't read again")
	require.Contains(t, joined, "Note. Then more words.")
}

// A claim that lands just as the turn ends still gets the reply read.
func TestLiveSpeech_AClaimAtTheTurnsEndIsRead(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	tr.write("A short answer. ")
	// The claim is in, but the speaker hasn't seen it when the turn ends.
	claimed, err := f.a.ds.ClaimSpeechTrack(tr.ctx, f.alice, *tr.job.SpeechTrackID)
	require.NoError(t, err)
	require.True(t, claimed)
	tr.finish(t)

	done := f.waitTrack(t, *tr.job.SpeechTrackID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	require.Len(t, done.Segments, 1)
}

// The instance shutting down mid-turn leaves the claimed track open with what it read. The
// turn's reply, saved but never tied to the track, is tied to it on the next poll, which
// carries on from there (ADR 0x052 §8).
func TestLiveSpeech_ShutdownMidTurnIsCarriedOnFromTheSavedReply(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	life, shutDown := context.WithCancel(context.Background())
	f.a.lifecycleCtx = life
	tr := f.startTurn(t)
	trackID := *tr.job.SpeechTrackID
	_, claimed, err := f.a.ClaimSpeech(tr.ctx, f.alice, trackID)
	require.NoError(t, err)
	require.True(t, claimed)

	final := longRound(12)
	tr.write(final[:len(final)/2])
	require.Eventually(t, func() bool {
		stored, err := f.a.ds.GetSpeechTrack(tr.ctx, f.alice, trackID)
		return err == nil && len(stored.Segments) >= 1
	}, 3*time.Second, 5*time.Millisecond, "a segment is stored before the instance goes")
	shutDown()
	<-tr.live.done
	stored, err := f.a.ds.GetSpeechTrack(tr.ctx, f.alice, trackID)
	require.NoError(t, err)
	require.Equal(t, models.SpeechTrackOpen, stored.Status, "shutting down is not the track's end")
	require.False(t, stored.HasWorker(time.Now()))
	require.Nil(t, stored.MessageID)
	read := len(stored.Segments)
	require.Positive(t, read)

	// The turn saved its reply before the instance went, but never tied the track to it.
	m, err := f.ent.ChatMessage.Create().SetChatID(f.chat).SetMessage(final).SetOrigin("Assistant").SetSentAt(time.Now()).Save(tr.ctx)
	require.NoError(t, err)
	require.NoError(t, f.ent.Job.UpdateOneID(tr.job.ID).SetStatus("complete").SetResultID(m.ID).Exec(tr.ctx))

	f.a.lifecycleCtx = context.Background()
	done := f.waitTrack(t, trackID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	require.Equal(t, m.ID, *done.MessageID)
	var texts []string
	for _, s := range done.Segments {
		texts = append(texts, s.Text)
	}
	require.Equal(t, strings.Join(strings.Fields(final), " "), strings.Join(texts, " "), "the whole reply, once")
}

// A turn that died without a reply leaves its track tied to nothing: a poll fails it and
// takes it off the pending list, rather than leaving the client to give up on it.
func TestLiveSpeech_ATrackWhoseTurnDiedWithoutAReplyFails(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	trackID := *tr.job.SpeechTrackID

	track, err := f.a.SpeechTrack(tr.ctx, f.alice, trackID)
	require.NoError(t, err)
	require.Equal(t, models.SpeechTrackOpen, track.Status, "a turn still at work is left to its speaker")
	require.Equal(t, models.SpeechAutoplayPending, track.Autoplay)

	// The instance running the turn goes; its job is left processing, and goes stale.
	require.NoError(t, f.ent.Job.UpdateOneID(tr.job.ID).SetUpdatedAt(time.Now().Add(-time.Hour)).Exec(tr.ctx))
	track, err = f.a.SpeechTrack(tr.ctx, f.alice, trackID)
	require.NoError(t, err)
	require.Equal(t, models.SpeechTrackFailed, track.Status)
	require.Equal(t, "turn_failed", track.ErrorCode)
	pending, err := f.a.PendingSpeech(tr.ctx, f.alice, f.chat)
	require.NoError(t, err)
	require.Empty(t, pending, "not read aloud on arrival")
}

// A turn's track no reply was ever tied to is swept after a day.
func TestSweepSpeech_DeletesATurnTrackNeverTiedToAReply(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "unused", 0)
	f.autoplayOn(t)
	tr := f.startTurn(t)
	trackID := *tr.job.SpeechTrackID
	tr.live.end(tr.ctx, &models.ChatMessage{ID: f.reply}) // tied, so the speaker leaves it be
	<-tr.live.done
	require.NoError(t, f.ent.SpeechTrack.UpdateOneID(trackID).ClearMessageID().Exec(tr.ctx))

	f.a.sweepSpeechOnce(tr.ctx)
	_, err := f.a.ds.GetSpeechTrack(tr.ctx, f.alice, trackID)
	require.NoError(t, err, "a day hasn't passed")

	_, err = f.db.ExecContext(tr.ctx, "UPDATE speech_tracks SET created_at = ? WHERE id = ?", time.Now().Add(-25*time.Hour), trackID)
	require.NoError(t, err)
	f.a.sweepSpeechOnce(tr.ctx)
	_, err = f.a.ds.GetSpeechTrack(tr.ctx, f.alice, trackID)
	require.Error(t, err)
}
