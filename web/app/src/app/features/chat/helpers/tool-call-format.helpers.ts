/**
 * Display formatting for tool-call inputs and outputs, shared by the inline tool-call rows, the
 * live in-flight timeline and the context panel's tools tab. Tool payloads are usually JSON
 * strings (sometimes JSON-encoded twice), but can be plain text; everything here degrades to the
 * raw text when parsing fails.
 */

/** Summary lines stay within one row of the tool-call strip. */
const SUMMARY_MAX_CHARS = 96;
/** Argument names that best describe what a call is doing, in preference order. */
const PRIMARY_INPUT_KEYS = ['query', 'q', 'search', 'url', 'path', 'name', 'title', 'topic', 'prompt', 'id'];

/** Parses a tool payload as JSON, unwrapping one level of JSON-in-a-string. Undefined if not JSON. */
export function parseToolPayload(raw: string | null | undefined): unknown {
  const text = raw?.trim();
  if (!text || !/^[[{"]/.test(text)) return undefined;
  try {
    const parsed: unknown = JSON.parse(text);
    if (typeof parsed === 'string') {
      const inner = parsed.trim();
      if (/^[[{]/.test(inner)) {
        try {
          return JSON.parse(inner);
        } catch {
          return parsed;
        }
      }
    }
    return parsed;
  } catch {
    return undefined;
  }
}

/** Pretty-printed JSON when the payload parses, otherwise the trimmed raw text. */
export function formatToolPayload(raw: string | null | undefined): string {
  const parsed = parseToolPayload(raw);
  if (parsed === undefined) return raw?.trim() ?? '';
  if (typeof parsed === 'string') return parsed;
  return JSON.stringify(parsed, null, 2);
}

/** A short "what is this call doing" line from its arguments, e.g. `"fox"` or `limit: 5, tag: work`. */
export function summarizeToolInput(raw: string | null | undefined): string {
  const parsed = parseToolPayload(raw);
  if (parsed === undefined) return clip(raw);
  if (!isRecord(parsed)) return clip(typeof parsed === 'string' ? parsed : JSON.stringify(parsed));

  for (const key of PRIMARY_INPUT_KEYS) {
    const value = parsed[key];
    if (typeof value === 'string' && value.trim()) return clip(`"${value.trim()}"`);
  }
  const scalars = Object.entries(parsed)
    .filter(([, value]) => isScalar(value) && String(value).trim() !== '')
    .slice(0, 2)
    .map(([key, value]) => `${key}: ${String(value).trim()}`);
  return clip(scalars.join(', '));
}

/** A short result label, e.g. `3 results`, `2 memories`, or the first line of a text result. */
export function summarizeToolOutput(raw: string | null | undefined): string {
  const parsed = parseToolPayload(raw);
  // A live preview of long JSON is cut off (ending "…") and no longer parses; say nothing rather
  // than echoing a fragment like `{"results":[{"id":…`. The saved row has the full output.
  if (parsed === undefined && isTruncatedJson(raw)) return '';
  if (parsed === undefined) return clip(raw?.split('\n').find(line => line.trim()));
  if (Array.isArray(parsed)) return countLabel(parsed.length, 'result');
  if (isRecord(parsed)) {
    const listEntry = Object.entries(parsed).find(([, value]) => Array.isArray(value));
    if (listEntry) return countLabel((listEntry[1] as unknown[]).length, listEntry[0]);
    for (const key of ['message', 'summary', 'status', 'result']) {
      const value = parsed[key];
      if (isScalar(value) && String(value).trim()) return clip(String(value));
    }
    return '';
  }
  return clip(String(parsed));
}

/**
 * One-line row summary: what the call did, then what came back, e.g. `"red fox" · 3 results`.
 * A running call has no result yet, so it shows its arguments only.
 */
export function summarizeToolCall(
  call: { tool_input?: string; tool_output?: string; tool_error?: string },
  status: 'running' | 'complete' | 'error',
): string {
  const input = summarizeToolInput(call.tool_input);
  if (status === 'running') return input;
  const result = summarizeToolOutput(status === 'error' ? call.tool_error : call.tool_output);
  return [input, result].filter(Boolean).join(' · ') || 'No summary';
}

/** `recall_memories` → `Recall Memories`. */
export function friendlyToolName(name: string): string {
  return name
    .split(/[_-]/)
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function isTruncatedJson(raw: string | null | undefined): boolean {
  const text = raw?.trim() ?? '';
  return /^[[{]/.test(text) && text.endsWith('…');
}

function countLabel(count: number, noun: string): string {
  const word = noun.replace(/_/g, ' ');
  if (count !== 1) return `${count} ${word.endsWith('s') ? word : `${word}s`}`;
  if (word.endsWith('ies')) return `1 ${word.slice(0, -3)}y`;
  return `1 ${word.endsWith('s') ? word.slice(0, -1) : word}`;
}

function clip(text: string | null | undefined): string {
  const flat = (text ?? '').replace(/\s+/g, ' ').trim();
  return flat.length > SUMMARY_MAX_CHARS ? `${flat.slice(0, SUMMARY_MAX_CHARS - 1)}…` : flat;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isScalar(value: unknown): value is string | number | boolean {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean';
}
