import { Personality, PersonalityExpression } from '../../../core/models/personality.model';

/**
 * Preferred expression keys to use as a personality's cover when no
 * explicit cover image is set. Order is significant — the first key
 * in this list that has an assigned image wins.
 */
const PREFERRED_COVER_KEYS = ['default', 'neutral', 'happy', 'content'] as const;

type ImageUrlResolver = (id: string, size: 'thumbnail' | 'full') => string;

/**
 * Resolves the URL to use as a personality's portrait. Falls back through:
 *   1. personality.cover_image_url (explicit)
 *   2. an expression image whose key matches the preferred fallback list
 *   3. the first expression image in the list (alphabetical by key)
 *   4. null (caller should render initials avatar)
 */
export function personalityCoverUrl(
  personality: Pick<Personality, 'cover_image_id' | 'cover_image_url'> | null | undefined,
  expressions: readonly PersonalityExpression[] | null | undefined,
  getImageUrl?: ImageUrlResolver,
): string | null {
  if (personality?.cover_image_id && getImageUrl) {
    return getImageUrl(personality.cover_image_id, 'thumbnail');
  }

  if (personality?.cover_image_url) {
    return personality.cover_image_url;
  }

  const list = (expressions ?? []).filter(expr => !!expr.image_id || !!expr.image_url);
  if (list.length === 0) return null;

  for (const key of PREFERRED_COVER_KEYS) {
    const match = list.find(expr => expr.expression_key === key);
    const url = expressionImageUrl(match, getImageUrl);
    if (url) {
      return url;
    }
  }

  const sorted = [...list].sort((a, b) => a.expression_key.localeCompare(b.expression_key));
  return expressionImageUrl(sorted[0], getImageUrl);
}

function expressionImageUrl(
  expression: PersonalityExpression | null | undefined,
  getImageUrl?: ImageUrlResolver,
): string | null {
  if (!expression) return null;
  if (expression.image_id && getImageUrl) {
    return getImageUrl(expression.image_id, 'thumbnail');
  }
  return expression.image_url ?? null;
}
