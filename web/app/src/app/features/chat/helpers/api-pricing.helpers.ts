import { ContextBreakdown } from '../../../core/models/message.model';

export interface InputAPICostEstimate {
  amountUsd: number;
  inputUsdPerMillion: number;
  pricingSource: string;
  pricingCheckedAt: string;
}

interface InputTokenPrice {
  inputUsdPerMillion: number;
  pricingSource: string;
  pricingCheckedAt: string;
}

const PRICING_CHECKED_AT = '2026-08-30';

// Standard pay-as-you-go text-input rates reviewed against official provider pricing.
// Exact provider/model keys are deliberate: custom and unknown models must not inherit a
// nearby model's price. Cached-input discounts, batch/priority tiers, regional uplifts,
// tool-call fees, and promotional account-specific pricing are outside this estimate.
const INPUT_TOKEN_PRICES: Record<string, InputTokenPrice> = {
  'openai/gpt-5.4': {
    inputUsdPerMillion: 2.5,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-5.4',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'openai/gpt-5.1': {
    inputUsdPerMillion: 1.25,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-5.1',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'openai/gpt-5': {
    inputUsdPerMillion: 1.25,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-5',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'openai/gpt-5-mini': {
    inputUsdPerMillion: 0.25,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-5-mini',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'openai/gpt-4.1-mini': {
    inputUsdPerMillion: 0.4,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-4.1-mini',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'openai/gpt-4o': {
    inputUsdPerMillion: 2.5,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-4o',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'openai/gpt-4o-mini': {
    inputUsdPerMillion: 0.15,
    pricingSource: 'https://developers.openai.com/api/docs/models/gpt-4o-mini',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'anthropic/claude-sonnet-4-6': {
    inputUsdPerMillion: 3,
    pricingSource: 'https://www.anthropic.com/news/claude-sonnet-4-6',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'anthropic/claude-haiku-4-5': {
    inputUsdPerMillion: 1,
    pricingSource: 'https://www.anthropic.com/claude/haiku',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
  'google/gemini-3.5-flash': {
    inputUsdPerMillion: 1.5,
    pricingSource: 'https://ai.google.dev/gemini-api/docs/pricing',
    pricingCheckedAt: PRICING_CHECKED_AT,
  },
};

export function estimateInputAPICost(breakdown: ContextBreakdown | null | undefined): InputAPICostEstimate | null {
  if (!breakdown?.provider || !breakdown.model || !Number.isFinite(breakdown.total_tokens) || breakdown.total_tokens < 0) {
    return null;
  }
  const key = `${breakdown.provider.trim().toLowerCase()}/${breakdown.model.trim().toLowerCase()}`;
  const price = INPUT_TOKEN_PRICES[key];
  if (!price) return null;

  return {
    amountUsd: (breakdown.total_tokens / 1_000_000) * price.inputUsdPerMillion,
    inputUsdPerMillion: price.inputUsdPerMillion,
    pricingSource: price.pricingSource,
    pricingCheckedAt: price.pricingCheckedAt,
  };
}
