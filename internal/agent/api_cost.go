package agent

import (
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// apiTokenPrice is a reviewed standard pay-as-you-go text-token rate. Prices are
// intentionally keyed by exact provider + model: unknown/custom models do not inherit
// a nearby model's price. Tool-call fees, cached-input discounts, batch/priority tiers,
// regional uplifts, and provider-specific promotions are not represented here.
type apiTokenPrice struct {
	InputUSDPerMillion  float64
	OutputUSDPerMillion float64
	Source              string
	CheckedAt           time.Time
}

var pricingCheckedAt = time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC)

var apiTokenPrices = map[string]apiTokenPrice{
	// OpenAI standard API rates, reviewed against official model pricing pages.
	"openai/gpt-5.4":      {2.50, 15.00, "https://developers.openai.com/api/docs/models/gpt-5.4", pricingCheckedAt},
	"openai/gpt-5.1":      {1.25, 10.00, "https://developers.openai.com/api/docs/models/gpt-5.1", pricingCheckedAt},
	"openai/gpt-5":        {1.25, 10.00, "https://developers.openai.com/api/docs/models/gpt-5", pricingCheckedAt},
	"openai/gpt-5-mini":   {0.25, 2.00, "https://developers.openai.com/api/docs/models/gpt-5-mini", pricingCheckedAt},
	"openai/gpt-4.1-mini": {0.40, 1.60, "https://developers.openai.com/api/docs/models/gpt-4.1-mini", pricingCheckedAt},
	"openai/gpt-4o":       {2.50, 10.00, "https://developers.openai.com/api/docs/models/gpt-4o", pricingCheckedAt},
	"openai/gpt-4o-mini":  {0.15, 0.60, "https://developers.openai.com/api/docs/models/gpt-4o-mini", pricingCheckedAt},

	// Anthropic standard global Claude API rates.
	"anthropic/claude-sonnet-4-6": {3.00, 15.00, "https://www.anthropic.com/news/claude-sonnet-4-6", pricingCheckedAt},
	"anthropic/claude-haiku-4-5":  {1.00, 5.00, "https://www.anthropic.com/claude/haiku", pricingCheckedAt},

	// Google Gemini Developer API standard paid-tier rate.
	"google/gemini-3.5-flash": {1.50, 9.00, "https://ai.google.dev/gemini-api/docs/pricing", pricingCheckedAt},
}

func estimateAPICost(providerName, modelName string, inputTokens, outputTokens int64, calculatedAt time.Time) *modeltypes.APICostEstimate {
	key := strings.ToLower(strings.TrimSpace(providerName)) + "/" + strings.ToLower(strings.TrimSpace(modelName))
	price, ok := apiTokenPrices[key]
	if !ok || inputTokens < 0 || outputTokens < 0 {
		return nil
	}
	if calculatedAt.IsZero() {
		calculatedAt = time.Now().UTC()
	}
	amount := (float64(inputTokens)/1_000_000)*price.InputUSDPerMillion +
		(float64(outputTokens)/1_000_000)*price.OutputUSDPerMillion
	return &modeltypes.APICostEstimate{
		AmountUSD:           amount,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		InputUSDPerMillion:  price.InputUSDPerMillion,
		OutputUSDPerMillion: price.OutputUSDPerMillion,
		PricingSource:       price.Source,
		PricingCheckedAt:    price.CheckedAt,
		CalculatedAt:        calculatedAt.UTC(),
	}
}
