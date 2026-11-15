package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// ShareUnlockAttempt is one try at a share link's passcode (ADR 0x051 §4), by address: what the
// rate limit and the bans count. The address is a keyed hash, never the address itself. Attempts
// older than a day are deleted, as are bans that ended more than a day ago.
type ShareUnlockAttempt struct {
	ent.Schema
}

// Fields of the ShareUnlockAttempt.
func (ShareUnlockAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("address_key").
			NotEmpty().
			Immutable(),
		// A try is recorded as failed until its passcode is known to be right, so tries that arrive
		// together count each other (ADR 0x051 §4).
		field.Bool("failed").
			Default(true),
		field.Time("at").
			Default(time.Now).
			Immutable(),
	}
}

// Indexes of the ShareUnlockAttempt.
func (ShareUnlockAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("address_key", "at"),
		index.Fields("at"),
	}
}

// ShareBan keeps an address from unlocking any share until a time (ADR 0x051 §4), after too many
// wrong passcodes. Strikes count the bans in a row, which make the next one longer.
type ShareBan struct {
	ent.Schema
}

// Fields of the ShareBan.
func (ShareBan) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("address_key").
			NotEmpty().
			Unique().
			Immutable(),
		field.Time("until"),
		field.Int("strikes").
			Default(1),
	}
}
