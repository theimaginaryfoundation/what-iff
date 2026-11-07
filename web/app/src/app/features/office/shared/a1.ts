/** A1-style references, as the office formats use them, to and from zero-based rows and columns. */

export interface CellRef {
  row: number;
  col: number;
}

export interface RangeRef {
  startRow: number;
  startColumn: number;
  endRow: number;
  endColumn: number;
}

export function columnLetter(col: number): string {
  let n = col + 1;
  let out = '';
  while (n > 0) {
    const rem = (n - 1) % 26;
    out = String.fromCharCode(65 + rem) + out;
    n = Math.floor((n - 1) / 26);
  }
  return out;
}

export function columnIndex(letters: string): number {
  let n = 0;
  for (const ch of letters.toUpperCase()) n = n * 26 + (ch.charCodeAt(0) - 64);
  return n - 1;
}

export function cellA1(row: number, col: number): string {
  return `${columnLetter(col)}${row + 1}`;
}

export function parseCellA1(ref: string): CellRef | null {
  const m = /^\$?([A-Za-z]{1,3})\$?(\d+)$/.exec(ref.trim());
  if (!m) return null;
  return { row: Number(m[2]) - 1, col: columnIndex(m[1]) };
}

/** "A1:C3" or "A1" (a single cell) to a zero-based range; null when it is not a range. */
export function parseRangeA1(ref: string): RangeRef | null {
  const [a, b] = ref.replace(/^.*!/, '').split(':');
  const start = parseCellA1(a);
  if (!start) return null;
  const end = b ? parseCellA1(b) : start;
  if (!end) return null;
  return {
    startRow: Math.min(start.row, end.row),
    startColumn: Math.min(start.col, end.col),
    endRow: Math.max(start.row, end.row),
    endColumn: Math.max(start.col, end.col),
  };
}

export function rangeA1(range: RangeRef): string {
  const start = cellA1(range.startRow, range.startColumn);
  const end = cellA1(range.endRow, range.endColumn);
  return start === end ? start : `${start}:${end}`;
}

/** Splits "A1:B2 D4:E9" (space-separated, as OOXML sqref) into ranges, dropping anything invalid. */
export function parseSqref(sqref: string): RangeRef[] {
  return sqref
    .split(/\s+/)
    .filter(Boolean)
    .map(parseRangeA1)
    .filter((r): r is RangeRef => r !== null);
}
