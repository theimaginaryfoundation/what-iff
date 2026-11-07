import { ChartData, ChartSeriesData } from '../shared/chart-svg';
import { NS, child, children, elementChildren, num } from '../shared/ooxml';
import { ExcelThemeColors } from './xlsx-theme';

/** DrawingML chart parts (xl/charts/chartN.xml) read down to the data their caches hold. */
const C = 'http://schemas.openxmlformats.org/drawingml/2006/chart';
const A = NS.a;

const KINDS: Record<string, ChartData['type']> = {
  barChart: 'bar',
  bar3DChart: 'bar',
  lineChart: 'line',
  line3DChart: 'line',
  pieChart: 'pie',
  pie3DChart: 'pie',
  ofPieChart: 'pie',
  doughnutChart: 'doughnut',
  areaChart: 'area',
  area3DChart: 'area',
  scatterChart: 'scatter',
  bubbleChart: 'bubble',
  radarChart: 'radar',
};

/** Reads one chart part; null when it holds no chart kind this renderer draws. */
export function parseChartXml(doc: Document, theme: ExcelThemeColors): ChartData | null {
  const chart = doc.getElementsByTagNameNS(C, 'chart')[0];
  if (!chart) return null;
  const plotArea = child(chart, C, 'plotArea');
  const group = elementChildren(plotArea).find(el => el.namespaceURI === C && KINDS[el.localName]);
  if (!group) return null;
  const type = KINDS[group.localName];
  const series: ChartSeriesData[] = [];
  const colors: string[] = [];
  for (const ser of children(group, C, 'ser')) {
    const name = textOf(child(child(ser, C, 'tx'), C, 'strRef')) || cacheStrings(child(ser, C, 'tx'))[0] || `Series ${series.length + 1}`;
    const cat = child(ser, C, 'cat') ?? child(ser, C, 'xVal');
    const val = child(ser, C, 'val') ?? child(ser, C, 'yVal');
    const values = cacheNumbers(val);
    let labels = cacheStrings(cat);
    if (!labels.length) labels = cacheNumbers(cat).map(String);
    if (labels.length < values.length) labels = values.map((_, i) => labels[i] ?? String(i + 1));
    series.push({ name, labels, values });
    const fill = child(child(ser, C, 'spPr'), A, 'solidFill');
    const rgb = child(fill, A, 'srgbClr')?.getAttribute('val');
    const scheme = child(fill, A, 'schemeClr')?.getAttribute('val');
    colors.push(rgb ? `#${rgb.toUpperCase()}` : (schemeColor(scheme, theme) ?? defaultColor(series.length - 1, theme)));
    // Pie slices: per-point colours.
    if ((type === 'pie' || type === 'doughnut') && series.length === 1) {
      const dPts = children(ser, C, 'dPt');
      if (dPts.length) {
        colors.length = 0;
        values.forEach((_, i) => {
          const dPt = dPts.find(d => num(child(d, C, 'idx')?.getAttribute('val')) === i);
          const pFill = child(child(dPt, C, 'spPr'), A, 'solidFill');
          const pRgb = child(pFill, A, 'srgbClr')?.getAttribute('val');
          colors.push(
            pRgb
              ? `#${pRgb.toUpperCase()}`
              : (schemeColor(child(pFill, A, 'schemeClr')?.getAttribute('val'), theme) ?? defaultColor(i, theme)),
          );
        });
      } else {
        colors.length = 0;
        values.forEach((_, i) => colors.push(defaultColor(i, theme)));
      }
    }
  }
  if (!series.length) return null;
  const out: ChartData = { type, series, colors };
  const barDir = child(group, C, 'barDir')?.getAttribute('val');
  if (type === 'bar') out.barDirection = barDir === 'bar' ? 'bar' : 'col';
  const grouping = child(group, C, 'grouping')?.getAttribute('val');
  if (grouping) out.grouping = grouping;
  const hole = num(child(group, C, 'holeSize')?.getAttribute('val'));
  if (hole !== undefined) out.holeSize = hole;
  const title = child(chart, C, 'title');
  const autoDeleted = child(chart, C, 'autoTitleDeleted')?.getAttribute('val') === '1';
  if (title && !autoDeleted) {
    const t = Array.from(title.getElementsByTagNameNS(A, 't'))
      .map(el => el.textContent ?? '')
      .join('');
    if (t.trim()) out.title = t.trim();
  }
  return out;
}

function textOf(ref: Element | null): string {
  return cacheStrings(ref)[0] ?? '';
}

/** The strings of a strRef/strCache (or multiLvlStrRef's deepest level), in point order. */
function cacheStrings(holder: Element | null): string[] {
  if (!holder) return [];
  const caches = [
    ...Array.from(holder.getElementsByTagNameNS(C, 'strCache')),
    ...Array.from(holder.getElementsByTagNameNS(C, 'multiLvlStrCache')),
  ];
  const cache = caches[0];
  if (!cache) {
    // A literal list.
    const lit = child(holder, C, 'strLit');
    return lit ? pointsOf(lit) : [];
  }
  const lvl = child(cache, C, 'lvl');
  return pointsOf(lvl ?? cache);
}

function cacheNumbers(holder: Element | null): number[] {
  if (!holder) return [];
  const cache = holder.getElementsByTagNameNS(C, 'numCache')[0] ?? child(holder, C, 'numLit');
  if (!cache) return [];
  return pointsOf(cache).map(v => Number(v) || 0);
}

function pointsOf(container: Element): string[] {
  const pts = children(container, C, 'pt').sort((a, b) => (num(a.getAttribute('idx')) ?? 0) - (num(b.getAttribute('idx')) ?? 0));
  const count = num(child(container, C, 'ptCount')?.getAttribute('val'));
  const out: string[] = [];
  const byIdx = new Map(pts.map(p => [num(p.getAttribute('idx')) ?? 0, child(p, C, 'v')?.textContent ?? '']));
  const n = count ?? (pts.length ? Math.max(...byIdx.keys()) + 1 : 0);
  for (let i = 0; i < n; i++) out.push(byIdx.get(i) ?? '');
  return out;
}

const SCHEME: Record<string, number> = {
  dk1: 0,
  tx1: 0,
  lt1: 1,
  bg1: 1,
  dk2: 2,
  tx2: 2,
  lt2: 3,
  bg2: 3,
  accent1: 4,
  accent2: 5,
  accent3: 6,
  accent4: 7,
  accent5: 8,
  accent6: 9,
  hlink: 10,
  folHlink: 11,
};

function schemeColor(name: string | null | undefined, theme: ExcelThemeColors): string | null {
  if (!name) return null;
  const i = SCHEME[name];
  return i === undefined ? null : (theme.scheme[i] ?? null);
}

function defaultColor(index: number, theme: ExcelThemeColors): string {
  return theme.scheme[4 + (index % 6)] ?? '#4472C4';
}
