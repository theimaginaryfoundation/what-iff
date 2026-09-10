import { ContextBreakdown } from '../../../core/models/message.model';
import { estimateInputAPICost } from './api-pricing.helpers';

function breakdown(overrides: Partial<ContextBreakdown> = {}): ContextBreakdown {
  return {
    segments: [],
    total_tokens: 1_000_000,
    budget_tokens: 30_000,
    model: 'gpt-5.4',
    provider: 'openai',
    captured_at: '2026-08-30T00:00:00Z',
    ...overrides,
  };
}

describe('estimateInputAPICost', () => {
  it('uses the exact reviewed provider/model rate', () => {
    const estimate = estimateInputAPICost(breakdown());

    expect(estimate?.amountUsd).toBe(2.5);
    expect(estimate?.inputUsdPerMillion).toBe(2.5);
    expect(estimate?.pricingCheckedAt).toBe('2026-08-30');
    expect(estimate?.pricingSource).toContain('openai.com');
  });

  it('supports reviewed Anthropic and Gemini seed models', () => {
    expect(estimateInputAPICost(breakdown({ provider: 'anthropic', model: 'claude-sonnet-4-6' }))?.amountUsd).toBe(3);
    expect(estimateInputAPICost(breakdown({ provider: 'google', model: 'gemini-3.5-flash' }))?.amountUsd).toBe(1.5);
  });

  it('does not guess pricing for an unknown or local model', () => {
    expect(estimateInputAPICost(breakdown({ provider: 'local', model: 'llama-custom' }))).toBeNull();
    expect(estimateInputAPICost(breakdown({ provider: 'openai', model: 'future-model' }))).toBeNull();
  });
});
