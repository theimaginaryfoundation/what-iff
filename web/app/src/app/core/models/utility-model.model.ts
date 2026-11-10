import { ReasoningEffort } from './turn-settings.model';

/** A group of background work that runs on one model (ADR 0x027 section 6). */
export type UtilityRole = 'memory' | 'memory_merge' | 'simple' | 'personality';

/** How fast, and at what price, a role's calls ask to be processed; only some models offer it. */
export type UtilityServiceTier = 'priority' | 'flex';

/** One scope's choice for a role: a model, with how it's asked to work, or off. */
export interface UtilityChoice {
  off: boolean;
  model_id: string | null;
  /** Null leaves the model's own default. */
  reasoning_effort: ReasoningEffort | null;
  /** Null leaves the API's default. */
  service_tier: UtilityServiceTier | null;
}

/** One role's choices. With neither, the role is not set, and its work doesn't run. */
export interface UtilityRoleModel {
  role: UtilityRole;
  account: UtilityChoice | null;
  deployment: UtilityChoice | null;
}

export interface UtilityModels {
  /** Whether the account may choose: only where it brings its own keys, and so pays. */
  accounts_choose: boolean;
  roles: UtilityRoleModel[];
}

/** What choosing a model for a role sends. */
export interface UtilityModelChoice {
  model_id: string;
  reasoning_effort?: ReasoningEffort | null;
  service_tier?: UtilityServiceTier | null;
}

/** Why a background step on a chat didn't go as it should. */
export type BackgroundNoticeCode = 'provider_not_configured' | 'provider_error' | 'model_unavailable' | 'model_not_set';

/**
 * Background work on a chat that failed or fell back, until the same step succeeds. `step` is a
 * checkpoint's (`scratchpad`, `memory`, `summary`), a turn's own call (`memory_query`,
 * `chat_name`, `mood_select`, `expression_pick`), or `fallback_<role>` for a role that passed
 * over its chosen model. `role` is the role the step runs on, missing for a step this app
 * doesn't know.
 */
export interface BackgroundNotice {
  id: string;
  chat_id: string;
  step: string;
  role?: UtilityRole;
  code: BackgroundNoticeCode;
  provider?: string;
  model?: string;
  count: number;
  created_at: string;
  updated_at: string;
}
