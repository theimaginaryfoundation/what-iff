package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"

	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// Model holds the schema definition for the Model entity.
type Model struct {
	ent.Schema
}

// Fields of the Model.
func (Model) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.String("name").
			NotEmpty().
			Comment("The name of the model"),
		field.String("display_name").
			NotEmpty().
			Comment("The user-facing display name of the model"),
		field.String("description"),
		field.Enum("provider").
			Values("openai", "anthropic", "zai", "google", "mistral", "deepseek", "qwen", "xiaomi",
				"openrouter", "ollama", "lmstudio", "custom").
			Default("openai").
			Comment("Provider backing this model: a built-in provider, or for a model from a provider connection, the preset it was made from (ADR 0x027). Stored as text, so adding a value needs no migration"),
		field.UUID("connection_id", uuid.UUID{}).
			Optional().
			Nillable().
			Immutable().
			Comment("The provider connection serving this model (ADR 0x027). Empty for the deployment's own catalog; set, the model is visible only to whoever can use the connection, and is deleted with it"),
		field.Bool("tool_support").
			Default(false).
			Comment("Whether the model supports tool calling"),
		field.Enum("reasoning_support_override").
			Values("none", "optional", "toggle", "always").
			Optional().
			Nillable().
			Comment("Admin override of how the model takes a reasoning effort (ADR 0x026). NULL derives it from the provider and name (models.ReasoningSupportFor), so a misclassified model is fixed without a deploy."),
		field.Bool("vision_support").
			Optional().
			Nillable().
			Comment("Whether the model accepts image input. NULL only on rows predating the column (every deployment has long since backfilled them); text-only models get images stripped"),
		field.Int64("base_credits_per_slab").
			Default(1).
			Comment("Credits charged per token slab for chat/job turns using this model"),
		// Speech is priced per character rather than by base_credits_per_slab: the slab is
		// 15,000 tokens, rounded up per call, and synthesis is billed per segment (a sentence
		// to a paragraph). At ~1,500 audio tokens a minute a slab is ~10 minutes of audio, so
		// slab pricing would charge a 30-second reply as ten minutes. Characters are also what
		// every speech route can count (the OpenAI-style route reports no tokens) and what a
		// cost can be shown in before anything is synthesised. See ADR 0x052 §11.
		field.Float("credits_per_1k_chars").
			Default(0).
			Min(0).
			Comment("Credits charged per 1,000 characters a speech model reads aloud (ADR 0x052 §11), for a linked meter; like base_credits_per_slab, it has no effect without one. Not slab-priced: a 15k-token slab is ~10 minutes of audio, rounded up per segment"),
		field.Float("credits_per_minute").
			Default(0).
			Min(0).
			Comment("Credits charged per minute a transcription model transcribes (ADR 0x052 §11), for a linked meter"),
		field.Enum("subscription_tier").
			Values("low", "medium", "high", "ultra").
			Default("high").
			Comment("Billing grade a linked meter may use; it has no effect without one"),
		field.Enum("kind").
			Values("chat", "speech", "transcription").
			Default("chat").
			Comment("What the model does (ADR 0x052): chat, speech synthesis, or transcription. Only chat models are offered for chat, subagents and the text utility roles"),
		field.JSON("speech_voices", []modeltypes.SpeechVoice{}).
			Optional().
			Comment("The voices a speech model offers (ADR 0x052 §2): the known table's by default, narrowed or relabelled by whoever configures voice. Empty for other kinds"),
		field.Bool("deleted").
			Default(false).
			Comment("Whether the model is soft deleted"),
		field.Bool("is_default").
			Default(false).
			Comment("Whether this is the application-wide default model. At most one active model may have this set; enforced by the datastore. When none is set, the env/const default is used as a bootstrap fallback"),
	}
}

// Edges of the Model.
func (Model) Edges() []ent.Edge {
	return []ent.Edge{
		// One-way, so ProviderConnection's schema does not change. Deleting a
		// connection (or the account that owns it) deletes its models: a chat on
		// one falls back to the default model, and a job on one fails clearly.
		// Unique() is the model's side: each model has at most one connection, and
		// a connection has many models (many-to-one; connection_id alone is not
		// unique, only (connection_id, name) is).
		edge.To("connection", ProviderConnection.Type).
			Field("connection_id").
			Unique().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

// Indexes of the Model.
func (Model) Indexes() []ent.Index {
	return []ent.Index{
		// One row per model per connection: disabling a model soft-deletes it, and
		// enabling it again brings the same row back.
		index.Fields("connection_id", "name").Unique(),
	}
}
