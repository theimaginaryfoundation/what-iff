package models

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxVoiceStyleChars bounds a personality's voice style (ADR 0x052 §5).
const MaxVoiceStyleChars = 500

// MaxPreviewChars bounds a voice preview's text.
const MaxPreviewChars = 300

// PersonalityVoice is how a personality sounds: a speech model, one of the voices it
// offers, and a style instruction for models that take one.
type PersonalityVoice struct {
	ModelID uuid.UUID `json:"model_id"`
	Voice   string    `json:"voice"`
	Style   string    `json:"style"`
}

// ErrInvalidPersonalityVoice is a voice that can't be set: not a speech model the
// account can use, a voice the model doesn't offer, or an overlong style.
var ErrInvalidPersonalityVoice = errors.New("that voice isn't available")

// ValidateStyle checks the style's length.
func (v PersonalityVoice) ValidateStyle() error {
	if utf8.RuneCountInString(v.Style) > MaxVoiceStyleChars {
		return fmt.Errorf("%w: a voice style is at most %d characters", ErrInvalidPersonalityVoice, MaxVoiceStyleChars)
	}
	return nil
}

// SpeechTrackStatus is where a track's synthesis is.
type SpeechTrackStatus string

const (
	SpeechTrackOpen     SpeechTrackStatus = "open"
	SpeechTrackComplete SpeechTrackStatus = "complete"
	SpeechTrackFailed   SpeechTrackStatus = "failed"
	// SpeechTrackStopped is a track the meter refused partway: it keeps what it has.
	SpeechTrackStopped SpeechTrackStatus = "stopped"
)

// SpeechTrack is a reply's audio in one voice (ADR 0x052 §7).
type SpeechTrack struct {
	ID        uuid.UUID         `json:"id"`
	UserID    uuid.UUID         `json:"-"`
	ChatID    uuid.UUID         `json:"chat_id"`
	MessageID *uuid.UUID        `json:"message_id,omitempty"`
	JobID     *uuid.UUID        `json:"-"`
	ModelID   uuid.UUID         `json:"-"`
	Voice     string            `json:"-"`
	Style     string            `json:"-"`
	VoiceKey  string            `json:"-"`
	Status    SpeechTrackStatus `json:"status"`
	ErrorCode string            `json:"error_code,omitempty"`
	Segments  []SpeechSegment   `json:"segments"`
}

// SpeechSegment is one synthesised piece of a track.
type SpeechSegment struct {
	Seq        int         `json:"seq"`
	Text       string      `json:"-"`
	FileKey    string      `json:"-"`
	Format     AudioFormat `json:"format"`
	DurationMS int         `json:"duration_ms"`
	SizeBytes  int         `json:"size_bytes"`
}
