package datastore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/chatmessage"
	entmodel "github.com/theimaginaryfoundation/what-iff/ent/model"
	"github.com/theimaginaryfoundation/what-iff/ent/personality"
	"github.com/theimaginaryfoundation/what-iff/ent/speechsegment"
	"github.com/theimaginaryfoundation/what-iff/ent/speechtrack"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Speech (ADR 0x052): personalities' voices, and the speech tracks replies are read
// aloud from.

// ErrSpeechTrackNotFound is a track that doesn't exist, or isn't the account's.
var ErrSpeechTrackNotFound = errors.New("speech track not found")

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
		Segments: []models.SpeechSegment{},
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
// no other worker holds it; it reports whether it did.
func (d *Datastore) AcquireSpeechWorker(ctx context.Context, trackID uuid.UUID, until time.Time) (bool, error) {
	n, err := d.dbClient.SpeechTrack.Update().
		Where(
			speechtrack.ID(trackID),
			speechtrack.StatusEQ(speechtrack.StatusOpen),
			speechtrack.Or(speechtrack.WorkerUntilIsNil(), speechtrack.WorkerUntilLT(time.Now())),
		).
		SetWorkerUntil(until).
		Save(ctx)
	return n == 1, err
}

// RenewSpeechWorker extends a worker's lease on its track.
func (d *Datastore) RenewSpeechWorker(ctx context.Context, trackID uuid.UUID, until time.Time) error {
	return d.dbClient.SpeechTrack.UpdateOneID(trackID).SetWorkerUntil(until).Exec(ctx)
}

// AddSpeechSegment records a synthesised segment of a track.
func (d *Datastore) AddSpeechSegment(ctx context.Context, trackID uuid.UUID, s models.SpeechSegment) error {
	err := d.dbClient.SpeechSegment.Create().
		SetTrackID(trackID).SetSeq(s.Seq).SetText(s.Text).SetFileKey(s.FileKey).
		SetFormat(string(s.Format)).SetDurationMs(s.DurationMS).SetSizeBytes(s.SizeBytes).
		Exec(ctx)
	if ent.IsConstraintError(err) {
		return nil // already there: a worker that lost its lease and came back
	}
	return err
}

// FinishSpeechTrack ends a track's synthesis, releasing the worker's lease.
func (d *Datastore) FinishSpeechTrack(ctx context.Context, trackID uuid.UUID, status models.SpeechTrackStatus, code string) error {
	return d.dbClient.SpeechTrack.UpdateOneID(trackID).
		SetStatus(speechtrack.Status(status)).
		SetErrorCode(code).
		ClearWorkerUntil().
		Exec(ctx)
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

// SweepableSpeechTracks lists up to limit tracks to delete: unplayed since cutoff, or
// whose message is gone.
func (d *Datastore) SweepableSpeechTracks(ctx context.Context, cutoff time.Time, limit int) ([]SpeechTrackFiles, error) {
	stale, err := d.dbClient.SpeechTrack.Query().
		Where(speechtrack.LastPlayedAtLT(cutoff)).
		WithSegments().
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	// Tracks whose message was deleted, a batch at a time.
	candidates, err := d.dbClient.SpeechTrack.Query().
		Where(speechtrack.MessageIDNotNil(), speechtrack.LastPlayedAtGTE(cutoff)).
		Order(ent.Asc(speechtrack.FieldLastPlayedAt)).
		Limit(limit).
		WithSegments().
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, t := range candidates {
		ids = append(ids, *t.MessageID)
	}
	alive, err := d.dbClient.ChatMessage.Query().Where(chatmessage.IDIn(ids...)).IDs(ctx)
	if err != nil {
		return nil, err
	}
	exists := make(map[uuid.UUID]bool, len(alive))
	for _, id := range alive {
		exists[id] = true
	}
	var out []SpeechTrackFiles
	add := func(t *ent.SpeechTrack) {
		f := SpeechTrackFiles{ID: t.ID}
		for _, s := range t.Edges.Segments {
			f.Files = append(f.Files, s.FileKey)
		}
		out = append(out, f)
	}
	for _, t := range stale {
		add(t)
	}
	for _, t := range candidates {
		if !exists[*t.MessageID] {
			add(t)
		}
	}
	return out, nil
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
