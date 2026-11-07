/**
 * A plain chart renderer shared by the office editors: SVG from series data, for charts the
 * editors show but do not edit (spreadsheet charts kept through the round trip; charts on
 * slides). Bars, columns, lines, areas, pies, doughnuts, scatter and radar, with axes, grid,
 * title and legend. Styling is deliberately simple and neutral.
 */
export interface ChartSeriesData {
  name: string;
  labels: string[];
  values: number[];
}

export interface ChartData {
  type: 'bar' | 'line' | 'pie' | 'doughnut' | 'area' | 'scatter' | 'radar' | 'bubble';
  barDirection?: 'bar' | 'col';
  series: ChartSeriesData[];
  colors: string[];
  holeSize?: number;
  title?: string;
  grouping?: string;
}

export interface ChartSvgOptions {
  width: number;
  height: number;
  background?: string;
  textColor?: string;
  fontFamily?: string;
}

const DEFAULT_COLORS = ['#4472C4', '#ED7D31', '#A5A5A5', '#FFC000', '#5B9BD5', '#70AD47', '#264478', '#9E480E'];

const esc = (s: string) => s.replace(/[&<>"]/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[ch] as string);
const f = (n: number) => (Math.round(n * 100) / 100).toString();

export function chartSvg(chart: ChartData, opts: ChartSvgOptions): string {
  const { width, height } = opts;
  const text = opts.textColor ?? '#404040';
  const font = opts.fontFamily ?? 'Calibri, Carlito, Arial, sans-serif';
  const colors = chart.colors.length ? chart.colors : DEFAULT_COLORS;
  const color = (i: number) => colors[i % colors.length];
  const parts: string[] = [];
  if (opts.background) parts.push(`<rect width="${f(width)}" height="${f(height)}" fill="${opts.background}"/>`);
  const titleH = chart.title ? 22 : 0;
  if (chart.title)
    parts.push(
      `<text x="${f(width / 2)}" y="16" text-anchor="middle" font-size="13" font-weight="600" fill="${text}">${esc(chart.title)}</text>`,
    );
  const showLegend = chart.series.length > 1 || chart.type === 'pie' || chart.type === 'doughnut';
  const legendH = showLegend ? 18 : 0;
  const plot = { x: 36, y: titleH + 8, w: width - 44, h: height - titleH - legendH - 30 };
  if (chart.type === 'pie' || chart.type === 'doughnut') parts.push(...pie(chart, plot, color, text));
  else if (chart.type === 'radar') parts.push(...radar(chart, plot, color, text));
  else parts.push(...cartesian(chart, plot, color, text));
  if (showLegend) {
    const items =
      chart.type === 'pie' || chart.type === 'doughnut'
        ? (chart.series[0]?.labels ?? []).map((l, i) => ({ label: l, color: color(i) }))
        : chart.series.map((s, i) => ({ label: s.name, color: color(i) }));
    const step = Math.min(120, Math.max(60, width / Math.max(1, items.length)));
    const startX = Math.max(4, (width - step * items.length) / 2);
    items.forEach((item, i) => {
      const x = startX + i * step;
      const y = height - 10;
      parts.push(
        `<rect x="${f(x)}" y="${f(y - 8)}" width="9" height="9" fill="${item.color}"/><text x="${f(x + 13)}" y="${f(y)}" font-size="10" fill="${text}">${esc(item.label.slice(0, 18))}</text>`,
      );
    });
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${f(width)}" height="${f(height)}" viewBox="0 0 ${f(width)} ${f(height)}" font-family="${esc(font)}">${parts.join('')}</svg>`;
}

/** The chart as a data: URL, for an <img> or an image drawing. */
export function chartSvgDataUrl(chart: ChartData, opts: ChartSvgOptions): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(chartSvg(chart, opts))}`;
}

interface Plot {
  x: number;
  y: number;
  w: number;
  h: number;
}

function niceMax(max: number): number {
  if (max <= 0) return 1;
  const exp = Math.pow(10, Math.floor(Math.log10(max)));
  const n = max / exp;
  const nice = n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10;
  return nice * exp;
}

function cartesian(chart: ChartData, plot: Plot, color: (i: number) => string, text: string): string[] {
  const out: string[] = [];
  const series = chart.series;
  const n = Math.max(1, ...series.map(s => s.values.length));
  const horizontal = chart.type === 'bar' && chart.barDirection === 'bar';
  const stacked = chart.grouping === 'stacked' || chart.grouping === 'percentStacked';
  const totals = Array.from({ length: n }, (_, i) => series.reduce((a, s) => a + Math.max(0, s.values[i] ?? 0), 0));
  const rawMax = stacked ? Math.max(1, ...totals) : Math.max(1, ...series.flatMap(s => s.values.map(v => Math.max(0, v))));
  const max = niceMax(rawMax);
  const ticks = 4;
  // Axis labels and grid.
  for (let t = 0; t <= ticks; t++) {
    const v = (max * t) / ticks;
    if (horizontal) {
      const x = plot.x + (plot.w * t) / ticks;
      out.push(`<line x1="${f(x)}" y1="${f(plot.y)}" x2="${f(x)}" y2="${f(plot.y + plot.h)}" stroke="#D9D9D9" stroke-width="1"/>`);
      out.push(`<text x="${f(x)}" y="${f(plot.y + plot.h + 12)}" text-anchor="middle" font-size="9" fill="${text}">${esc(fmt(v))}</text>`);
    } else {
      const y = plot.y + plot.h - (plot.h * t) / ticks;
      out.push(`<line x1="${f(plot.x)}" y1="${f(y)}" x2="${f(plot.x + plot.w)}" y2="${f(y)}" stroke="#D9D9D9" stroke-width="1"/>`);
      out.push(`<text x="${f(plot.x - 4)}" y="${f(y + 3)}" text-anchor="end" font-size="9" fill="${text}">${esc(fmt(v))}</text>`);
    }
  }
  const labels = series[0]?.labels ?? [];
  if (chart.type === 'bar') {
    const groupSize = (horizontal ? plot.h : plot.w) / n;
    const barSize = (groupSize * 0.7) / (stacked ? 1 : Math.max(1, series.length));
    const offsets = new Array(n).fill(0);
    series.forEach((s, si) => {
      s.values.forEach((v, i) => {
        const len = (Math.max(0, v) / max) * (horizontal ? plot.w : plot.h);
        const base = offsets[i];
        if (horizontal) {
          const y = plot.y + i * groupSize + groupSize * 0.15 + (stacked ? 0 : si * barSize);
          out.push(`<rect x="${f(plot.x + base)}" y="${f(y)}" width="${f(len)}" height="${f(barSize)}" fill="${color(si)}"/>`);
        } else {
          const x = plot.x + i * groupSize + groupSize * 0.15 + (stacked ? 0 : si * barSize);
          out.push(
            `<rect x="${f(x)}" y="${f(plot.y + plot.h - base - len)}" width="${f(barSize)}" height="${f(len)}" fill="${color(si)}"/>`,
          );
        }
        if (stacked) offsets[i] += len;
      });
    });
    labels.forEach((label, i) => {
      if (horizontal)
        out.push(
          `<text x="${f(plot.x - 4)}" y="${f(plot.y + i * groupSize + groupSize / 2 + 3)}" text-anchor="end" font-size="9" fill="${text}">${esc(String(label).slice(0, 10))}</text>`,
        );
      else
        out.push(
          `<text x="${f(plot.x + i * groupSize + groupSize / 2)}" y="${f(plot.y + plot.h + 12)}" text-anchor="middle" font-size="9" fill="${text}">${esc(String(label).slice(0, 12))}</text>`,
        );
    });
  } else if (chart.type === 'scatter' || chart.type === 'bubble') {
    const xs = series[0]?.labels.map(Number) ?? [];
    const xMax = niceMax(Math.max(1, ...xs.filter(Number.isFinite)));
    series.forEach((s, si) => {
      s.values.forEach((v, i) => {
        const x = plot.x + ((Number(s.labels[i]) || 0) / xMax) * plot.w;
        const y = plot.y + plot.h - (Math.max(0, v) / max) * plot.h;
        out.push(`<circle cx="${f(x)}" cy="${f(y)}" r="3" fill="${color(si)}"/>`);
      });
    });
  } else {
    // line and area
    series.forEach((s, si) => {
      const pts = s.values.map(
        (v, i) =>
          [
            plot.x + (s.values.length > 1 ? (i / (s.values.length - 1)) * plot.w : plot.w / 2),
            plot.y + plot.h - (Math.max(0, v) / max) * plot.h,
          ] as const,
      );
      if (!pts.length) return;
      const path = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${f(x)},${f(y)}`).join('');
      if (chart.type === 'area')
        out.push(
          `<path d="${path}L${f(pts[pts.length - 1][0])},${f(plot.y + plot.h)}L${f(pts[0][0])},${f(plot.y + plot.h)}Z" fill="${color(si)}" fill-opacity="0.35"/>`,
        );
      out.push(`<path d="${path}" fill="none" stroke="${color(si)}" stroke-width="2"/>`);
      pts.forEach(([x, y]) => out.push(`<circle cx="${f(x)}" cy="${f(y)}" r="2.5" fill="${color(si)}"/>`));
    });
    labels.forEach((label, i) => {
      const x = plot.x + (labels.length > 1 ? (i / (labels.length - 1)) * plot.w : plot.w / 2);
      out.push(
        `<text x="${f(x)}" y="${f(plot.y + plot.h + 12)}" text-anchor="middle" font-size="9" fill="${text}">${esc(String(label).slice(0, 12))}</text>`,
      );
    });
  }
  out.push(
    `<line x1="${f(plot.x)}" y1="${f(plot.y)}" x2="${f(plot.x)}" y2="${f(plot.y + plot.h)}" stroke="#808080"/><line x1="${f(plot.x)}" y1="${f(plot.y + plot.h)}" x2="${f(plot.x + plot.w)}" y2="${f(plot.y + plot.h)}" stroke="#808080"/>`,
  );
  return out;
}

function pie(chart: ChartData, plot: Plot, color: (i: number) => string, text: string): string[] {
  const out: string[] = [];
  const values = chart.series[0]?.values ?? [];
  const total = values.reduce((a, b) => a + Math.max(0, b), 0) || 1;
  const cx = plot.x + plot.w / 2;
  const cy = plot.y + plot.h / 2;
  const radius = Math.max(4, Math.min(plot.w, plot.h) / 2 - 4);
  const inner = chart.type === 'doughnut' ? radius * ((chart.holeSize ?? 50) / 100) : 0;
  let angle = -Math.PI / 2;
  values.forEach((v, i) => {
    const sweep = (Math.max(0, v) / total) * Math.PI * 2;
    const a0 = angle;
    const a1 = angle + sweep;
    angle = a1;
    const large = sweep > Math.PI ? 1 : 0;
    const p = (a: number, rr: number) => `${f(cx + rr * Math.cos(a))},${f(cy + rr * Math.sin(a))}`;
    const d = inner
      ? `M${p(a0, radius)}A${f(radius)},${f(radius)} 0 ${large},1 ${p(a1, radius)}L${p(a1, inner)}A${f(inner)},${f(inner)} 0 ${large},0 ${p(a0, inner)}Z`
      : `M${f(cx)},${f(cy)}L${p(a0, radius)}A${f(radius)},${f(radius)} 0 ${large},1 ${p(a1, radius)}Z`;
    out.push(`<path d="${d}" fill="${color(i)}" stroke="#FFFFFF" stroke-width="1"/>`);
    if (sweep > 0.3) {
      const mid = (a0 + a1) / 2;
      const rr = inner ? (radius + inner) / 2 : radius * 0.65;
      out.push(
        `<text x="${f(cx + rr * Math.cos(mid))}" y="${f(cy + rr * Math.sin(mid) + 3)}" text-anchor="middle" font-size="9" fill="#FFFFFF">${esc(fmt((v / total) * 100))}%</text>`,
      );
    }
  });
  void text;
  return out;
}

function radar(chart: ChartData, plot: Plot, color: (i: number) => string, text: string): string[] {
  const out: string[] = [];
  const labels = chart.series[0]?.labels ?? [];
  const n = Math.max(3, labels.length);
  const cx = plot.x + plot.w / 2;
  const cy = plot.y + plot.h / 2;
  const radius = Math.max(4, Math.min(plot.w, plot.h) / 2 - 12);
  const max = niceMax(Math.max(1, ...chart.series.flatMap(s => s.values)));
  for (let ring = 1; ring <= 4; ring++) {
    const r = (radius * ring) / 4;
    const pts = Array.from({ length: n }, (_, i) => {
      const a = -Math.PI / 2 + (i * 2 * Math.PI) / n;
      return `${f(cx + r * Math.cos(a))},${f(cy + r * Math.sin(a))}`;
    });
    out.push(`<polygon points="${pts.join(' ')}" fill="none" stroke="#D9D9D9"/>`);
  }
  labels.forEach((label, i) => {
    const a = -Math.PI / 2 + (i * 2 * Math.PI) / n;
    out.push(
      `<text x="${f(cx + (radius + 8) * Math.cos(a))}" y="${f(cy + (radius + 8) * Math.sin(a) + 3)}" text-anchor="middle" font-size="9" fill="${text}">${esc(String(label).slice(0, 10))}</text>`,
    );
  });
  chart.series.forEach((s, si) => {
    const pts = s.values.map((v, i) => {
      const a = -Math.PI / 2 + (i * 2 * Math.PI) / n;
      const r = (Math.max(0, v) / max) * radius;
      return `${f(cx + r * Math.cos(a))},${f(cy + r * Math.sin(a))}`;
    });
    out.push(`<polygon points="${pts.join(' ')}" fill="${color(si)}" fill-opacity="0.25" stroke="${color(si)}" stroke-width="2"/>`);
  });
  return out;
}

function fmt(v: number): string {
  if (Math.abs(v) >= 1_000_000) return `${f(v / 1_000_000)}M`;
  if (Math.abs(v) >= 1_000) return `${f(v / 1_000)}k`;
  return f(v);
}
