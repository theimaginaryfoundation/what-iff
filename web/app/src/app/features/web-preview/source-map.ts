/**
 * The original line of a place in a compiled module, read from the module's inline source map (as
 * esbuild writes it, ADR 0x050 §4): where a script made an element, from the call site the bridge
 * recorded in the compiled code, is then a line of the file the user wrote (§5).
 */

const BASE64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
const DIGIT = new Map([...BASE64].map((c, i) => [c, i]));

/** The source map inlined at the end of compiled code, or null. */
export function inlineSourceMap(code: string): { mappings: string } | null {
  const m = /\/\/# sourceMappingURL=data:application\/json;(?:charset=[^;,]+;)?base64,([A-Za-z0-9+/=]+)\s*$/.exec(code);
  if (!m) return null;
  try {
    const json = new TextDecoder().decode(Uint8Array.from(atob(m[1]), c => c.charCodeAt(0)));
    const map = JSON.parse(json) as { mappings?: unknown };
    return typeof map.mappings === 'string' ? { mappings: map.mappings } : null;
  } catch {
    return null;
  }
}

/**
 * The original 1-based line of the compiled 1-based `line` and `column`: the mapping on that line at
 * or before the column (else the line's first). Null when the line has none.
 */
export function originalLine(mappings: string, line: number, column: number): number | null {
  // Fields are relative to the previous segment's: the column within a line, the rest across lines.
  let sourceLine = 0;
  const lines = mappings.split(';');
  for (let l = 0; l < lines.length && l < line; l++) {
    let generatedColumn = 0;
    let best: number | null = null;
    let first: number | null = null;
    for (const segment of lines[l].split(',')) {
      if (!segment) continue;
      const fields = decodeVlq(segment);
      if (!fields) return null;
      generatedColumn += fields[0];
      if (fields.length < 4) continue;
      sourceLine += fields[2];
      if (l === line - 1) {
        first ??= sourceLine;
        if (generatedColumn <= column - 1) best = sourceLine;
      }
    }
    if (l === line - 1) {
      const found = best ?? first;
      return found === null ? null : found + 1;
    }
  }
  return null;
}

function decodeVlq(segment: string): number[] | null {
  const out: number[] = [];
  let value = 0;
  let shift = 0;
  for (const c of segment) {
    const digit = DIGIT.get(c);
    if (digit === undefined) return null;
    value += (digit & 31) << shift;
    if (digit & 32) {
      shift += 5;
      continue;
    }
    out.push(value & 1 ? -(value >>> 1) : value >>> 1);
    value = 0;
    shift = 0;
  }
  return out;
}
