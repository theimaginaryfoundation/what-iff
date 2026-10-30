import { UserPreferences } from '../models/user.model';

/** What the API sends for an unset id (default_model_id, default_personality_id). */
export const NIL_UUID = '00000000-0000-0000-0000-000000000000';

/**
 * Whether the account has a default model. On an install without a model
 * catalog an account has none until it enables a model on one of its
 * connections, and the API reports that as the nil UUID (ADR 0x027).
 */
export function hasDefaultModel(prefs: Pick<UserPreferences, 'default_model_id'> | null | undefined): boolean {
  const id = prefs?.default_model_id;
  return !!id && id !== NIL_UUID;
}
