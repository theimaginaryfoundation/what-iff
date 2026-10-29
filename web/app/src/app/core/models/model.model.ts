import { ReasoningSupport } from './turn-settings.model';

export interface Model {
  id: string;
  name: string;
  display_name: string;
  description: string;
  /** Provider backend for this model (e.g. openai, anthropic). */
  provider?: string;
  tool_support: boolean;
  /**
   * Whether the model accepts image input. Optional for rollout safety; absence
   * is treated as vision-capable so no icon shows until the backend says otherwise.
   */
  vision_support?: boolean;
  /**
   * Credits consumed per billing increment (≈ 15k input tokens for chat).
   * Optional so older backend responses remain safe during a rollout.
   * The backend schema has a non-zero default, so absence means "not yet returned".
   */
  base_credits_per_slab?: number;
  /**
   * Billing grade: 'low', 'medium', 'high' or 'ultra'. The API sends it only
   * where the deployment bills by tier, so it is absent otherwise.
   */
  subscription_tier?: string;
  /**
   * How the model takes a reasoning effort (ADR 0x026). Optional for rollout safety; absence is
   * treated as `none` (no control shown).
   */
  reasoning_support?: ReasoningSupport;
  /** Admin-only: an override of `reasoning_support` (which then equals it); null derives it from the provider and model. */
  reasoning_support_override?: ReasoningSupport | null;
}

