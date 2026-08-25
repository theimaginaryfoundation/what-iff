import { PersonalityExpression } from '../../../core/models/personality.model';

/**
 * Merge representation of an expression slot in the manager UI.
 */
export interface MergedExpression {
  /** URL-safe expression key. */
  expressionKey: string;
  /** Optional display label and when-to-use hint for picker / continuity (server field `label`). */
  label: string | null;
  /** Image gallery file attachment ID assigned to this slot, or null when unset. */
  imageId: string | null;
  /** Read-only authenticated URL for the assigned image, or null. */
  imageUrl: string | null;
  /** True if the manifest of starter suggestions includes this key. */
  isSuggested: boolean;
  /** True if a server-side row exists for this key (i.e. it has a label or image). */
  isAssigned: boolean;
  /** Sort order weight for stable rendering. */
  sortKey: string;
}

/**
 * Canonical 3×3 expression grid keys; aligned with `agent.ExpressionGridKeys` on the server.
 * Used to decide Generate vs Regenerate and must stay in sync with default-grid generation.
 */
export const DEFAULT_EXPRESSION_SUGGESTIONS = [
  'happy',
  'content',
  'sad',
  'angry',
  'surprised',
  'confused',
  'tired',
  'in-love',
  'thinking',
] as const;

/**
 * Maps persisted personality expression rows to grid slot view models, sorted by key.
 * The expressions manager renders only this list (no client-side default placeholders).
 */
export function slotsFromPersistedExpressions(
  expressions: readonly PersonalityExpression[],
): MergedExpression[] {
  return [...expressions]
    .sort((a, b) => a.expression_key.localeCompare(b.expression_key))
    .map(e => ({
      expressionKey: e.expression_key,
      label: e.label,
      imageId: e.image_id,
      imageUrl: e.image_url,
      isSuggested: false,
      isAssigned: true,
      sortKey: e.expression_key,
    }));
}

/**
 * True when every {@link DEFAULT_EXPRESSION_SUGGESTIONS} key has a persisted row with an image.
 * Mirrors `completeDefaultExpressionGrid` in the personality API.
 */
export function isDefaultExpressionGridComplete(
  expressions: readonly PersonalityExpression[],
  gridKeys: readonly string[] = [...DEFAULT_EXPRESSION_SUGGESTIONS],
): boolean {
  const byKey = new Map(expressions.map(e => [e.expression_key, e]));
  for (const key of gridKeys) {
    const row = byKey.get(key);
    if (!row || !row.image_id?.trim()) {
      return false;
    }
  }
  return true;
}

/**
 * Merges a curated suggestion list with the user's existing expression rows
 * so the UI can render every suggestion alongside any user-created custom
 * keys. Server rows that don't appear in the suggestion list are appended.
 */
export function mergeManifestWithAssignments(
  manifestKeys: readonly string[],
  expressions: readonly PersonalityExpression[],
): MergedExpression[] {
  const byKey = new Map<string, PersonalityExpression>();
  for (const expression of expressions) {
    byKey.set(expression.expression_key, expression);
  }

  const merged: MergedExpression[] = [];
  const seen = new Set<string>();

  for (const key of manifestKeys) {
    seen.add(key);
    const existing = byKey.get(key);
    merged.push({
      expressionKey: key,
      label: existing?.label ?? null,
      imageId: existing?.image_id ?? null,
      imageUrl: existing?.image_url ?? null,
      isSuggested: true,
      isAssigned: !!existing,
      sortKey: key,
    });
  }

  const extras = expressions
    .filter(expression => !seen.has(expression.expression_key))
    .sort((a, b) => a.expression_key.localeCompare(b.expression_key));

  for (const expression of extras) {
    merged.push({
      expressionKey: expression.expression_key,
      label: expression.label,
      imageId: expression.image_id,
      imageUrl: expression.image_url,
      isSuggested: false,
      isAssigned: true,
      sortKey: expression.expression_key,
    });
  }

  return merged;
}

/**
 * Returns the merged slots that have no image assigned. Useful for the empty-state
 * "n expressions to assign" indicator.
 */
export function missingExpressions(merged: readonly MergedExpression[]): MergedExpression[] {
  return merged.filter(slot => !slot.imageId);
}

/** Validates a candidate user-created expression key against the backend regex. */
export function isValidExpressionKey(key: string): boolean {
  return /^[a-z0-9][a-z0-9_-]{0,63}$/.test(key);
}
