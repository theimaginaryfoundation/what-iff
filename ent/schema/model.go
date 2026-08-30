package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
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
			Values("openai", "anthropic", "zai", "google", "mistral", "deepseek", "qwen", "xiaomi").
			Default("openai").
			Comment("Provider backing this model"),
		field.Bool("tool_support").
			Default(false).
			Comment("Legacy coarse flag indicating whether the model supports tool calling"),
		field.JSON("capabilities", modeltypes.ModelCapabilities{}).
			Default(modeltypes.ModelCapabilities{}).
			Comment("Fine-grained model capabilities used for UI presentation and runtime tool exposure"),
		field.Int64("base_credits_per_slab").
			Default(1).
			Comment("Credits charged per token slab for chat/job turns using this model"),
		field.Enum("subscription_tier").
			Values("low", "medium", "high", "ultra").
			Default("high").
			Comment("Minimum tier required for free-chat access on this model"),
		field.Bool("deleted").
			Default(false).
			Comment("Whether the model is soft deleted"),
		field.Bool("is_default").
			Default(false).
			Comment("Whether this is the application-wide default model. At most one active model may have this set; enforced by the datastore. When none is set, the env/const default is used as a bootstrap fallback"),
	}
}
