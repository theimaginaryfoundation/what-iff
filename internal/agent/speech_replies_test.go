package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/migrate"
	"github.com/theimaginaryfoundation/what-iff/internal/apicontext"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// memFiles is an in-memory file store.
type memFiles struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (m *memFiles) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = append([]byte(nil), content...)
	return nil
}

func (m *memFiles) DownloadFile(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files[key], nil
}

func (m *memFiles) DeleteFile(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	return nil
}

func (m *memFiles) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.files)
}

// speechFixture is alice with a personality whose voice is a speech model on her
// Custom connection, a chat with it, and a reply in that chat.
type speechFixture struct {
	*connectionFixture
	files       *memFiles
	voiceModel  *ent.Model
	personality uuid.UUID
	chat        uuid.UUID
	reply       uuid.UUID
	userMessage uuid.UUID
	spoken      []string // the inputs the speech server was asked to read
	calls       atomic.Int32
	mu          sync.Mutex
	// hold, when set, runs before the speech server answers its nth call (from 1), with the
	// request's context.
	hold func(ctx context.Context, n int32)
}

func newSpeechFixture(t *testing.T, reply string, finalRoundOffset int) *speechFixture {
	t.Helper()
	f := &speechFixture{connectionFixture: newConnectionFixture(t), files: &memFiles{files: map[string][]byte{}}}
	// The database is in memory with no shared cache: every connection would be a new, empty
	// one, and the speech worker runs beside the test.
	f.db.SetMaxOpenConns(1)
	ctx := context.Background()
	migrator, err := schema.NewMigrate(entsql.OpenDB(dialect.SQLite, f.db))
	require.NoError(t, err)
	schemaMu.Lock()
	err = migrator.Create(ctx, schemaTables(migrate.UsersTable, migrate.ChatsTable, migrate.ModelsTable,
		migrate.PersonalitiesTable, migrate.MoodsTable, migrate.PersonalityMoodsTable, migrate.FileAttachmentsTable,
		migrate.PersonalityExpressionsTable, migrate.ChatMessagesTable, migrate.ToolCallsTable,
		migrate.SpeechTracksTable, migrate.SpeechSegmentsTable, migrate.RitualsTable, migrate.ChatMessageRitualsTable,
		migrate.ChatMessageContextItemsTable, migrate.JobsTable)...)
	schemaMu.Unlock()
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input        string `json:"input"`
			Voice        string `json:"voice"`
			Instructions string `json:"instructions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.spoken = append(f.spoken, body.Input)
		hold := f.hold
		f.mu.Unlock()
		n := f.calls.Add(1)
		if hold != nil {
			hold(r.Context(), n)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "ID3"+body.Input)
	}))
	t.Cleanup(srv.Close)
	conn, err := f.ent.ProviderConnection.Create().SetUserID(f.alice).SetPreset("custom").SetName("Kokoro").Save(ctx)
	require.NoError(t, err)
	f.connectionFixture.mu.Lock()
	f.owners[conn.ID] = f.alice
	f.connectionFixture.mu.Unlock()
	f.voiceModel, err = f.ent.Model.Create().SetName("kokoro").SetDisplayName("Kokoro").SetDescription("").
		SetProvider("custom").SetConnectionID(conn.ID).SetKind("speech").
		SetSpeechVoices([]models.SpeechVoice{{ID: "af_bella", Name: "Bella"}}).Save(ctx)
	require.NoError(t, err)
	f.serve(f.voiceModel, ConnectionEndpoint{Preset: "custom", Flavor: ConnectionFlavorOpenAI, BaseURL: srv.URL + "/v1"})
	f.a.connectionClient = srv.Client()
	f.a.fileStore = f.files
	f.a.lifecycleCtx = context.Background()

	p, err := f.ent.Personality.Create().SetName("Ada").SetSystemPrompt("You are Ada.").SetUserID(f.alice).Save(ctx)
	require.NoError(t, err)
	f.personality = p.ID
	chat, err := f.ent.Chat.Create().SetName("Weather").SetOwnerID(f.alice).SetPersonalityID(p.ID).Save(ctx)
	require.NoError(t, err)
	f.chat = chat.ID
	um, err := f.ent.ChatMessage.Create().SetChatID(chat.ID).SetMessage("Will it rain?").SetOrigin("User").SetSentAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	f.userMessage = um.ID
	m, err := f.ent.ChatMessage.Create().SetChatID(chat.ID).SetMessage(reply).SetOrigin("Assistant").
		SetSentAt(time.Now()).SetFinalRoundOffset(finalRoundOffset).Save(ctx)
	require.NoError(t, err)
	f.reply = m.ID
	return f
}

func (f *speechFixture) giveVoice(t *testing.T, style string) {
	t.Helper()
	_, err := f.a.ds.SetPersonalityVoice(context.Background(), f.alice, f.personality, &models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella", Style: style})
	require.NoError(t, err)
}

// waitTrack polls the track until it leaves open.
func (f *speechFixture) waitTrack(t *testing.T, id uuid.UUID) *models.SpeechTrack {
	t.Helper()
	var track *models.SpeechTrack
	require.Eventually(t, func() bool {
		var err error
		track, err = f.a.SpeechTrack(context.Background(), f.alice, id)
		require.NoError(t, err)
		return track.Status != models.SpeechTrackOpen
	}, 5*time.Second, 10*time.Millisecond)
	return track
}

// A worker interrupted by its instance shutting down leaves the track open with no worker,
// keeping the segments it made, and the next poll carries on from there (ADR 0x052 §8).
func TestSpeechTrack_PollResumesATrackInterruptedByShutdown(t *testing.T) {
	t.Parallel()
	reply := "It will **rain** on Tuesday. Take a coat.\n\nThe rest of the week is dry."
	f := newSpeechFixture(t, reply, 0)
	f.giveVoice(t, "")
	ctx := context.Background()
	life, shutDown := context.WithCancel(context.Background())
	f.a.lifecycleCtx = life
	f.mu.Lock()
	f.hold = func(req context.Context, n int32) {
		if n == 2 {
			shutDown()
			<-req.Done()
		}
	}
	f.mu.Unlock()

	track, err := f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	var stored *models.SpeechTrack
	require.Eventually(t, func() bool {
		// The stored row, not SpeechTrack, which would resume it.
		stored, err = f.a.ds.GetSpeechTrack(ctx, f.alice, track.ID)
		require.NoError(t, err)
		return !stored.HasWorker(time.Now())
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, models.SpeechTrackOpen, stored.Status, "shutting down is not the track's end")
	require.Len(t, stored.Segments, 1, "it keeps the segment it made")

	f.mu.Lock()
	f.hold = nil
	f.mu.Unlock()
	f.a.lifecycleCtx = context.Background()
	done := f.waitTrack(t, track.ID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	require.Len(t, done.Segments, 2)
	require.EqualValues(t, 3, f.calls.Load(), "only the interrupted segment is asked for again")
}

func TestListen_ReadsTheFinalRoundInSegmentsAndStoresThem(t *testing.T) {
	t.Parallel()
	reply := "Let me check the forecast.\n\nIt will **rain** on Tuesday. Take a coat.\n\nThe rest of the week is dry."
	f := newSpeechFixture(t, reply, strings.Index(reply, "It will"))
	f.giveVoice(t, "warm")
	meter := &recordingMeter{allow: true}
	f.a.meter = meter
	ctx := apicontext.WithUserID(context.Background(), f.alice)

	track, err := f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	done := f.waitTrack(t, track.ID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	require.Equal(t, []string{"It will rain on Tuesday.", "Take a coat.\n\nThe rest of the week is dry."}, f.spoken,
		"the narration before the tool call is not read; the first sentence goes alone")
	require.Len(t, done.Segments, 2)
	require.Equal(t, 2, f.files.count())
	require.Equal(t, []string{models.ActionTypeSpeechSynthesis, models.ActionTypeSpeechSynthesis}, meter.checks)
	require.Equal(t, len([]rune("It will rain on Tuesday.")), meter.records[0].SpeechCharacters)
	require.Equal(t, "kokoro", meter.records[0].Model)
	first, second := len([]rune("It will rain on Tuesday.")), len([]rune("Take a coat.\n\nThe rest of the week is dry."))
	require.Equal(t, first, meter.records[0].SpeechTrackCharacters)
	require.Equal(t, first+second, meter.records[1].SpeechTrackCharacters, "the track's running total, for rounding once per track")

	seg, err := f.a.SpeechSegmentAudio(ctx, f.alice, track.ID, 0, "")
	require.NoError(t, err)
	require.Equal(t, models.AudioFormatMP3, seg.Format)
	require.Equal(t, "ID3It will rain on Tuesday.", string(seg.Data))
	require.NotEmpty(t, seg.ETag)
	same, err := f.a.SpeechSegmentAudio(ctx, f.alice, track.ID, 0, seg.ETag)
	require.NoError(t, err)
	require.True(t, same.NotModified, "the version the client has")
	require.Nil(t, same.Data, "not read again")

	// Playing it again replays the stored audio.
	again, err := f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	require.Equal(t, track.ID, again.ID)
	require.EqualValues(t, 2, f.calls.Load(), "no provider call for a replay")

	// Another voice is another track.
	f.giveVoice(t, "brisk")
	other, err := f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	require.NotEqual(t, track.ID, other.ID)
	f.waitTrack(t, other.ID)
}

func TestListen_OutOfCreditsStopsAndAskingAgainResumes(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "One. Two. Three.", 0)
	f.giveVoice(t, "")
	meter := &recordingMeter{allow: false}
	f.a.meter = meter
	ctx := apicontext.WithUserID(context.Background(), f.alice)

	track, err := f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	stopped := f.waitTrack(t, track.ID)
	require.Equal(t, models.SpeechTrackStopped, stopped.Status)
	require.Equal(t, models.ErrCodeQuotaExceeded, stopped.ErrorCode)
	require.Zero(t, f.calls.Load())

	meter.mu.Lock()
	meter.allow = true
	meter.mu.Unlock()
	_, err = f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	require.Equal(t, models.SpeechTrackComplete, f.waitTrack(t, track.ID).Status)
}

func TestListen_Refusals(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "Hello.", 0)
	ctx := apicontext.WithUserID(context.Background(), f.alice)

	_, err := f.a.Listen(ctx, f.alice, f.reply)
	require.ErrorIs(t, err, ErrNoVoice)
	f.giveVoice(t, "")
	_, err = f.a.Listen(ctx, f.alice, f.userMessage)
	require.ErrorIs(t, err, ErrNotAReply)
	_, err = f.a.Listen(apicontext.WithUserID(context.Background(), f.bob), f.bob, f.reply)
	require.Error(t, err, "not bob's message")

	// The voice is narrowed away from the model.
	_, err = f.ent.Model.UpdateOneID(f.voiceModel.ID).SetSpeechVoices([]models.SpeechVoice{{ID: "am_adam", Name: "Adam"}}).Save(ctx)
	require.NoError(t, err)
	_, err = f.a.Listen(ctx, f.alice, f.reply)
	require.ErrorIs(t, err, ErrVoiceUnavailable)
}

func TestSetPersonalityVoice(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "Hi.", 0)
	ctx := context.Background()
	set := func(v *models.PersonalityVoice) error {
		_, err := f.a.ds.SetPersonalityVoice(ctx, f.alice, f.personality, v)
		return err
	}
	require.ErrorIs(t, set(&models.PersonalityVoice{ModelID: f.aliceModel.ID, Voice: "af_bella"}), models.ErrInvalidPersonalityVoice, "a chat model isn't a voice")
	require.ErrorIs(t, set(&models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "nobody"}), models.ErrInvalidPersonalityVoice)
	require.ErrorIs(t, set(&models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella", Style: strings.Repeat("x", 501)}), models.ErrInvalidPersonalityVoice)
	require.NoError(t, set(&models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: " af_bella ", Style: " dry wit "}))
	p, err := f.a.ds.GetPersonality(ctx, f.alice, f.personality)
	require.NoError(t, err)
	require.Equal(t, &models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella", Style: "dry wit"}, p.Voice)

	_, err = f.a.ds.SetPersonalityVoice(ctx, f.bob, f.personality, nil)
	require.Error(t, err, "not bob's personality")
	require.NoError(t, set(nil))
	p, err = f.a.ds.GetPersonality(ctx, f.alice, f.personality)
	require.NoError(t, err)
	require.Nil(t, p.Voice)
}

func TestPreviewVoice(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "Hi.", 0)
	meter := &recordingMeter{allow: true}
	f.a.meter = meter
	ctx := apicontext.WithUserID(context.Background(), f.alice)
	pv := models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella", Style: "warm"}

	audio, err := f.a.PreviewVoice(ctx, f.alice, pv, "Hi, I'm **Ada**.")
	require.NoError(t, err)
	require.Equal(t, "ID3Hi, I'm Ada.", string(audio.Data))
	require.Len(t, meter.records, 1)
	require.Zero(t, f.files.count(), "a preview isn't stored")
	require.Equal(t, meter.records[0].SpeechCharacters, meter.records[0].SpeechTrackCharacters, "a preview is its own track")

	_, err = f.a.PreviewVoice(ctx, f.alice, pv, strings.Repeat("a", models.MaxPreviewChars+1))
	require.ErrorIs(t, err, ErrPreviewTooLong)
	meter.mu.Lock()
	meter.allow = false
	meter.mu.Unlock()
	_, err = f.a.PreviewVoice(ctx, f.alice, pv, "Hello.")
	require.ErrorIs(t, err, ErrQuotaExceeded)
}

func TestSweepSpeech_DeletesUnplayedAndOrphanedTracks(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "Keep me. And me.", 0)
	f.giveVoice(t, "")
	ctx := apicontext.WithUserID(context.Background(), f.alice)
	track, err := f.a.Listen(ctx, f.alice, f.reply)
	require.NoError(t, err)
	f.waitTrack(t, track.ID)
	require.Equal(t, 2, f.files.count())

	f.a.sweepSpeechOnce(ctx)
	require.Equal(t, 2, f.files.count(), "played recently, its message there: kept")

	require.NoError(t, f.ent.ChatMessage.DeleteOneID(f.reply).Exec(ctx))
	f.a.sweepSpeechOnce(ctx)
	require.Zero(t, f.files.count(), "the message is gone, and its audio with it")
	_, err = f.a.SpeechTrack(ctx, f.alice, track.ID)
	require.Error(t, err)
}

func TestTrimWithOffset(t *testing.T) {
	t.Parallel()
	text, off := trimWithOffset("  Let me look.\n\nFound it. ", 16)
	require.Equal(t, "Let me look.\n\nFound it.", text)
	require.Equal(t, "Found it.", text[off:])
	_, off = trimWithOffset("abc", 99)
	require.Equal(t, 3, off)
}

// The draft buffer marks where the final round starts, past the paragraph break it adds.
func TestDraftBuffer_FinalRoundStart(t *testing.T) {
	t.Parallel()
	b := &jobDraftDeltaBuffer{ds: nil}
	// Without a datastore the buffer records nothing; drive its fields directly.
	b.allText = "Checking."
	b.MarkRoundBoundary()
	b.mu.Lock()
	brk := paragraphBreakBefore(b.allText, "Done.")
	b.finalRoundStart = len(b.allText) + len(brk)
	b.allText += brk + "Done."
	b.mu.Unlock()
	text, off := b.streamed()
	require.Equal(t, "Done.", text[off:])
}

// A worker writes only while it holds the track's lease: one whose lease went to another
// worker adds no segment, bills nothing, and doesn't end the track.
func TestSpeechWorker_ALostLeaseWritesNothing(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "One. Two.", 0)
	f.giveVoice(t, "")
	ctx := apicontext.WithUserID(context.Background(), f.alice)
	track, _, err := f.a.ds.FindOrCreateSpeechTrack(ctx, datastore.NewSpeechTrack{
		UserID: f.alice, ChatID: f.chat, MessageID: f.reply, ModelID: f.voiceModel.ID, Voice: "af_bella", VoiceKey: "k",
	})
	require.NoError(t, err)
	stale, ok, err := f.a.ds.AcquireSpeechWorker(ctx, track.ID, time.Now().Add(-time.Second)) // already lapsed
	require.NoError(t, err)
	require.True(t, ok)
	fresh, ok, err := f.a.ds.AcquireSpeechWorker(ctx, track.ID, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.True(t, ok, "a lapsed lease can be taken")

	seg := models.SpeechSegment{Seq: 0, Text: "One.", FileKey: "k0", Format: models.AudioFormatMP3, SizeBytes: 3}
	require.ErrorIs(t, f.a.ds.AddSpeechSegment(ctx, track.ID, stale, seg), datastore.ErrSpeechLeaseLost)
	require.ErrorIs(t, f.a.ds.RenewSpeechWorker(ctx, track.ID, stale, time.Now().Add(time.Minute)), datastore.ErrSpeechLeaseLost)
	require.NoError(t, f.a.ds.FinishSpeechTrack(ctx, track.ID, stale, models.SpeechTrackFailed, "x"))
	require.NoError(t, f.a.ds.AddSpeechSegment(ctx, track.ID, fresh, seg))
	require.ErrorIs(t, f.a.ds.AddSpeechSegment(ctx, track.ID, fresh, seg), datastore.ErrSpeechSegmentExists, "not billed twice")

	got, err := f.a.ds.GetSpeechTrack(ctx, f.alice, track.ID)
	require.NoError(t, err)
	require.Equal(t, models.SpeechTrackOpen, got.Status, "the stale worker's finish didn't end its successor's track")
	require.Len(t, got.Segments, 1)
}

// A track whose worker went away mid-track is picked up by the client's next poll, and
// carries on from the text it had read, however that was cut.
func TestSpeechTrack_APollResumesAStalledTrackFromItsText(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "Sure. The rest of the first paragraph.\n\nA second paragraph.", 0)
	f.giveVoice(t, "")
	meter := &recordingMeter{allow: true}
	f.a.meter = meter
	ctx := apicontext.WithUserID(context.Background(), f.alice)
	v, err := f.a.resolveVoice(ctx, f.alice, models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella"})
	require.NoError(t, err)
	track, _, err := f.a.ds.FindOrCreateSpeechTrack(ctx, datastore.NewSpeechTrack{
		UserID: f.alice, ChatID: f.chat, MessageID: f.reply, ModelID: f.voiceModel.ID, Voice: "af_bella", VoiceKey: v.key(),
	})
	require.NoError(t, err)
	// A worker read "Sure." and died, its lease lapsing.
	dead, _, err := f.a.ds.AcquireSpeechWorker(ctx, track.ID, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, f.a.ds.AddSpeechSegment(ctx, track.ID, dead, models.SpeechSegment{Seq: 0, Text: "Sure.", FileKey: "old", Format: models.AudioFormatMP3, SizeBytes: 3}))
	require.NoError(t, f.ent.SpeechTrack.UpdateOneID(track.ID).SetWorkerUntil(time.Now().Add(-time.Second)).Exec(ctx))

	done := f.waitTrack(t, track.ID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Equal(t, []string{"The rest of the first paragraph.\n\nA second paragraph."}, f.spoken, "nothing read twice, nothing skipped")
	require.Len(t, done.Segments, 2)
	require.Equal(t, "Sure.", done.Segments[0].Text)
}

// Segments the reply no longer starts with (cut by a live speaker from narration, or
// by older rules) are withdrawn, and the track read again from the start.
func TestSpeechTrack_ResumingWithdrawsSegmentsTheReplyDoesntSay(t *testing.T) {
	t.Parallel()
	f := newSpeechFixture(t, "Fresh words.", 0)
	f.giveVoice(t, "")
	ctx := apicontext.WithUserID(context.Background(), f.alice)
	v, err := f.a.resolveVoice(ctx, f.alice, models.PersonalityVoice{ModelID: f.voiceModel.ID, Voice: "af_bella"})
	require.NoError(t, err)
	track, _, err := f.a.ds.FindOrCreateSpeechTrack(ctx, datastore.NewSpeechTrack{
		UserID: f.alice, ChatID: f.chat, MessageID: f.reply, ModelID: f.voiceModel.ID, Voice: "af_bella", VoiceKey: v.key(),
	})
	require.NoError(t, err)
	require.NoError(t, f.files.UploadFile(ctx, "narration", []byte("x"), "audio/mpeg"))
	dead, _, err := f.a.ds.AcquireSpeechWorker(ctx, track.ID, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, f.a.ds.AddSpeechSegment(ctx, track.ID, dead, models.SpeechSegment{Seq: 0, Text: "Let me look.", FileKey: "narration", Format: models.AudioFormatMP3, SizeBytes: 1}))
	require.NoError(t, f.ent.SpeechTrack.UpdateOneID(track.ID).SetWorkerUntil(time.Now().Add(-time.Second)).Exec(ctx))

	done := f.waitTrack(t, track.ID)
	require.Equal(t, models.SpeechTrackComplete, done.Status)
	require.Len(t, done.Segments, 1)
	require.Equal(t, "Fresh words.", done.Segments[0].Text)
	f.files.mu.Lock()
	_, kept := f.files.files["narration"]
	f.files.mu.Unlock()
	require.False(t, kept, "the withdrawn segment's audio is deleted")
}
