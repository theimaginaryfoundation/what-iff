export interface Model {
  id: string;
  name: string;
  display_name: string;
  description: string;
  /** Provider backend for this model (e.g. openai, anthropic). */
  provider?: string;
  tool_support: boolean;
  /**
   * Credits consumed per billing increment (≈ 15k input tokens for chat).
   * Optional so older backend responses remain safe during a rollout.
   * The backend schema has a non-zero default, so absence means "not yet returned".
   */
  base_credits_per_slab?: number;
  /**
   * Subscription tier this model is accessible from (e.g. 'casual', 'regular').
   * Optional for the same rollout-safety reason.
   */
  subscription_tier?: string;
}

