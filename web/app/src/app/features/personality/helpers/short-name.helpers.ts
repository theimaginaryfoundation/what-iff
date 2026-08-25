/**
 * Abbreviates a multi-word name by keeping the first word and
 * collapsing each subsequent word to its initial followed by a period.
 *
 * Single words and empty strings are returned unchanged. Examples:
 *   "Filbolt"             -> "Filbolt"
 *   "Filbolt Pottsworth"  -> "Filbolt P."
 *   "Hugo The Magnificent" -> "Hugo T. M."
 */
export function shortName(name: string): string {
  if (!name) return '';
  const trimmed = name.trim();
  if (!trimmed) return '';
  const parts = trimmed.split(/\s+/);
  if (parts.length <= 1) return parts[0];
  const head = parts[0];
  const tail = parts.slice(1).map(word => `${[...word][0]}.`).join(' ');
  return `${head} ${tail}`;
}
