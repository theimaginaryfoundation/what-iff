import { REL_TYPE, Relationship, extensionForMime, parseDataUrl, parseXml, resolveTarget } from '../shared/ooxml';
import { ChartSpec, LiveSeries, chartSpecFromXml } from '../sheets/charts/chart-spec';
import { RESOURCE_DRAWINGS, UniverDrawingsForSheet, UniverSheetImage, UniverWorkbook, readResource } from './univer-sheet-types';
import { chartXmlForSave, newChartXml } from './xlsx-chart-write';
import {
  GridPosition,
  appendChartAnchor,
  appendPictureAnchor,
  chartAnchor,
  drawingXml,
  emptyDrawingXml,
  moveAnchor,
  pictureAnchors,
} from './xlsx-drawing-patch';
import { PreservedParts, emptyPreservedSheet } from './xlsx-preserve';
import { ExcelThemeColors } from './xlsx-theme';

const REL_CHART = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart';
const REL_DRAWING_CT = 'application/vnd.openxmlformats-officedocument.drawing+xml';
const CT_CHART = 'application/vnd.openxmlformats-officedocument.drawingml.chart+xml';
/** How far a position may differ from the imported one, in pixels, and still count as not moved. */
const MOVE_TOLERANCE_PX = 1.5;

/** What a save needs to know about the charts and drawings of the open sheet. */
export interface ChartSaveState {
  /** The charts as read from the file. */
  originals: readonly ChartSpec[];
  /** The charts now; a chart missing here has been deleted. */
  current: readonly ChartSpec[];
  /** What a chart draws right now. */
  live(spec: ChartSpec): LiveSeries[];
  /** The drawings as the import placed them, by sheet id. */
  baseline: Record<string, UniverDrawingsForSheet>;
}

/** Every sheet's drawings at import, compared against at save to see what moved. */
export function drawingBaseline(workbook: UniverWorkbook): Record<string, UniverDrawingsForSheet> {
  return structuredClone(readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS) ?? {});
}

function positionOf(image: UniverSheetImage): GridPosition {
  const { from, to } = image.sheetTransform;
  const { left, top, width, height } = image.transform;
  return { from, to, left, top, width, height };
}

function moved(a: UniverSheetImage, b: UniverSheetImage): boolean {
  const near = (x: number, y: number) => Math.abs(x - y) <= MOVE_TOLERANCE_PX;
  const same = (p: GridPosition['from'], q: GridPosition['from']) =>
    p.column === q.column && p.row === q.row && near(p.columnOffset, q.columnOffset) && near(p.rowOffset, q.rowOffset);
  return !(same(a.sheetTransform.from, b.sheetTransform.from) && same(a.sheetTransform.to, b.sheetTransform.to));
}

function relative(fromPart: string, toPart: string): string {
  const fromDir = fromPart.slice(0, fromPart.lastIndexOf('/')).split('/').filter(Boolean);
  const to = toPart.split('/');
  let i = 0;
  while (i < fromDir.length && i < to.length - 1 && fromDir[i] === to[i]) i++;
  return [...Array(fromDir.length - i).fill('..'), ...to.slice(i)].join('/');
}

function freshPath(parts: PreservedParts['parts'], make: (n: number) => string): string {
  let n = 1;
  while (parts[make(n)]) n++;
  return make(n);
}

function nextRelId(rels: Relationship[]): string {
  let n = rels.length + 1;
  while (rels.some(r => r.id === `rId${n}`)) n++;
  return `rId${n}`;
}

/**
 * The preserved parts as this save should write them: chart parts edited or refreshed, new charts
 * added, deleted ones gone, and drawings following what moved. Always works from the file as read,
 * so saving twice is the same as saving once; `preserved` is not changed.
 */
export function applyChartChanges(preserved: PreservedParts, workbook: UniverWorkbook, state: ChartSaveState): PreservedParts {
  const out: PreservedParts = { ...preserved, parts: { ...preserved.parts }, rels: { ...preserved.rels }, bySheet: { ...preserved.bySheet } };
  const encoder = new TextEncoder();
  // A copy, so the bytes belong to this realm (JSZip checks, and a test runner's TextEncoder may differ).
  const encode = (text: string) => new Uint8Array(encoder.encode(text));
  const decoder = new TextDecoder();
  const drawings = readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS) ?? {};
  const putText = (path: string, text: string, contentType: string) => {
    out.parts[path] = { path, bytes: encode(text), contentType };
  };
  for (const sheetId of workbook.sheetOrder) {
    const sheet = workbook.sheets[sheetId];
    if (!sheet) continue;
    const images = drawings[sheetId]?.data ?? {};
    const baseline = state.baseline[sheetId]?.data ?? {};
    const newCharts = state.current.filter(c => c.sheet === sheet.name && !c.sourcePath && images[c.id]);
    const original = out.bySheet[sheet.name]?.drawing;
    if (!original && !newCharts.length) continue;

    const entry = { ...(out.bySheet[sheet.name] ?? emptyPreservedSheet()) };
    const drawing = original
      ? { ...original, charts: [...original.charts] }
      : { path: freshPath(out.parts, n => `xl/drawings/drawingNew${n}.xml`), charts: [], shapes: 0 };
    entry.drawing = drawing;
    out.bySheet[sheet.name] = entry;
    const sourceText = original ? decoder.decode(preserved.parts[original.path].bytes) : emptyDrawingXml();
    const doc = parseXml(sourceText);
    const rels: Relationship[] = [...(out.rels[drawing.path] ?? [])];
    let changed = !original;

    // Charts from the file.
    const kept: typeof drawing.charts = [];
    drawing.charts.forEach((chart, index) => {
      const id = `chart-${sheetId}-${index}`;
      const rel = rels.find(r => !r.external && r.type === REL_CHART && resolveTarget(drawing.path, r.target) === chart.chartPath);
      const anchor = rel ? chartAnchor(doc, rel.id) : null;
      const before = state.originals.find(c => c.id === id);
      const after = state.current.find(c => c.id === id);
      const image = images[id];
      if (baseline[id] && (!image || (before && !after))) {
        anchor?.remove();
        if (rel) rels.splice(rels.indexOf(rel), 1);
        removePartTree(out, chart.chartPath);
        changed = true;
        return;
      }
      kept.push(chart);
      if (anchor && image && baseline[id] && moved(baseline[id], image)) {
        moveAnchor(anchor, positionOf(image));
        changed = true;
      }
      if (before && after) {
        const part = preserved.parts[chart.chartPath];
        const text = decoder.decode(part.bytes);
        const next = chartXmlForSave(text, before, after, state.live(after));
        if (next !== text) out.parts[chart.chartPath] = { ...part, bytes: encode(next) };
      }
    });
    drawing.charts = kept;

    // Pictures from the file, matched by their order in the drawing, and the rest added.
    const pictures = pictureAnchors(doc);
    const claimed = new Set<string>();
    pictures.forEach((anchor, index) => {
      const id = `image-${sheetId}-${index}`;
      claimed.add(id);
      if (!baseline[id]) return;
      const image = images[id];
      if (!image) {
        anchor.remove();
        changed = true;
      } else if (moved(baseline[id], image)) {
        moveAnchor(anchor, positionOf(image));
        changed = true;
      }
    });
    for (const [id, image] of Object.entries(images)) {
      if (id.startsWith('chart-') || id.startsWith('slicer-') || claimed.has(id)) continue;
      const data = image.imageSourceType === 'BASE64' ? parseDataUrl(image.source) : null;
      const extension = data ? extensionForMime(data.mime) : 'bin';
      if (!data || extension === 'bin' || extension === 'svg') continue;
      const mediaPath = freshPath(out.parts, n => `xl/media/imageNew${n}.${extension}`);
      out.parts[mediaPath] = { path: mediaPath, bytes: data.bytes, contentType: data.mime };
      const relId = nextRelId(rels);
      rels.push({ id: relId, type: REL_TYPE.image, target: relative(drawing.path, mediaPath), external: false });
      appendPictureAnchor(doc, positionOf(image), relId, `Picture ${id}`);
      changed = true;
    }

    // Charts made here.
    for (const chart of newCharts) {
      const chartPath = freshPath(out.parts, n => `xl/charts/chartNew${n}.xml`);
      putText(chartPath, newChartXml(chart, state.live(chart)), CT_CHART);
      const relId = nextRelId(rels);
      rels.push({ id: relId, type: REL_CHART, target: relative(drawing.path, chartPath), external: false });
      appendChartAnchor(doc, positionOf(images[chart.id]), relId, chart.name);
      changed = true;
    }

    if (changed) {
      putText(drawing.path, drawingXml(doc), REL_DRAWING_CT);
      if (rels.length) out.rels[drawing.path] = rels;
      else delete out.rels[drawing.path];
    }
  }
  return out;
}

/** Drops a part and what only it pointed at (a chart's style and colour parts, its embedded workbook). */
function removePartTree(out: PreservedParts, path: string): void {
  const rels = out.rels[path] ?? [];
  delete out.parts[path];
  delete out.rels[path];
  for (const rel of rels) {
    if (rel.external) continue;
    const target = resolveTarget(path, rel.target);
    const stillUsed = Object.entries(out.rels).some(([owner, list]) => list.some(r => !r.external && resolveTarget(owner, r.target) === target));
    if (!stillUsed && out.parts[target]) removePartTree(out, target);
  }
}

/** The charts of the file as specs, under the ids their pictures have. */
export function chartSpecsFromPreserved(preserved: PreservedParts, workbook: UniverWorkbook, theme: ExcelThemeColors): ChartSpec[] {
  const out: ChartSpec[] = [];
  const decoder = new TextDecoder();
  for (const sheetId of workbook.sheetOrder) {
    const sheet = workbook.sheets[sheetId];
    const charts = sheet ? preserved.bySheet[sheet.name]?.drawing?.charts : undefined;
    charts?.forEach((chart, index) => {
      const part = preserved.parts[chart.chartPath];
      if (!part) return;
      try {
        const spec = chartSpecFromXml(parseXml(decoder.decode(part.bytes)), {
          id: `chart-${sheetId}-${index}`,
          sheet: sheet.name,
          name: chart.name,
          sourcePath: chart.chartPath,
          anchor: { from: chart.from, to: chart.to },
          theme,
        });
        if (spec) out.push(spec);
      } catch {
        // A part we cannot read stays in the file, as a placeholder picture.
      }
    });
  }
  return out;
}
