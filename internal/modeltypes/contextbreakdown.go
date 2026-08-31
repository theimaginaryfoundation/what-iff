// Package modeltypes holds plain value types shared between the models layer and
// the Ent schema. It imports no generated Ent code (nor internal/models), so the
// Ent schema can depend on it without creating a schema → models → generated-ent
// bootstrap cycle when the generated code is absent (it is produced by
// `go generate ./ent`, not checked in).
package modeltypes

import "time"

// ContextBreakdownVersion is the schema version stamped on every persisted ContextBreakdown.
// Bump it when the shape changes in a way readers must branch on (additive fields do not need
// a bump; tolerant unmarshal handles those). Stored so old rows can be interpreted or skipped.
const ContextBreakdownVersion = 1

// ContextSegmentStat is a single row of the per-turn context breakdown: one model-context
// segment kind, how many segments of that kind were present, their aggregated estimated
// token count, whether any were part of the cacheable prompt prefix, and how many image
// payloads they carried. Named buckets are estimates except vendor_prompt_other, which
// reconciles the snapshot to vendor-reported input usage when available.
type ContextSegmentStat struct {
	Kind      string `json:"kind"`
	Segments  int    `json:"segments"`
	Tokens    int    `json:"tokens"`
	Cacheable bool   `json:"cacheable"`
	Images    int    `json:"images,omitempty"`
}

// APICostEstimate records the approximate standard API cost for a BYOK/self-hosted
// turn together with the exact usage and price snapshot used to calculate it. The
// estimate is deliberately persisted so later price-table updates do not rewrite history.
type APICostEstimate struct {
	AmountUSD           float64   `json:"amount_usd"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	InputUSDPerMillion  float64   `json:"input_usd_per_million"`
	OutputUSDPerMillion float64   `json:"output_usd_per_million"`
	PricingSource       string    `json:"pricing_source"`
	PricingCheckedAt    time.Time `json:"pricing_checked_at"`
	CalculatedAt        time.Time `json:"calculated_at"`
}

// ContextBreakdown is a snapshot of the model context assembled for one assistant turn:
// the segment rows in the order they were laid out, the estimated total, the display
// budget, and the model/provider that consumed it. It is captured at generation time and
// persisted on the assistant ChatMessage so the UI can show "what was in the model's head"
// for that reply. The total uses vendor-reported input usage when available; named
// context and tool-definition buckets are cl100k estimates, with a remainder bucket
// for vendor framing, image input, and other un-attributable tokens.
type ContextBreakdown struct {
	// Version is the ContextBreakdownVersion in effect when this snapshot was written.
	Version      int                  `json:"version"`
	Segments     []ContextSegmentStat `json:"segments"`
	TotalTokens  int                  `json:"total_tokens"`
	BudgetTokens int                  `json:"budget_tokens"`
	Model        string               `json:"model,omitempty"`
	Provider     string               `json:"provider,omitempty"`
	CapturedAt   time.Time            `json:"captured_at"`
	// APICost is present only when this exact provider/model has a reviewed standard
	// token price. Nil means the application does not know enough to estimate safely.
	APICost *APICostEstimate `json:"api_cost,omitempty"`
}
