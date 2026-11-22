package datastore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/predicate"
	"github.com/theimaginaryfoundation/what-iff/ent/speechsegment"
	"github.com/theimaginaryfoundation/what-iff/ent/speechtrack"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Speech (ADR 0x052): personalities' voices, and the speech tracks replies are read
// aloud from.

var (
	// ErrSpeechTrackNotFound is a track that doesn't exist, or isn't the account's.
	ErrSpeechTrackNotFound = errors.New("speech track not found")
	// ErrSpeechLeaseLost is a worker whose lease on a track lapsed and went to another,
	// or whose track is gone: it stops without writing.
	ErrSpeechLeaseLost = errors.New("the speech track's lease is held by another worker")
	// ErrSpeechSegmentExists is a segment the track already has.
	ErrSpeechSegmentExists = errors.New("the speech track already has this segment")
)

// SetPersonalityVoice sets the personality's voice, or clears it when voice is nil.
// The model must be a speech model the account can use, and the voice one it offers;
// otherwise models.ErrInvalidPersonalityVoice. A personality that isn't the account's
// is ErrPersonalityNotFound.
func (d *Datastore) SetPersonalityVoice(ctx context.Context, userID, personalityID uuid.UUID, voice *models.PersonalityVoice) (*models.Personality, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	owned, err := tx.Personality.Query().
		Where(personality.ID(personalityID), personality.HasUserWith(user.ID(userID))).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrPersonalityNotFound
	}
	update := tx.Personality.UpdateOneID(personalityID)
	if voice == nil {
		update.ClearVoiceModelID().SetVoice("").SetVoiceStyle("")
	} else {
		v := *voice
		v.Voice, v.Style = strings.TrimSpace(v.Voice), strings.TrimSpace(v.Style)
		if err := v.ValidateStyle(); err != nil {
			return nil, err
		}
		if err := assertUserCanUseModelOfKindTx(ctx, tx, userID, v.ModelID, models.ModelKindSpeech); err != nil {
			if errors.Is(err, ErrModelNotFound) {
				return nil, fmt.Errorf("%w: not a voice model you can use", models.ErrInvalidPersonalityVoice)
			}
			return nil, err
		}
		m, err := tx.Model.Query().Where(entmodel.ID(v.ModelID)).Only(ctx)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(m.SpeechVoices, func(o models.SpeechVoice) bool { return o.ID == v.Voice }) {
			return nil, fmt.Errorf("%w: %s doesn't offer the voice %q", models.ErrInvalidPersonalityVoice, m.DisplayName, v.Voice)
		}
		update.SetVoiceModelID(v.ModelID).SetVoice(v.Voice).SetVoiceStyle(v.Style)
	}
	if _, err := update.Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetPersonality(ctx, userID, personalityID)
}

func toSpeechTrack(e *ent.SpeechTrack) *models.SpeechTrack {
	t := &models.SpeechTrack{
		ID: e.ID, UserID: e.UserID, ChatID: e.ChatID, MessageID: e.MessageID, JobID: e.JobID,
		ModelID: e.ModelID, Voice: e.Voice, Style: e.Style, VoiceKey: e.VoiceKey,
		Status: models.SpeechTrackStatus(e.Status), ErrorCode: e.ErrorCode,
		Segments: []models.SpeechSegment{}, WorkerUntil: e.WorkerUntil,
	}
	segs := append([]*ent.SpeechSegment(nil), e.Edges.Segments...)
	slices.SortFunc(segs, func(a, b *ent.SpeechSegment) int { return a.Seq - b.Seq })
	for _, s := range segs {
		t.Segments = append(t.Segments, models.SpeechSegment{
			Seq: s.Seq, Text: s.Text, FileKey: s.FileKey, Format: models.AudioFormat(s.Format),
			DurationMS: s.DurationMs, SizeBytes: s.SizeBytes,
		})
	}
	return t
}

// NewSpeechTrack describes a track to find or create.
type NewSpeechTrack struct {
	UserID, ChatID, MessageID, ModelID uuid.UUID
	Voice, Style, VoiceKey             string
}

// FindOrCreateSpeechTrack returns the message's track in this voice, creating it
// open when there is none, and marks it played now. created says which.
func (d *Datastore) FindOrCreateSpeechTrack(ctx context.Context, n NewSpeechTrack) (*models.SpeechTrack, bool, error) {
	now := time.Now()
	find := func() (*ent.SpeechTrack, error) {
		return d.dbClient.SpeechTrack.Query().
			Where(speechtrack.UserID(n.UserID), speechtrack.MessageID(n.MessageID), speechtrack.VoiceKey(n.VoiceKey)).
			WithSegments().
			Only(ctx)
	}
	existing, err := find()
	if err == nil {
		if err := d.dbClient.SpeechTrack.UpdateOneID(existing.ID).SetLastPlayedAt(now).Exec(ctx); err != nil {
			return nil, false, err
		}
		return toSpeechTrack(existing), false, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, err
	}
	_, err = d.dbClient.SpeechTrack.Create().
		SetUserID(n.UserID).SetChatID(n.ChatID).SetMessageID(n.MessageID).
		SetModelID(n.ModelID).SetVoice(n.Voice).SetStyle(n.Style).SetVoiceKey(n.VoiceKey).
		SetLastPlayedAt(now).
		Save(ctx)
	if ent.IsConstraintError(err) {
		// Another request made it first.
		existing, err = find()
		if err != nil {
			return nil, false, err
		}
		return toSpeechTrack(existing), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	created, err := find()
	if err != nil {
		return nil, false, err
	}
	return toSpeechTrack(created), true, nil
}

// GetSpeechTrack returns the account's track with its segments.
func (d *Datastore) GetSpeechTrack(ctx context.Context, userID, trackID uuid.UUID) (*models.SpeechTrack, error) {
	e, err := d.dbClient.SpeechTrack.Query().
		Where(speechtrack.ID(trackID), speechtrack.UserID(userID)).
		WithSegments().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrSpeechTrackNotFound
	}
	if err != nil {
		return nil, err
	}
	return toSpeechTrack(e), nil
}

// AcquireSpeechWorker claims an open track for a synthesis worker until until, when
// no other worker holds it. It returns the worker's token, which every later write
// names, and whether it got the lease.
func (d *Datastore) AcquireSpeechWorker(ctx context.Context, trackID uuid.UUID, until time.Time) (uuid.UUID, bool, error) {
	token := uuid.New()
	n, err := d.dbClient.SpeechTrack.Update().
		Where(
			speechtrack.ID(trackID),
			speechtrack.StatusEQ(speechtrack.StatusOpen),
			speechtrack.Or(speechtrack.WorkerUntilIsNil(), speechtrack.WorkerUntilLT(time.Now())),
		).
		SetWorkerUntil(until).
		SetWorkerToken(token).
		Save(ctx)
	if err != nil || n != 1 {
		return uuid.Nil, false, err
	}
	return token, true, nil
}

// heldBy is the track while the worker with token holds it.
func heldBy(trackID, token uuid.UUID) predicate.SpeechTrack {
	return speechtrack.And(speechtrack.ID(trackID), speechtrack.WorkerToken(token), speechtrack.StatusEQ(speechtrack.StatusOpen))
}

// RenewSpeechWorker extends a worker's lease on its track, or is ErrSpeechLeaseLost.
func (d *Datastore) RenewSpeechWorker(ctx context.Context, trackID, token uuid.UUID, until time.Time) error {
	n, err := d.dbClient.SpeechTrack.Update().Where(heldBy(trackID, token)).SetWorkerUntil(until).Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSpeechLeaseLost
	}
	return nil
}

// AddSpeechSegment records a synthesised segment of a track, for the worker holding it:
// ErrSpeechLeaseLost when it no longer does, ErrSpeechSegmentExists when the track
// already has the segment (so it isn't billed twice).
func (d *Datastore) AddSpeechSegment(ctx context.Context, trackID, token uuid.UUID, s models.SpeechSegment) error {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Touching the row proves the lease and holds it until the segment is in.
	n, err := tx.SpeechTrack.Update().Where(heldBy(trackID, token)).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSpeechLeaseLost
	}
	exists, err := tx.SpeechSegment.Query().Where(speechsegment.TrackID(trackID), speechsegment.Seq(s.Seq)).Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return ErrSpeechSegmentExists
	}
	if err := tx.SpeechSegment.Create().
		SetTrackID(trackID).SetSeq(s.Seq).SetText(s.Text).SetFileKey(s.FileKey).
		SetFormat(string(s.Format)).SetDurationMs(s.DurationMS).SetSizeBytes(s.SizeBytes).
		Exec(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

// WithdrawSpeechSegments deletes a track's segments from seq on, for the worker holding
// it, returning their files for the caller to delete: segments that no longer match
// what the reply says.
func (d *Datastore) WithdrawSpeechSegments(ctx context.Context, trackID, token uuid.UUID, fromSeq int) ([]string, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := tx.SpeechTrack.Update().Where(heldBy(trackID, token)).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrSpeechLeaseLost
	}
	segs, err := tx.SpeechSegment.Query().
		Where(speechsegment.TrackID(trackID), speechsegment.SeqGTE(fromSeq)).All(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(segs))
	for _, s := range segs {
		keys = append(keys, s.FileKey)
	}
	if _, err := tx.SpeechSegment.Delete().
		Where(speechsegment.TrackID(trackID), speechsegment.SeqGTE(fromSeq)).Exec(ctx); err != nil {
		return nil, err
	}
	return keys, tx.Commit()
}

// FinishSpeechTrack ends a track's synthesis, releasing the lease. A worker names its
// token, and finishes only a track it still holds; uuid.Nil finishes one no worker holds.
// Either way, a track another worker holds is left to it.
func (d *Datastore) FinishSpeechTrack(ctx context.Context, trackID, token uuid.UUID, status models.SpeechTrackStatus, code string) error {
	where := heldBy(trackID, token)
	if token == uuid.Nil {
		where = speechtrack.And(speechtrack.ID(trackID),
			speechtrack.Or(speechtrack.WorkerUntilIsNil(), speechtrack.WorkerUntilLT(time.Now())))
	}
	_, err := d.dbClient.SpeechTrack.Update().Where(where).
		SetStatus(speechtrack.Status(status)).
		SetErrorCode(code).
		ClearWorkerUntil().
		ClearWorkerToken().
		Save(ctx)
	return err
}

// ReopenSpeechTrack opens a failed or stopped track again, keeping the segments it
// has, so asking for it once more tries the rest; it reports whether it did.
func (d *Datastore) ReopenSpeechTrack(ctx context.Context, trackID uuid.UUID) (bool, error) {
	n, err := d.dbClient.SpeechTrack.Update().
		Where(speechtrack.ID(trackID), speechtrack.StatusIn(speechtrack.StatusFailed, speechtrack.StatusStopped)).
		SetStatus(speechtrack.StatusOpen).
		SetErrorCode("").
		Save(ctx)
	return n == 1, err
}

// SpeechTrackFiles is a track the sweeper may delete, with its audio files.
type SpeechTrackFiles struct {
	ID    uuid.UUID
	Files []string
}

// messageGone is a track whose message was deleted.
func messageGone() predicate.SpeechTrack {
	return predicate.SpeechTrack(func(sel *sql.Selector) {
		msgs := sql.Table(chatmessage.Table)
		sel.Where(sql.And(
			sql.NotNull(sel.C(speechtrack.FieldMessageID)),
			sql.NotIn(sel.C(speechtrack.FieldMessageID), sql.Select(msgs.C(chatmessage.FieldID)).From(msgs)),
		))
	})
}

// sweepable is a track to delete: unplayed since cutoff, or whose message is gone.
func sweepable(cutoff time.Time) predicate.SpeechTrack {
	return speechtrack.Or(speechtrack.LastPlayedAtLT(cutoff), messageGone())
}

// SweepableSpeechTracks lists up to limit tracks to delete: unplayed since cutoff, or
// whose message is gone. Deleting them takes them off the list, so each sweep reaches
// further.
func (d *Datastore) SweepableSpeechTracks(ctx context.Context, cutoff time.Time, limit int) ([]SpeechTrackFiles, error) {
	rows, err := d.dbClient.SpeechTrack.Query().
		Where(sweepable(cutoff)).
		Order(ent.Asc(speechtrack.FieldLastPlayedAt)).
		WithSegments().
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SpeechTrackFiles, 0, len(rows))
	for _, t := range rows {
		f := SpeechTrackFiles{ID: t.ID}
		for _, s := range t.Edges.Segments {
			f.Files = append(f.Files, s.FileKey)
		}
		out = append(out, f)
	}
	return out, nil
}

// SweepSpeechTrack deletes a track the sweeper listed, with its segments' rows, if it
// is still sweepable: one played since it was listed is kept. It returns the files of
// the segments it deleted, for the caller to delete after, and whether it deleted it.
func (d *Datastore) SweepSpeechTrack(ctx context.Context, trackID uuid.UUID, cutoff time.Time) ([]string, bool, error) {
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	// Locks the row against a Listen marking it played until the delete commits.
	n, err := tx.SpeechTrack.Update().Where(speechtrack.ID(trackID), sweepable(cutoff)).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil || n == 0 {
		return nil, false, err
	}
	segs, err := tx.SpeechSegment.Query().Where(speechsegment.TrackID(trackID)).All(ctx)
	if err != nil {
		return nil, false, err
	}
	keys := make([]string, 0, len(segs))
	for _, s := range segs {
		keys = append(keys, s.FileKey)
	}
	if _, err := tx.SpeechSegment.Delete().Where(speechsegment.TrackID(trackID)).Exec(ctx); err != nil {
		return nil, false, err
	}
	if err := tx.SpeechTrack.DeleteOneID(trackID).Exec(ctx); err != nil {
		return nil, false, err
	}
	return keys, true, tx.Commit()
}

// DeleteSpeechTrack deletes a track and its segments' rows; its files are the
// caller's to delete first.
func (d *Datastore) DeleteSpeechTrack(ctx context.Context, trackID uuid.UUID) error {
	if _, err := d.dbClient.SpeechSegment.Delete().Where(speechsegment.TrackID(trackID)).Exec(ctx); err != nil {
		return err
	}
	err := d.dbClient.SpeechTrack.DeleteOneID(trackID).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}
