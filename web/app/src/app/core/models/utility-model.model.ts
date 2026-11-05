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
