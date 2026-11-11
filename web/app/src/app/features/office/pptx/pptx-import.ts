import type * as PJ from 'pptxtojson';

import { FidelityCollector, FidelityReport } from '../shared/fidelity';
import { NS, OoxmlPackage, REL_TYPE, attr, children, resolveTarget } from '../shared/ooxml';
import {
  Border,
  ChartElement,
  Deck,
  Fill,
  GroupElement,
  ImageElement,
  LineElement,
  ShapeElement,
  Slide,
  SlideElement,
  TableCell,
  TableElement,
  emptyTextBody,
} from './deck-model';
import { PptxComments, readPptxComments } from './pptx-comments';
import { hex, htmlToPlainText, textBodyFromHtml } from './pptx-text';

export interface PptxImportOptions {
  id: string;
  name: string;
}

export interface PptxImportResult {
  deck: Deck;
  report: FidelityReport;
  /** The comment threads on its slides, and the comment parts they came from (ADR 0x045). */
  comments: PptxComments;
}

/** The parser is loaded lazily; the converter itself is pure so tests and workers can feed it JSON. */
export type PptxParser = (file: ArrayBuffer, options?: PJ.Options) => Promise<PptxJson>;
export type PptxJson = Awaited<ReturnType<(typeof import('pptxtojson'))['parse']>>;

export async function pptxToDeck(bytes: ArrayBuffer | Uint8Array, opts: PptxImportOptions, parser?: PptxParser): Promise<PptxImportResult> {
  const parse = parser ?? (await import('pptxtojson')).parse;
  const buffer = bytes instanceof Uint8Array ? bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) : bytes;
  const json = await parse(buffer as ArrayBuffer, { imageMode: 'base64' });
  let slideParts: SlideParts | undefined;
  let pkg: OoxmlPackage | null = null;
  try {
    pkg = await OoxmlPackage.open(bytes);
    slideParts = await listSlideParts(pkg);
  } catch {
    slideParts = undefined;
  }
  const result = jsonToDeck(json, opts, slideParts);
  if (pkg && slideParts) result.comments = await readPptxComments(pkg, slideParts.presPath, result.deck);
  return result;
}

const REL_SLIDE = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide';
const CT_SLIDE = 'application/vnd.openxmlformats-officedocument.presentationml.slide+xml';
const P = 'http://schemas.openxmlformats.org/presentationml/2006/main';

export interface SlideParts {
  /** Slide parts by the number in their name, the order pptxtojson reads them in (it lists the content types). */
  byNumber: string[];
  /** Slide parts in presentation order (`p:sldIdLst`), the order PowerPoint shows them in. */
  order: string[];
  /** The presentation part. */
  presPath: string;
}

export async function listSlideParts(pkg: OoxmlPackage): Promise<SlideParts> {
  const byNumber: string[] = [];
  const types = await pkg.xml('[Content_Types].xml');
  for (const el of Array.from(types?.getElementsByTagNameNS(NS.ct, 'Override') ?? [])) {
    if (el.getAttribute('ContentType') === CT_SLIDE) byNumber.push((el.getAttribute('PartName') ?? '').replace(/^\//, ''));
  }
  const n = (path: string) => Number(/(\d+)\.xml$/.exec(path)?.[1] ?? 0);
  byNumber.sort((a, b) => n(a) - n(b));
  const root = await pkg.rels('');
  const main = [...root.values()].find(r => r.type === REL_TYPE.officeDocument);
  const presPath = main ? resolveTarget('', main.target) : 'ppt/presentation.xml';
  const pres = await pkg.xml(presPath);
  const order: string[] = [];
  if (pres) {
    const rels = await pkg.rels(presPath);
    for (const sldId of children(children(pres.documentElement, P, 'sldIdLst')[0], P, 'sldId')) {
      const rel = rels.get(attr(sldId, NS.r, 'id') ?? '');
      if (rel && rel.type === REL_SLIDE) order.push(resolveTarget(presPath, rel.target));
    }
  }
  return { byNumber, order, presPath };
}

/**
 * pptxtojson's JSON to the deck model: slides, backgrounds, notes, text boxes and shapes with
 * rich text, lines, images, tables, charts and groups. SmartArt becomes its shapes, equations
 * their rendered picture; video and audio are reported as dropped.
 */
export function jsonToDeck(json: PptxJson, opts: PptxImportOptions, slideParts?: SlideParts): PptxImportResult {
  const slidePaths = slideParts?.byNumber ?? [];
  const fidelity = new FidelityCollector();
  const deck: Deck = {
    id: opts.id,
    name: opts.name,
    size: { width: json.size.width, height: json.size.height },
    themeColors: (json.themeColors ?? []).map(c => hex(c) ?? c),
    slides: [],
  };
  json.slides.forEach((slide, index) => {
    const where = `Slide ${index + 1}`;
    const out: Slide = {
      id: `slide-${index + 1}`,
      ...(slidePaths[index] ? { sourcePath: slidePaths[index] } : {}),
      background: fillOf(slide.fill, fidelity, where),
      notes: htmlToPlainText(slide.note ?? ''),
      layoutElements: (slide.layoutElements ?? [])
        .map((el, i) => element(el, `layout-${index + 1}-${i}`, fidelity, where))
        .filter((e): e is SlideElement => e !== null),
      elements: uniqueSourceIds(
        slide.elements.map((el, i) => element(el, `el-${index + 1}-${i}`, fidelity, where)).filter((e): e is SlideElement => e !== null),
      ),
    };
    if (slide.transition?.type) fidelity.warn('Transitions and animations are kept but not played', where);
    deck.slides.push(out);
  });
  // pptxtojson reads slide parts by number; the presentation's own list says what order they show in.
  if (slideParts?.order.length && deck.slides.every(s => s.sourcePath)) {
    const byPath = new Map(deck.slides.map(s => [s.sourcePath!, s]));
    const ordered = slideParts.order.map(path => byPath.get(path)).filter((s): s is Slide => !!s);
    if (ordered.length < deck.slides.length) fidelity.warn('Slide parts the presentation does not list were left out');
    if (ordered.length) deck.slides = ordered;
  }
  return { deck, report: fidelity.report(), comments: { threads: [], authors: [] } };
}

/**
 * A slide's shape ids should be unique, but some writers repeat them; the n-th repeat of an id
 * becomes "id#n" so it still names one shape (pptx-patch numbers the tree's repeats the same way).
 */
export function uniqueSourceIds<T extends { sourceId?: string }>(elements: T[]): T[] {
  const seen = new Map<string, number>();
  return elements.map(el => {
    if (!el.sourceId) return el;
    const n = seen.get(el.sourceId) ?? 0;
    seen.set(el.sourceId, n + 1);
    return n === 0 ? el : { ...el, sourceId: `${el.sourceId}#${n}` };
  });
}

function element(el: PJ.Element, id: string, fidelity: FidelityCollector, where: string): SlideElement | null {
  switch (el.type) {
    case 'shape':
      return shape(el, id, fidelity, where);
    case 'text':
      return textBox(el, id);
    case 'image':
      return image(el, id, fidelity, where);
    case 'table':
      return table(el, id);
    case 'chart':
      return chart(el, id, fidelity, where);
    case 'group':
      return group(el, id, fidelity, where);
    case 'diagram': {
      fidelity.warn('SmartArt shown as its shapes', where);
      const g: GroupElement = {
        ...base(el, id, `Diagram`),
        kind: 'group',
        children: el.elements.map((c, i) => element(c, `${id}-${i}`, fidelity, where)).filter((e): e is SlideElement => e !== null),
      };
      return g;
    }
    case 'math': {
      fidelity.warn('Equations shown as pictures', where);
      if (!el.picBase64) {
        fidelity.warn('Equations are kept but not shown', where);
        return null;
      }
      const img: ImageElement = { ...base(el, id, 'Equation'), kind: 'image', src: el.picBase64, border: null, geometry: 'rect' };
      return img;
    }
    case 'video':
    case 'audio':
      fidelity.warn('Video and audio are kept in the file but not shown', where);
      return null;
    default:
      fidelity.warn('Unsupported slide content is kept in the file but not shown', where);
      return null;
  }
}

function base(
  el: {
    left: number;
    top: number;
    width: number;
    height: number;
    rotate?: number;
    isFlipH?: boolean;
    isFlipV?: boolean;
    name?: string;
    link?: string;
    id?: string;
  },
  id: string,
  fallbackName: string,
) {
  return {
    id,
    ...(el.id ? { sourceId: String(el.id) } : {}),
    name: el.name ?? fallbackName,
    left: round(el.left),
    top: round(el.top),
    width: round(el.width),
    height: round(el.height),
    rotate: el.rotate ?? 0,
    flipH: !!el.isFlipH,
    flipV: !!el.isFlipV,
    ...(el.link ? { link: el.link } : {}),
  };
}

function round(n: number): number {
  return Math.round((n ?? 0) * 100) / 100;
}

function shape(el: PJ.Shape, id: string, fidelity: FidelityCollector, where: string): SlideElement {
  const border = borderOf(el.borderColor, el.borderWidth, el.borderType);
  if (
    el.shapType === 'line' ||
    el.shapType === 'straightConnector1' ||
    el.shapType === 'bentConnector3' ||
    el.shapType === 'curvedConnector3'
  ) {
    if (el.shapType !== 'line' && el.shapType !== 'straightConnector1') fidelity.warn('Elbow and curved connectors drawn straight', where);
    const line: LineElement = {
      ...base(el, id, 'Line'),
      kind: 'line',
      border: border ?? { color: '#000000', width: 1, style: 'solid' },
      headEnd: { type: el.headEnd?.type ?? 'none' },
      tailEnd: { type: el.tailEnd?.type ?? 'none' },
    };
    return line;
  }
  const fill = fillOf(el.fill, fidelity, where);
  const hasText = !!el.content && el.content.replace(/<[^>]+>/g, '').trim() !== '';
  const out: ShapeElement = {
    ...base(el, id, 'Shape'),
    kind: 'shape',
    shapeType: el.shapType || 'rect',
    fill,
    border,
    text: hasText || !fill ? textBodyFromHtml(el.content ?? '', el.vAlign, el.textInset, el.wrap !== false, el.autoFit) : null,
    isTextBox: !fill && !border && (el.shapType === 'rect' || !el.shapType),
  };
  if (el.path) out.path = el.path;
  if (el.pathViewBox) out.viewBox = el.pathViewBox;
  if (el.keypoints && Object.keys(el.keypoints).length) out.adjustments = el.keypoints;
  if (el.shadow) fidelity.warn('Shadows are kept but not drawn', where);
  return out;
}

function textBox(el: PJ.Text, id: string): ShapeElement {
  return {
    ...base(el, id, 'Text'),
    kind: 'shape',
    shapeType: 'rect',
    fill: el.fill ? fillOf(el.fill, undefined, '') : null,
    border: borderOf(el.borderColor, el.borderWidth, el.borderType),
    text: textBodyFromHtml(el.content, el.vAlign, el.textInset, el.wrap !== false, el.autoFit),
    isTextBox: true,
  };
}

function image(el: PJ.Image, id: string, fidelity: FidelityCollector, where: string): ImageElement | null {
  const src = el.base64 || el.blob;
  if (!src) {
    fidelity.warn('Linked or unsupported images are kept but not shown', where);
    return null;
  }
  const out: ImageElement = {
    ...base(el, id, 'Picture'),
    kind: 'image',
    src,
    border: borderOf(el.borderColor, el.borderWidth, el.borderType),
    geometry: el.geom || 'rect',
  };
  if (el.rect && (el.rect.t || el.rect.b || el.rect.l || el.rect.r))
    out.crop = { top: (el.rect.t ?? 0) / 100, bottom: (el.rect.b ?? 0) / 100, left: (el.rect.l ?? 0) / 100, right: (el.rect.r ?? 0) / 100 };
  if (el.filters && Object.keys(el.filters).length) fidelity.warn('Picture corrections are kept but not drawn', where);
  return out;
}

function table(el: PJ.Table, id: string): TableElement {
  const rows: TableCell[][] = el.data.map(row =>
    row.map(cell => ({
      text: textBodyFromHtml(cell.text, cell.vAlign),
      fill: cell.fillColor ? { type: 'color', color: hex(cell.fillColor) ?? cell.fillColor } : null,
      borders: {
        top: cellBorder(cell.borders?.top),
        bottom: cellBorder(cell.borders?.bottom),
        left: cellBorder(cell.borders?.left),
        right: cellBorder(cell.borders?.right),
      },
      rowSpan: cell.rowSpan ?? 1,
      colSpan: cell.colSpan ?? 1,
      covered: !!cell.vMerge || !!cell.hMerge,
    })),
  );
  const colCount = Math.max(0, ...rows.map(r => r.length));
  const colWidths = el.colWidths?.length
    ? el.colWidths.map(round)
    : Array.from({ length: colCount }, () => round(el.width / Math.max(1, colCount)));
  const rowHeights = (el.rowHeights ?? []).map(round);
  const evenRow = rows.length ? round(el.height / rows.length) : 0;
  return { ...base(el, id, 'Table'), kind: 'table', rows, colWidths, rowHeights: rows.map((_, i) => rowHeights[i] || evenRow) };
}

function cellBorder(b: PJ.Border | undefined): Border | undefined {
  if (!b || !b.borderWidth) return undefined;
  return borderOf(b.borderColor, b.borderWidth, b.borderType) ?? undefined;
}

function chart(el: PJ.Chart, id: string, fidelity: FidelityCollector, where: string): SlideElement | null {
  const b = base(el, id, 'Chart');
  const colors = (el.colors ?? []).map(c => hex(c) ?? c);
  if (el.chartType === 'scatterChart' || el.chartType === 'bubbleChart') {
    const data = el.data as number[][];
    const xs = data[0] ?? [];
    const series = data.slice(1).map((ys, i) => ({ name: `Series ${i + 1}`, labels: xs.map(String), values: ys }));
    const out: ChartElement = { ...b, kind: 'chart', chartType: el.chartType === 'bubbleChart' ? 'bubble' : 'scatter', series, colors };
    return out;
  }
  const common = el as PJ.CommonChart;
  const typeMap: Record<string, ChartElement['chartType']> = {
    lineChart: 'line',
    line3DChart: 'line',
    barChart: 'bar',
    bar3DChart: 'bar',
    pieChart: 'pie',
    pie3DChart: 'pie',
    doughnutChart: 'doughnut',
    areaChart: 'area',
    area3DChart: 'area',
    radarChart: 'radar',
  };
  const chartType = typeMap[el.chartType];
  if (!chartType) {
    fidelity.warn('Charts of an unsupported kind are kept but not drawn', where);
    return null;
  }
  if (el.chartType.includes('3D')) fidelity.warn('3D charts drawn flat', where);
  const series = (common.data ?? []).map(item => ({
    name: item.key,
    labels: item.values.map(v => item.xlabels?.[v.x] ?? v.x),
    values: item.values.map(v => v.y),
  }));
  const out: ChartElement = { ...b, kind: 'chart', chartType, series, colors };
  if (common.barDir) out.barDirection = common.barDir;
  if (common.holeSize) out.holeSize = Number(String(common.holeSize).replace('%', '')) || undefined;
  if (common.grouping) out.grouping = common.grouping;
  return out;
}

function group(el: PJ.Group, id: string, fidelity: FidelityCollector, where: string): GroupElement {
  return {
    ...base(el, id, 'Group'),
    kind: 'group',
    children: el.elements.map((c, i) => element(c, `${id}-${i}`, fidelity, where)).filter((e): e is SlideElement => e !== null),
  };
}

function borderOf(color: string | undefined, width: number | undefined, type: string | undefined): Border | null {
  if (!width || width <= 0) return null;
  return {
    color: hex(color) ?? '#000000',
    width: round(width),
    style: type === 'dashed' ? 'dashed' : type === 'dotted' ? 'dotted' : 'solid',
  };
}

export function fillOf(fill: PJ.Fill | null | undefined, fidelity: FidelityCollector | undefined, where: string): Fill | null {
  if (!fill) return null;
  switch (fill.type) {
    case 'color': {
      const color = hex(fill.value);
      return color ? { type: 'color', color } : null;
    }
    case 'gradient': {
      const stops = fill.value.colors.map(c => ({ pos: Number(String(c.pos).replace('%', '')) || 0, color: hex(c.color) ?? c.color }));
      if (!stops.length) return null;
      return { type: 'gradient', angle: fill.value.rot ?? 0, stops };
    }
    case 'image': {
      const src = fill.value.base64 || fill.value.blob;
      if (!src) return null;
      return { type: 'image', src, opacity: fill.value.opacity };
    }
    case 'pattern':
      fidelity?.warn('Pattern fills shown as their foreground colour', where);
      return { type: 'color', color: hex(fill.value.foregroundColor) ?? '#808080' };
    default:
      return null;
  }
}

export { emptyTextBody };
