/**
 * Wire format for "attach another thread as context".
 *
 * The composer prepends one line per attached thread to the user's message. The line uses
 * the find_context tool's own vocabulary so the agent can route it straight to
 * `find_context(mode="conversation", target=<uuid>)`:
 *
 *   [Referenced thread "<name>" — read it with find_context mode="conversation" target="<uuid>"]
 *
 * The name is JSON-escaped (quotes, backslashes, newlines) so a line can always be parsed
 * back. {@link parseThreadReferenceBlock} is the strict inverse used to render the block as
 * chips in the user's own message bubble; any text that doesn't match exactly is left alone.
 */

/** A thread named in a reference block. */
export interface ThreadReference {
  id: string;
  name: string;
}

/** Leading reference lines split from the rest of a message. */
export interface ParsedThreadReferenceBlock {
  references: ThreadReference[];
  /** Everything after the block (the whole text when there is no block). */
  body: string;
}

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// One JSON string literal (the escaped name), then the exact tool hint with a UUID target.
const REFERENCE_LINE_PATTERN =
  /^\[Referenced thread ("(?:[^"\\\n]|\\.)*") — read it with find_context mode="conversation" target="([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"\]$/i;

export function isUuid(value: string): boolean {
  return UUID_PATTERN.test(value);
}

/** One reference line (no trailing newline). */
export function formatThreadReferenceLine(thread: ThreadReference): string {
  return `[Referenced thread ${JSON.stringify(thread.name)} — read it with find_context mode="conversation" target="${thread.id}"]`;
}

/**
 * Text block sent ahead of the user's message, one line per thread plus a trailing newline.
 * Threads whose id isn't a UUID are skipped (the agent couldn't resolve them anyway);
 * returns '' when nothing remains.
 */
export function formatThreadReferences(threads: readonly ThreadReference[]): string {
  const lines = threads.filter(thread => isUuid(thread.id)).map(formatThreadReferenceLine);
  return lines.length > 0 ? `${lines.join('\n')}\n` : '';
}

/** Splits leading reference lines off a message. Non-matching text is returned untouched. */
export function parseThreadReferenceBlock(text: string): ParsedThreadReferenceBlock {
  const references: ThreadReference[] = [];
  let offset = 0;
  while (offset < text.length) {
    const newline = text.indexOf('\n', offset);
    // The block always ends with a newline; a last line without one is message text.
    if (newline === -1) break;
    const match = REFERENCE_LINE_PATTERN.exec(text.slice(offset, newline));
    if (!match) break;
    const name = parseJsonString(match[1]);
    if (name === null) break;
    references.push({ id: match[2], name });
    offset = newline + 1;
  }
  return references.length > 0 ? { references, body: text.slice(offset) } : { references, body: text };
}

function parseJsonString(literal: string): string | null {
  try {
    const value: unknown = JSON.parse(literal);
    return typeof value === 'string' ? value : null;
  } catch {
    return null;
  }
}

/** "1 thread added" / "3 threads added" — shared by the composer chips and sidebar select bar. */
export function threadReferenceCountLabel(count: number): string {
  return count === 1 ? '1 thread added' : `${count} threads added`;
}
