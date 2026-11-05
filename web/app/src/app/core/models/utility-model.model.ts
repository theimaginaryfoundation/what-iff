/** A group of background work that runs on one model (ADR 0x027 section 6). */
export type UtilityRole = 'memory' | 'quick' | 'personality';

/** One role's models: the account's choice, the deployment's, and the built-in fallback. */
export interface UtilityRoleModel {
  role: UtilityRole;
  account_model_id: string | null;
  deployment_model_id: string | null;
  /** The name of the model the role runs on when neither chose one. */
  built_in_model: string;
}

export interface UtilityModels {
  roles: UtilityRoleModel[];
}

/** Why a background step on a chat didn't go as it should. */
export type BackgroundNoticeCode = 'provider_not_configured' | 'provider_error' | 'model_unavailable';

/**
 * Background work on a chat that failed or fell back, until the same step succeeds. `step` is
 * `scratchpad`, `memory` or `summary`, or `fallback_<role>` for a role that passed over its
 * chosen model.
 */
export interface BackgroundNotice {
  id: string;
  chat_id: string;
  step: string;
  code: BackgroundNoticeCode;
  provider?: string;
  model?: string;
  count: number;
  created_at: string;
  updated_at: string;
}
