import { GridValue } from '../sheet-grid';

/**
 * A pivot table computed from its source rows (ADR 0x043, office suite), in Excel's compact layout:
 * one label column with nested rows, subtotals on the parent row, value names over the data and a
 * grand total at the end. Pure, so what the panels and the file writer rely on can be tested alone.
 */

export type Aggregate = 'sum' | 'count' | 'average' | 'max' | 'min' | 'product' | 'countNums' | 'stdDev' | 'stdDevp' | 'var' | 'varp';

export const AGGREGATES: readonly { id: Aggregate; label: string }[] = [
  { id: 'sum', label: 'Sum' },
  { id: 'count', label: 'Count' },
  { id: 'average', label: 'Average' },
  { id: 'max', label: 'Max' },
  { id: 'min', label: 'Min' },
  { id: 'product', label: 'Product' },
  { id: 'countNums', label: 'Count Numbers' },
  { id: 'stdDev', label: 'StdDev' },
  { id: 'stdDevp', label: 'StdDevp' },
  { id: 'var', label: 'Var' },
  { id: 'varp', label: 'Varp' },
];

export interface PivotValueField {
  field: string;
  aggregate: Aggregate;
  /** The caption; defaults to "Sum of Field". */
  name?: string;
  numFmtId?: number;
}

export interface PivotConfig {
  rows: string[];
  columns: string[];
  values: PivotValueField[];
  /** Report filters, shown above the table. */
  filters: string[];
  /** Labels left out, per field, whatever area the field is in (slicers filter this way too). */
  hidden: Record<string, string[]>;
  rowGrandTotals: boolean;
  colGrandTotals: boolean;
}

export const BLANK_LABEL = '(blank)';

/** The label an item goes by, in filters, slicers and the table. */
export function itemLabel(value: GridValue): string {
  if (value === null || value === '') return BLANK_LABEL;
  if (typeof value === 'boolean') return value ? 'TRUE' : 'FALSE';
  return String(value);
}

export function valueCaption(value: PivotValueField): string {
  if (value.name) return value.name;
  const label = AGGREGATES.find(a => a.id === value.aggregate)?.label ?? 'Sum';
  return `${label} of ${value.field}`;
}

/** Numbers in order, then text without regard to case, then blanks; as Excel sorts a pivot's items. */
export function compareLabels(a: string, b: string): number {
  if (a === b) return 0;
  if (a === BLANK_LABEL) return 1;
  if (b === BLANK_LABEL) return -1;
  const x = Number(a);
  const y = Number(b);
  const xNum = a.trim() !== '' && !Number.isNaN(x);
  const yNum = b.trim() !== '' && !Number.isNaN(y);
  if (xNum && yNum) return x - y;
  if (xNum) return -1;
  if (yNum) return 1;
  return a.localeCompare(b, undefined, { sensitivity: 'base' }) || (a < b ? -1 : 1);
}

export function aggregate(kind: Aggregate, values: readonly GridValue[]): number | null {
  const numbers = values.filter((v): v is number => typeof v === 'number');
  switch (kind) {
    case 'count':
      return values.filter(v => v !== null && v !== '').length;
    case 'countNums':
      return numbers.length;
    case 'sum':
      return numbers.reduce((a, b) => a + b, 0);
    case 'product':
      return numbers.length ? numbers.reduce((a, b) => a * b, 1) : 0;
    case 'average':
      return numbers.length ? numbers.reduce((a, b) => a + b, 0) / numbers.length : null;
    case 'max':
      return numbers.length ? Math.max(...numbers) : 0;
    case 'min':
      return numbers.length ? Math.min(...numbers) : 0;
    default: {
      const sample = kind === 'stdDev' || kind === 'var';
      const n = numbers.length;
      if (n < (sample ? 2 : 1)) return null;
      const mean = numbers.reduce((a, b) => a + b, 0) / n;
      const variance = numbers.reduce((a, b) => a + (b - mean) ** 2, 0) / (sample ? n - 1 : n);
      return kind === 'stdDev' || kind === 'stdDevp' ? Math.sqrt(variance) : variance;
    }
  }
}

export interface PivotAxisItem {
  /** Labels from the outermost field in; empty for a grand total. */
  path: string[];
  kind: 'item' | 'subtotal' | 'grand';
  /** Which value field a column shows; 0 on the row axis. */
  valueIndex: number;
}

export interface PivotResult {
  /** The table itself, without the report filters. */
  cells: GridValue[][];
  /** `[field, shown selection]`, a row per report filter, to go above the table with a blank row between. */
  pageCells: GridValue[][];
  firstHeaderRow: number;
  firstDataRow: number;
  firstDataCol: number;
  rowItems: PivotAxisItem[];
  colItems: PivotAxisItem[];
  /** Every label in the source per field, sorted: what the file's item lists hold. */
  fieldItems: Record<string, string[]>;
}

export class PivotSourceError extends Error {}

/** The source's field names, from its header row; refuses what a pivot table cannot use. */
export function sourceFields(table: readonly GridValue[][]): string[] {
  if (table.length < 2) throw new PivotSourceError('A pivot table needs a header row and at least one row of data.');
  const names = table[0].map(v => (v === null ? '' : String(v).trim()));
  if (names.some(n => !n)) throw new PivotSourceError('Every column of the source needs a name in its first row.');
  if (new Set(names.map(n => n.toLowerCase())).size !== names.length) throw new PivotSourceError('The source has two columns with the same name.');
  return names;
}

export function computePivot(config: PivotConfig, table: readonly GridValue[][]): PivotResult {
  const fields = sourceFields(table);
  const column = (name: string) => {
    const i = fields.indexOf(name);
    if (i === -1) throw new PivotSourceError(`The source no longer has a column named "${name}".`);
    return i;
  };
  const data = table.slice(1);
  const rowCols = config.rows.map(column);
  const colCols = config.columns.map(column);
  const valueCols = config.values.map(v => column(v.field));
  const fieldItems: Record<string, string[]> = {};
  fields.forEach((name, i) => {
    fieldItems[name] = [...new Set(data.map(r => itemLabel(r[i] ?? null)))].sort(compareLabels);
  });
  const hiddenBy = Object.entries(config.hidden)
    .filter(([name, labels]) => labels.length && fields.includes(name))
    .map(([name, labels]) => ({ col: fields.indexOf(name), labels: new Set(labels) }));
  const visible = data.filter(r => hiddenBy.every(h => !h.labels.has(itemLabel(r[h.col] ?? null))));

  // Every combination of row prefix and column prefix collects its values once.
  const buckets = new Map<string, GridValue[][]>();
  const rowTree = new Set<string>();
  const colTree = new Set<string>();
  for (const r of visible) {
    const rowPath = rowCols.map(c => itemLabel(r[c] ?? null));
    const colPath = colCols.map(c => itemLabel(r[c] ?? null));
    for (let i = 0; i <= rowPath.length; i++) {
      const rowKey = JSON.stringify(rowPath.slice(0, i));
      rowTree.add(rowKey);
      for (let j = 0; j <= colPath.length; j++) {
        const colKey = JSON.stringify(colPath.slice(0, j));
        colTree.add(colKey);
        const key = `${rowKey}|${colKey}`;
        let bucket = buckets.get(key);
        if (!bucket) {
          bucket = valueCols.map(() => []);
          buckets.set(key, bucket);
        }
        valueCols.forEach((c, v) => bucket[v].push(r[c] ?? null));
      }
    }
  }
  const cell = (rowPath: string[], colPath: string[], v: number): GridValue => {
    const bucket = buckets.get(`${JSON.stringify(rowPath)}|${JSON.stringify(colPath)}`);
    return bucket ? aggregate(config.values[v].aggregate, bucket[v]) : null;
  };

  const rowPaths = walk(rowTree, config.rows.length);
  const colPaths = walk(colTree, config.columns.length);
  const multi = config.values.length > 1;
  const hasRows = config.rows.length > 0;
  const hasCols = config.columns.length > 0;
  const depth = config.columns.length;
  const valueCount = Math.max(1, config.values.length);

  // Columns: outer items carry a subtotal after their children; values multiply each.
  const colSlots: { path: string[]; kind: PivotAxisItem['kind']; v: number }[] = [];
  for (const path of hasCols ? colsWithSubtotals(colPaths, depth) : [{ path: [], kind: 'item' as const }]) {
    for (let v = 0; v < valueCount; v++) colSlots.push({ path: path.path, kind: path.kind, v });
  }
  if (hasCols && config.colGrandTotals) for (let v = 0; v < valueCount; v++) colSlots.push({ path: [], kind: 'grand', v });

  const labelColumns = hasRows ? 1 : 0;
  const width = labelColumns + Math.max(1, colSlots.length);
  const header: GridValue[][] = [];
  const blankRow = (): GridValue[] => Array.from({ length: width }, () => null);
  let firstHeaderRow: number;
  const captions = config.values.map(valueCaption);
  const caption = (v: number) => captions[v] ?? 'Values';
  const slotHeader = (slot: { path: string[]; kind: PivotAxisItem['kind'] }, level: number, previous?: { path: string[]; kind: string }): GridValue => {
    if (slot.kind === 'grand') return level === 0 ? 'Grand Total' : null;
    if (slot.kind === 'subtotal') return level === slot.path.length - 1 ? `${slot.path[level]} Total` : null;
    if (level >= slot.path.length) return null;
    const same = previous && previous.kind === 'item' && JSON.stringify(previous.path.slice(0, level + 1)) === JSON.stringify(slot.path.slice(0, level + 1));
    return same ? null : slot.path[level];
  };
  if (!hasCols) {
    firstHeaderRow = multi ? 0 : 1;
    const line = blankRow();
    if (hasRows) line[0] = 'Row Labels';
    captions.forEach((text, v) => (line[labelColumns + v] = text));
    if (!captions.length) line[labelColumns] = 'Values';
    header.push(line);
  } else {
    firstHeaderRow = 1;
    const top = blankRow();
    if (!multi) top[0] = caption(0);
    top[labelColumns] = 'Column Labels';
    header.push(top);
    for (let level = 0; level < depth; level++) {
      const line = blankRow();
      colSlots.forEach((slot, i) => {
        const prev = i > 0 ? colSlots[i - 1] : undefined;
        if (slot.v === 0) line[labelColumns + i] = slotHeader(slot, level, prev && { path: prev.path, kind: prev.kind });
      });
      if (!multi && level === depth - 1 && hasRows) line[0] = 'Row Labels';
      header.push(line);
    }
    if (multi) {
      const line = blankRow();
      if (hasRows) line[0] = 'Row Labels';
      colSlots.forEach((slot, i) => {
        line[labelColumns + i] = slot.kind === 'grand' ? `Total ${caption(slot.v)}` : caption(slot.v);
      });
      header.push(line);
    }
  }

  const rowItems: PivotAxisItem[] = [];
  const rows: GridValue[][] = [];
  const dataFor = (rowPath: string[]): GridValue[] =>
    colSlots.length ? colSlots.map(slot => (config.values.length ? cell(rowPath, slot.path, slot.v) : null)) : [null];
  if (hasRows) {
    for (const path of rowPaths) {
      rows.push([`${'   '.repeat(path.length - 1)}${path[path.length - 1]}`, ...dataFor(path)]);
      rowItems.push({ path, kind: 'item', valueIndex: 0 });
    }
    if (config.rowGrandTotals) {
      rows.push(['Grand Total', ...dataFor([])]);
      rowItems.push({ path: [], kind: 'grand', valueIndex: 0 });
    }
  } else {
    rows.push(dataFor([]));
    rowItems.push({ path: [], kind: 'item', valueIndex: 0 });
  }
  const firstDataRow = header.length;
  const pageCells: GridValue[][] = config.filters.map(name => {
    const hidden = new Set(config.hidden[name] ?? []);
    const shown = (fieldItems[name] ?? []).filter(l => !hidden.has(l));
    return [name, !hidden.size ? '(All)' : shown.length === 1 ? shown[0] : '(Multiple Items)'];
  });
  return {
    cells: [...header, ...rows],
    pageCells,
    firstHeaderRow,
    firstDataRow,
    firstDataCol: labelColumns,
    rowItems,
    colItems: colSlots.map(s => ({ path: s.path, kind: s.kind, valueIndex: s.v })),
    fieldItems,
  };
}

/** The tree's nodes in display order, each parent before its children; the root is left out. */
function walk(tree: Set<string>, depth: number): string[][] {
  if (!depth) return [];
  return [...tree]
    .map(key => JSON.parse(key) as string[])
    .filter(path => path.length > 0)
    .sort((a, b) => {
      for (let i = 0; i < Math.min(a.length, b.length); i++) {
        const c = compareLabels(a[i], b[i]);
        if (c) return c;
      }
      return a.length - b.length;
    });
}

function colsWithSubtotals(paths: string[][], depth: number): { path: string[]; kind: PivotAxisItem['kind'] }[] {
  const out: { path: string[]; kind: PivotAxisItem['kind'] }[] = [];
  const emit = (parent: string[]) => {
    const kids = paths.filter(p => p.length === parent.length + 1 && parent.every((l, i) => p[i] === l));
    for (const kid of kids) {
      if (kid.length === depth) out.push({ path: kid, kind: 'item' });
      else {
        emit(kid);
        out.push({ path: kid, kind: 'subtotal' });
      }
    }
  };
  emit([]);
  return out;
}
