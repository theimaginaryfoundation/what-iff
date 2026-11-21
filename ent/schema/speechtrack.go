package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// SpeechTrack is a reply's audio in one voice (ADR 0x052 §7): its segments, in
// order. A message has at most one track per voice key, so playing a reply again
// in the same voice replays the stored audio.
//
// The message and chat are plain ids, not edges: a message or chat deleted
// leaves its tracks for the speech sweeper, which deletes their audio files
// with them; a cascade would drop the rows and strand the files. Deleting the
// account deletes its tracks, and its files go with the account's own.
type SpeechTrack struct {
	ent.Schema
}

func (SpeechTrack) Mixin() []ent.Mixin {
	return []ent.Mixin{TimeMixin{}}
}

func (SpeechTrack) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("user_id", uuid.UUID{}).Immutable(),
		field.UUID("chat_id", uuid.UUID{}).Immutable(),
		// The reply, once saved; empty while the turn that writes it runs.
		field.UUID("message_id", uuid.UUID{}).Optional().Nillable(),
		// The turn writing the reply, while it runs.
		field.UUID("job_id", uuid.UUID{}).Optional().Nillable(),
		// The voice: a speech model, one of its voices, and the style it was read in.
		field.UUID("model_id", uuid.UUID{}).Immutable(),
		field.String("voice").NotEmpty().Immutable(),
		field.Text("style").Default("").Immutable(),
		// voice_key is a hash of the three, so one track per message per voice.
		field.String("voice_key").NotEmpty().Immutable(),
		// open (segments still coming), complete, failed, or stopped (the meter refused).
		field.Enum("status").Values("open", "complete", "failed", "stopped").Default("open"),
		// Why a failed track failed, as a stable code; empty otherwise.
		field.String("error_code").Default(""),
		// The synthesis worker's lease: past it, a track still open has no worker and a
		// request for it starts one.
		field.Time("worker_until").Optional().Nillable(),
		// Autoplay (ADR 0x052 §9): pending from the turn that writes the reply when its chat
		// reads replies aloud, then played by the one client that claims it, or skipped.
		// NULL for a track made by Listen.
		field.Enum("autoplay").Values("pending", "played", "skipped").Optional().Nillable(),
		// When the track was last asked for; the sweeper deletes tracks unplayed for 30 days.
		field.Time("last_played_at"),
	}
}

func (SpeechTrack) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("owner", User.Type).
			Field("user_id").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("segments", SpeechSegment.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SpeechTrack) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("message_id", "voice_key").Unique(),
		index.Fields("job_id"),
		index.Fields("chat_id", "autoplay"),
		index.Fields("last_played_at"),
	}
}

func (SpeechTrack) Annotations() []schema.Annotation {
	return []schema.Annotation{
		schema.Comment("A reply's audio in one voice (ADR 0x052): segments synthesised in order, kept 30 days after last play."),
		entsql.WithComments(true),
	}
}

// SpeechSegment is one synthesised piece of a track: a sentence or a paragraph,
// one audio file in the file store.
type SpeechSegment struct {
	ent.Schema
}

func (SpeechSegment) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("track_id", uuid.UUID{}).Immutable(),
		field.Int("seq").NonNegative().Immutable(),
		field.Text("text").Immutable(),
		field.String("file_key").NotEmpty().Immutable(),
		field.String("format").NotEmpty().Immutable(),
		field.Int("duration_ms").NonNegative().Default(0),
		field.Int("size_bytes").NonNegative().Immutable(),
	}
}

func (SpeechSegment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("track", SpeechTrack.Type).
			Ref("segments").
			Field("track_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (SpeechSegment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("track_id", "seq").Unique(),
	}
}
