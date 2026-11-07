import type * as PJ from 'pptxtojson';

import { FidelityCollector, FidelityReport } from '../shared/fidelity';
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
import { hex, htmlToPlainText, textBodyFromHtml } from './pptx-text';

export interface PptxImportOptions {
  id: string;
  name: string;
}

export interface PptxImportResult {
  deck: Deck;
  report: FidelityReport;
}

/** The parser is loaded lazily; the converter itself is pure so tests and workers can feed it JSON. */
export type PptxParser = (file: ArrayBuffer, options?: PJ.Options) => Promise<PptxJson>;
export type PptxJson = Awaited<ReturnType<(typeof import('pptxtojson'))['parse']>>;

export async function pptxToDeck(bytes: ArrayBuffer | Uint8Array, opts: PptxImportOptions, parser?: PptxParser): Promise<PptxImportResult> {
  const parse = parser ?? (await import('pptxtojson')).parse;
  const buffer = bytes instanceof Uint8Array ? bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) : bytes;
  const json = await parse(buffer as ArrayBuffer, { imageMode: 'base64' });
  return jsonToDeck(json, opts);
}

/**
 * pptxtojson's JSON to the deck model: slides, backgrounds, notes, text boxes and shapes with
 * rich text, lines, images, tables, charts and groups. SmartArt becomes its shapes, equations
 * their rendered picture; video and audio are reported as dropped.
 */
export function jsonToDeck(json: PptxJson, opts: PptxImportOptions): PptxImportResult {
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
      background: fillOf(slide.fill, fidelity, where),
      notes: htmlToPlainText(slide.note ?? ''),
      layoutElements: (slide.layoutElements ?? [])
        .map((el, i) => element(el, `layout-${index + 1}-${i}`, fidelity, where))
        .filter((e): e is SlideElement => e !== null),
      elements: slide.elements
        .map((el, i) => element(el, `el-${index + 1}-${i}`, fidelity, where))
        .filter((e): e is SlideElement => e !== null),
    };
    if (slide.transition?.type) fidelity.drop('Slide transitions', where);
    deck.slides.push(out);
  });
  return { deck, report: fidelity.report() };
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
      fidelity.warn('SmartArt converted to shapes', where);
      const g: GroupElement = {
        ...base(el, id, `Diagram`),
        kind: 'group',
        children: el.elements.map((c, i) => element(c, `${id}-${i}`, fidelity, where)).filter((e): e is SlideElement => e !== null),
      };
      return g;
    }
    case 'math': {
      fidelity.warn('Equations kept as pictures', where);
      if (!el.picBase64) {
        fidelity.drop('Equations', where);
        return null;
      }
      const img: ImageElement = { ...base(el, id, 'Equation'), kind: 'image', src: el.picBase64, border: null, geometry: 'rect' };
      return img;
    }
    case 'video':
      fidelity.drop('Video', where);
      return null;
    case 'audio':
      fidelity.drop('Audio', where);
      return null;
    default:
      fidelity.drop('Unsupported slide content', where);
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
  },
  id: string,
  fallbackName: string,
) {
  return {
    id,
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
  if (el.shadow) fidelity.warn('Shadows', where);
  if (el.shapType === 'custom' || (!el.shapType && el.path)) fidelity.warn('Custom shapes exported as rectangles', where);
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
    fidelity.drop('Linked or unsupported images', where);
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
  if (el.filters && Object.keys(el.filters).length) fidelity.warn('Picture corrections', where);
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
    fidelity.drop('Charts of an unsupported kind', where);
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
