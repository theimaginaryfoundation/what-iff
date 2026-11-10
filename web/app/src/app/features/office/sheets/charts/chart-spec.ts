import { ChartData } from '../../shared/chart-svg';
import { rangeA1, parseRangeA1 } from '../../shared/a1';
import { NS, child, children, elementChildren, num } from '../../shared/ooxml';
import { UniverRange } from '../../xlsx/univer-sheet-types';
import { ExcelThemeColors } from '../../xlsx/xlsx-theme';
import { GridValue } from '../sheet-grid';

/**
 * A spreadsheet chart as the panel edits it: what it draws and where its data lives, read from the
 * chart part's reference formulas. Everything else in the part (styles, colour parts, extensions,
 * the formatting of untouched series) stays in the XML; see xlsx/xlsx-chart-write.ts.
 */

export const C = 'http://schemas.openxmlformats.org/drawingml/2006/chart';
const A = NS.a;

export type ChartKind = 'column' | 'bar' | 'line' | 'area' | 'pie' | 'doughnut' | 'scatter' | 'radar';
export type ChartGrouping = 'standard' | 'stacked' | 'percentStacked';
export type LegendPosition = 'r' | 'l' | 't' | 'b' | 'tr' | 'none';

export interface CellsRef {
  sheet: string;
  range: UniverRange;
}

/** A reference formula as written, and the cells it means; `cells` is null for what we cannot read (another workbook, a name, a union). */
export interface SeriesRef {
  formula: string;
  cells: CellsRef | null;
}

export interface CachedSeries {
  name: string;
  labels: string[];
  values: (number | null)[];
}

export interface ChartSeriesSpec {
  /** The series' position among the original part's `c:ser` elements; null for a series added here. */
  sourceIndex: number | null;
  nameLiteral: string | null;
  nameRef: SeriesRef | null;
  categories: SeriesRef | null;
  values: SeriesRef | null;
  /** "#RRGGBB", as drawn; a theme colour stays a theme colour in the file until this changes. */
  color: string | null;
  /** What the part's caches held, drawn when a reference cannot be read. */
  cached: CachedSeries;
}

export interface ChartAnchor {
  from: { col: number; colOff: number; row: number; rowOff: number };
  to: { col: number; colOff: number; row: number; rowOff: number };
}

export interface ChartSpec {
  /** The picture's drawing id: `chart-<sheetId>-<n>`. */
  id: string;
  sheet: string;
  name: string;
  kind: ChartKind;
  grouping: ChartGrouping;
  title: string;
  legend: LegendPosition;
  categoryAxisTitle: string;
  valueAxisTitle: string;
  series: ChartSeriesSpec[];
  /** False for what we keep but do not edit: combination, 3D and other kinds. Those stay byte-identical. */
  editable: boolean;
  /** Why not editable, or what in it stays as it is. */
  limits: string[];
  /** The chart part in the original package; null for a chart made here. */
  sourcePath: string | null;
  /** Whether hidden rows drop out of the chart (`c:plotVisOnly`). */
  visibleOnly: boolean;
  anchor: ChartAnchor;
}

/** Reads `'My Sheet'!$A$1:$B$2`; null for anything that is not one plain range on a sheet of this workbook. */
export function parseChartRef(formula: string): CellsRef | null {
  const m = /^(?:'((?:[^']|'')+)'|([^'!()[\],\s]+))!(\$?[A-Za-z]{1,3}\$?\d+(?::\$?[A-Za-z]{1,3}\$?\d+)?)$/.exec(formula.trim());
  if (!m) return null;
  const sheet = m[1] !== undefined ? m[1].replace(/''/g, "'") : m[2];
  if (sheet.startsWith('[')) return null;
  const range = parseRangeA1(m[3]);
  return range ? { sheet, range } : null;
}

/** Absolute, and quoted when the name needs it, as Excel writes them. */
export function formatChartRef(ref: CellsRef): string {
  const name = /^[A-Za-z_][A-Za-z0-9_.]*$/.test(ref.sheet) ? ref.sheet : `'${ref.sheet.replace(/'/g, "''")}'`;
  const absolute = rangeA1(ref.range).replace(/([A-Z]+)(\d+)/g, '$$$1$$$2');
  return `${name}!${absolute}`;
}

export function seriesRef(cells: CellsRef): SeriesRef {
  return { formula: formatChartRef(cells), cells };
}

const EDITABLE_KINDS: Record<string, ChartKind | undefined> = {
  barChart: 'column',
  lineChart: 'line',
  areaChart: 'area',
  pieChart: 'pie',
  doughnutChart: 'doughnut',
  scatterChart: 'scatter',
  radarChart: 'radar',
};
const GROUP_NAME =
  /^(area|area3D|line|line3D|stock|radar|scatter|pie|pie3D|doughnut|bar|bar3D|ofPie|surface|surface3D|bubble)Chart$/;

export const NOT_EDITABLE_NOTICE = '3D and combination charts are kept but not editable here';
export const KEPT_FEATURES_NOTICE = 'Trendlines and error bars are kept but not editable here';
export const DRAWN_NOTICE = "Charts are drawn here in a simpler style than Excel's; their formatting is kept in the file";

interface SpecContext {
  id: string;
  sheet: string;
  name: string;
  sourcePath: string | null;
  anchor: ChartAnchor;
  theme: ExcelThemeColors;
}

/** Reads a chart part; null when it holds no chart we can draw. */
export function chartSpecFromXml(doc: Document, ctx: SpecContext): ChartSpec | null {
  const chart = doc.getElementsByTagNameNS(C, 'chart')[0];
  const plotArea = child(chart, C, 'plotArea');
  if (!chart || !plotArea) return null;
  const groups = elementChildren(plotArea).filter(el => el.namespaceURI === C && GROUP_NAME.test(el.localName));
  const group = groups[0];
  if (!group) return null;
  const known = EDITABLE_KINDS[group.localName];
  const limits: string[] = [];
  if (groups.length > 1) limits.push('A combination chart');
  else if (/3D/.test(group.localName)) limits.push('A 3D chart');
  else if (!known) limits.push('This kind of chart');
  const editable = limits.length === 0 && !!known;
  const kind: ChartKind = known ?? (/^pie|ofPie/.test(group.localName) ? 'pie' : /^line/.test(group.localName) ? 'line' : /^area/.test(group.localName) ? 'area' : 'column');
  const barDir = child(group, C, 'barDir')?.getAttribute('val');
  const rawGrouping = child(group, C, 'grouping')?.getAttribute('val');
  const series = children(group, C, 'ser').map((ser, i) => seriesFromXml(ser, i, ctx.theme, kind));
  if (!series.length) return null;
  if (groups.some(g => children(g, C, 'ser').some(s => child(s, C, 'trendline') || child(s, C, 'errBars')))) limits.push('Trendlines and error bars');
  const { category, value } = axisTitles(plotArea);
  const legendPos = child(child(chart, C, 'legend'), C, 'legendPos')?.getAttribute('val') as LegendPosition | undefined;
  return {
    id: ctx.id,
    sheet: ctx.sheet,
    name: ctx.name,
    kind: kind === 'column' && barDir === 'bar' ? 'bar' : kind,
    grouping: rawGrouping === 'stacked' || rawGrouping === 'percentStacked' ? rawGrouping : 'standard',
    title: titleText(chart),
    legend: child(chart, C, 'legend') ? (legendPos ?? 'r') : 'none',
    categoryAxisTitle: category,
    valueAxisTitle: value,
    series,
    editable,
    limits,
    sourcePath: ctx.sourcePath,
    visibleOnly: child(chart, C, 'plotVisOnly')?.getAttribute('val') !== '0',
    anchor: ctx.anchor,
  };
}

function seriesFromXml(ser: Element, index: number, theme: ExcelThemeColors, kind: ChartKind): ChartSeriesSpec {
  const tx = child(ser, C, 'tx');
  const nameRefEl = child(tx, C, 'strRef');
  const cat = child(ser, C, 'cat') ?? child(ser, C, 'xVal');
  const val = child(ser, C, 'val') ?? child(ser, C, 'yVal');
  const literal = child(tx, C, 'v')?.textContent ?? null;
  const labels = cacheStrings(cat);
  const values = cacheNumbers(val);
  return {
    sourceIndex: index,
    nameLiteral: nameRefEl ? null : literal,
    nameRef: refOf(nameRefEl),
    categories: refOf(firstRef(cat)),
    values: refOf(firstRef(val)),
    color: seriesColor(ser, theme, kind),
    cached: {
      name: cacheStrings(tx)[0] ?? literal ?? `Series ${index + 1}`,
      labels: labels.length >= values.length ? labels : values.map((_, i) => labels[i] ?? String(i + 1)),
      values,
    },
  };
}

function firstRef(holder: Element | null): Element | null {
  return child(holder, C, 'strRef') ?? child(holder, C, 'numRef') ?? child(holder, C, 'multiLvlStrRef');
}

function refOf(ref: Element | null): SeriesRef | null {
  const formula = child(ref, C, 'f')?.textContent?.trim();
  if (!formula) return null;
  // Several levels of category cannot be rebuilt from one cell per point; the part keeps them.
  return { formula, cells: ref?.localName === 'multiLvlStrRef' ? null : parseChartRef(formula) };
}

function seriesColor(ser: Element, theme: ExcelThemeColors, kind: ChartKind): string | null {
  const spPr = child(ser, C, 'spPr');
  const line = kind === 'line' || kind === 'scatter' || kind === 'radar' ? child(child(spPr, A, 'ln'), A, 'solidFill') : null;
  const fill = line ?? child(spPr, A, 'solidFill');
  const rgb = child(fill, A, 'srgbClr')?.getAttribute('val');
  if (rgb) return `#${rgb.toUpperCase()}`;
  const scheme = child(fill, A, 'schemeClr')?.getAttribute('val');
  return (scheme && theme.scheme[SCHEME[scheme]]) || null;
}

const SCHEME: Record<string, number> = { dk1: 0, tx1: 0, lt1: 1, bg1: 1, dk2: 2, tx2: 2, lt2: 3, bg2: 3, accent1: 4, accent2: 5, accent3: 6, accent4: 7, accent5: 8, accent6: 9 };

function titleText(holder: Element | null): string {
  const title = child(holder, C, 'title');
  if (!title) return '';
  if (child(holder, C, 'autoTitleDeleted')?.getAttribute('val') === '1' && !title.getElementsByTagNameNS(A, 't').length) return '';
  const rich = Array.from(title.getElementsByTagNameNS(A, 't'))
    .map(el => el.textContent ?? '')
    .join('');
  return (rich || cacheStrings(child(title, C, 'tx'))[0] || '').trim();
}

function axisTitles(plotArea: Element): { category: string; value: string } {
  const axes = elementChildren(plotArea).filter(el => el.namespaceURI === C && /Ax$/.test(el.localName));
  const cat = axes.find(a => a.localName === 'catAx' || a.localName === 'dateAx') ?? axes.find(a => a.localName === 'valAx');
  const val = axes.find(a => a.localName === 'valAx' && a !== cat);
  return { category: titleText(cat ?? null), value: titleText(val ?? null) };
}

function cacheStrings(holder: Element | null): string[] {
  if (!holder) return [];
  const cache =
    holder.getElementsByTagNameNS(C, 'strCache')[0] ??
    holder.getElementsByTagNameNS(C, 'numCache')[0] ??
    holder.getElementsByTagNameNS(C, 'multiLvlStrCache')[0] ??
    child(holder, C, 'strLit') ??
    child(holder, C, 'numLit');
  if (!cache) return [];
  return points(child(cache, C, 'lvl') ?? cache).map(p => p ?? '');
}

function cacheNumbers(holder: Element | null): (number | null)[] {
  if (!holder) return [];
  const cache = holder.getElementsByTagNameNS(C, 'numCache')[0] ?? child(holder, C, 'numLit');
  if (!cache) return [];
  return points(cache).map(p => (p === null || p === '' || !Number.isFinite(Number(p)) ? null : Number(p)));
}

function points(container: Element): (string | null)[] {
  const byIdx = new Map(children(container, C, 'pt').map(p => [num(p.getAttribute('idx')) ?? 0, child(p, C, 'v')?.textContent ?? '']));
  const count = num(child(container, C, 'ptCount')?.getAttribute('val')) ?? (byIdx.size ? Math.max(...byIdx.keys()) + 1 : 0);
  return Array.from({ length: count }, (_, i) => byIdx.get(i) ?? null);
}

// --- live data ---------------------------------------------------------------------------------

export interface GridReader {
  values(sheet: string, range: UniverRange): GridValue[][];
  isRowHidden(sheet: string, row: number): boolean;
}

export interface LiveSeries extends CachedSeries {
  /** Whether the categories are all numbers, so they are written as a number reference. */
  numericLabels: boolean;
}

/** One column or row of cells, in order, without hidden rows when the chart leaves them out. */
function readVector(grid: GridReader, ref: CellsRef, visibleOnly: boolean): GridValue[] {
  const out: GridValue[] = [];
  grid.values(ref.sheet, ref.range).forEach((row, r) => {
    if (visibleOnly && grid.isRowHidden(ref.sheet, ref.range.startRow + r)) return;
    out.push(...row);
  });
  return out;
}

function label(value: GridValue): string {
  return value === null ? '' : String(value);
}

/** What each series draws right now: live cells where they can be read, the part's caches elsewhere. */
export function liveSeries(spec: ChartSpec, grid: GridReader): LiveSeries[] {
  return spec.series.map(series => {
    const name = series.nameRef?.cells ? label(readVector(grid, series.nameRef.cells, false)[0] ?? null) : (series.nameLiteral ?? series.cached.name);
    const rawValues = series.values?.cells ? readVector(grid, series.values.cells, spec.visibleOnly) : null;
    const values = rawValues ? rawValues.map(v => (typeof v === 'number' ? v : null)) : series.cached.values;
    const rawLabels = series.categories?.cells ? readVector(grid, series.categories.cells, spec.visibleOnly) : null;
    const labels = rawLabels ? rawLabels.map(label) : series.cached.labels;
    return {
      name,
      labels: labels.length ? labels : values.map((_, i) => String(i + 1)),
      values,
      numericLabels: !!rawLabels?.length && rawLabels.every(v => typeof v === 'number'),
    };
  });
}

/** Whether what the part caches is what the cells now hold, allowing for how numbers print. */
export function sameAsCached(spec: ChartSpec, live: readonly LiveSeries[]): boolean {
  return spec.series.every((series, i) => {
    const now = live[i];
    if (!now) return false;
    const cached = series.cached;
    return (
      (!series.nameRef?.cells || now.name === cached.name) &&
      (!series.values?.cells || sameNumbers(now.values, cached.values)) &&
      (!series.categories?.cells || sameLabels(now.labels, cached.labels))
    );
  });
}

function sameNumbers(a: readonly (number | null)[], b: readonly (number | null)[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i] || (v !== null && b[i] !== null && Math.abs(v - (b[i] as number)) <= 1e-9 * Math.max(1, Math.abs(v))));
}

function sameLabels(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i] || (v !== '' && b[i] !== '' && Number(v) === Number(b[i])));
}

const DEFAULT_COLORS = ['#4472C4', '#ED7D31', '#A5A5A5', '#FFC000', '#5B9BD5', '#70AD47'];

function themeColor(i: number, theme?: ExcelThemeColors): string {
  return theme?.scheme[4 + (i % 6)] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length];
}

/** What the shared renderer draws for a chart. */
export function chartDataFor(spec: ChartSpec, live: readonly LiveSeries[], theme?: ExcelThemeColors): ChartData {
  const round = spec.kind === 'pie' || spec.kind === 'doughnut';
  let rows = live.map(s => s.values.map(v => v ?? 0));
  const count = Math.max(0, ...rows.map(r => r.length));
  if (spec.grouping === 'percentStacked' && !round) {
    const totals = Array.from({ length: count }, (_, i) => rows.reduce((sum, r) => sum + Math.abs(r[i] ?? 0), 0));
    rows = rows.map(r => r.map((v, i) => (totals[i] ? (v / totals[i]) * 100 : 0)));
  }
  // The renderer stacks bars itself; lines and areas stack by adding up here.
  if (spec.grouping !== 'standard' && (spec.kind === 'line' || spec.kind === 'area')) {
    const running = new Array(count).fill(0);
    rows = rows.map(r => r.map((v, i) => (running[i] += v)));
  }
  const colors = round
    ? (live[0]?.values ?? []).map((_, i) => themeColor(i, theme))
    : spec.series.map((s, i) => s.color ?? themeColor(i, theme));
  const data: ChartData = {
    type: spec.kind === 'column' ? 'bar' : spec.kind,
    series: live.map((s, i) => ({ name: s.name, labels: s.labels, values: rows[i] })),
    colors,
    legend: spec.legend !== 'none',
  };
  if (spec.kind === 'column' || spec.kind === 'bar') data.barDirection = spec.kind === 'bar' ? 'bar' : 'col';
  if (spec.grouping !== 'standard') data.grouping = spec.grouping;
  if (spec.title) data.title = spec.title;
  if (spec.categoryAxisTitle) data.categoryAxisTitle = spec.categoryAxisTitle;
  if (spec.valueAxisTitle) data.valueAxisTitle = spec.valueAxisTitle;
  return data;
}

/** A chart from the selection: the first row and first column are its headers when they look like text. */
export function specFromSelection(id: string, grid: GridReader, sheet: string, range: UniverRange, name: string): ChartSpec | null {
  const cells = grid.values(sheet, range);
  if (!cells.length || !cells[0].length) return null;
  const rows = cells.length;
  const columns = cells[0].length;
  const isText = (v: GridValue | undefined) => typeof v === 'string' && v !== '';
  const headerRow = rows > 1 && cells[0].slice(columns > 1 ? 1 : 0).some(isText);
  const firstData = headerRow ? 1 : 0;
  const hasLabels = columns > 1 && cells.slice(firstData).every(r => typeof r[0] !== 'number' || r[0] === null) && cells.slice(firstData).some(r => isText(r[0]));
  const firstValue = hasLabels ? 1 : 0;
  if (firstValue >= columns || firstData >= rows) return null;
  const at = (startRow: number, endRow: number, column: number): CellsRef => ({
    sheet,
    range: { startRow: range.startRow + startRow, endRow: range.startRow + endRow, startColumn: range.startColumn + column, endColumn: range.startColumn + column },
  });
  const series: ChartSeriesSpec[] = [];
  for (let c = firstValue; c < columns; c++) {
    series.push({
      sourceIndex: null,
      nameLiteral: headerRow ? null : `Series ${c - firstValue + 1}`,
      nameRef: headerRow ? seriesRef(at(0, 0, c)) : null,
      categories: hasLabels ? seriesRef(at(firstData, rows - 1, 0)) : null,
      values: seriesRef(at(firstData, rows - 1, c)),
      color: null,
      cached: { name: '', labels: [], values: [] },
    });
  }
  const column = range.endColumn + 2;
  return {
    id,
    sheet,
    name,
    kind: 'column',
    grouping: 'standard',
    title: '',
    legend: series.length > 1 ? 'b' : 'none',
    categoryAxisTitle: '',
    valueAxisTitle: '',
    series,
    editable: true,
    limits: [],
    sourcePath: null,
    visibleOnly: true,
    anchor: { from: { col: column, colOff: 0, row: range.startRow, rowOff: 0 }, to: { col: column + 7, colOff: 0, row: range.startRow + 15, rowOff: 0 } },
  };
}

/** What a chart's caches hold, in the shape of live data: what it draws until something changes. */
export function cachedSeries(spec: ChartSpec): LiveSeries[] {
  return spec.series.map(s => ({ ...s.cached, numericLabels: false }));
}
