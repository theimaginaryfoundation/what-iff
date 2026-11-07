import type PptxGenJS from 'pptxgenjs';

import { FidelityCollector, FidelityReport } from '../shared/fidelity';
import { OoxmlPackage } from '../shared/ooxml';
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
  TableElement,
  TextBody,
} from './deck-model';
import { paragraphsToRuns } from './pptx-text';

export interface PptxExportResult {
  bytes: Uint8Array;
  report: FidelityReport;
}

export type PptxGenFactory = () => Promise<PptxGenJS>;

const PT_PER_INCH = 72;

/** Writes the deck as a .pptx with pptxgenjs; the mirror of pptxToDeck. */
export async function deckToPptx(deck: Deck, factory?: PptxGenFactory): Promise<PptxExportResult> {
  const fidelity = new FidelityCollector();
  const pptx = factory ? await factory() : new (await import('pptxgenjs')).default();
  writeDeck(pptx, deck, fidelity);
  const out = await pptx.write({ outputType: 'arraybuffer' });
  const raw =
    out instanceof ArrayBuffer ? new Uint8Array(out) : out instanceof Uint8Array ? out : new Uint8Array(await (out as Blob).arrayBuffer());
  return { bytes: await normalizePackage(raw), report: fidelity.report() };
}

const PPTX_MIME = 'application/vnd.openxmlformats-officedocument.presentationml.presentation';

/**
 * Two pptxgenjs habits are rewritten into the form PowerPoint itself writes: a paragraph-properties
 * element before every run of a paragraph (PresentationML allows one, first in a:p), and chart
 * categories as a multi-level string reference even when there is a single level (readers such
 * as pptxtojson only look for the plain string reference).
 */
export async function normalizePackage(bytes: Uint8Array): Promise<Uint8Array> {
  const pkg = await OoxmlPackage.open(bytes);
  let changed = false;
  for (const path of pkg.paths()) {
    const isSlide = /^ppt\/(slides|notesSlides|slideLayouts|slideMasters)\/[^/]+\.xml$/.test(path);
    const isChart = /^ppt\/charts\/chart[^/]*\.xml$/.test(path);
    if (!isSlide && !isChart) continue;
    const xml = await pkg.text(path);
    if (!xml) continue;
    const fixed = isChart ? singleLevelCategories(xml) : dedupeParagraphProperties(xml);
    if (fixed !== xml) {
      pkg.put(path, fixed);
      changed = true;
    }
  }
  return changed ? pkg.save(PPTX_MIME) : bytes;
}

function singleLevelCategories(xml: string): string {
  return xml.replace(
    /<c:multiLvlStrRef>([\s\S]*?)<c:multiLvlStrCache>([\s\S]*?)<\/c:multiLvlStrCache>\s*<\/c:multiLvlStrRef>/g,
    (whole, ref: string, cache: string) => {
      const levels = cache.match(/<c:lvl>/g)?.length ?? 0;
      if (levels !== 1) return whole;
      const inner = cache.replace(/<\/?c:lvl>/g, '');
      return `<c:strRef>${ref}<c:strCache>${inner}</c:strCache></c:strRef>`;
    },
  );
}

function dedupeParagraphProperties(xml: string): string {
  return xml.replace(/<a:p>([\s\S]*?)<\/a:p>/g, (whole, inner: string) => {
    let seen = false;
    const cleaned = inner.replace(/<a:pPr\b[^>]*\/>|<a:pPr\b[^>]*>[\s\S]*?<\/a:pPr>/g, m => {
      if (seen) return '';
      seen = true;
      return m;
    });
    return cleaned === inner ? whole : `<a:p>${cleaned}</a:p>`;
  });
}

export function writeDeck(pptx: PptxGenJS, deck: Deck, fidelity: FidelityCollector): void {
  pptx.defineLayout({ name: 'DECK', width: deck.size.width / PT_PER_INCH, height: deck.size.height / PT_PER_INCH });
  pptx.layout = 'DECK';
  pptx.title = deck.name;
  deck.slides.forEach((slide, index) => {
    const where = `Slide ${index + 1}`;
    const target = pptx.addSlide();
    writeBackground(target, slide, fidelity, where);
    if (slide.layoutElements.length) fidelity.warn('Layout graphics became slide shapes', where);
    for (const el of slide.layoutElements) writeElement(pptx, target, el, 0, 0, fidelity, where);
    for (const el of slide.elements) writeElement(pptx, target, el, 0, 0, fidelity, where);
    if (slide.notes) target.addNotes(slide.notes);
  });
}

function writeBackground(slide: PptxGenJS.Slide, source: Slide, fidelity: FidelityCollector, where: string): void {
  const bg = source.background;
  if (!bg) return;
  if (bg.type === 'color') slide.background = { color: rgb(bg.color) };
  else if (bg.type === 'image') slide.background = { data: imageData(bg.src) };
  else {
    fidelity.warn('Gradient backgrounds flattened to their first colour', where);
    slide.background = { color: rgb(bg.stops[0]?.color ?? '#FFFFFF') };
  }
}

function writeElement(
  pptx: PptxGenJS,
  slide: PptxGenJS.Slide,
  el: SlideElement,
  dx: number,
  dy: number,
  fidelity: FidelityCollector,
  where: string,
): void {
  switch (el.kind) {
    case 'shape':
      writeShape(pptx, slide, el, dx, dy, fidelity, where);
      return;
    case 'line':
      writeLine(pptx, slide, el, dx, dy);
      return;
    case 'image':
      writeImage(slide, el, dx, dy);
      return;
    case 'table':
      writeTable(slide, el, dx, dy);
      return;
    case 'chart':
      writeChart(pptx, slide, el, dx, dy, fidelity, where);
      return;
    case 'group':
      writeGroup(pptx, slide, el, dx, dy, fidelity, where);
      return;
  }
}

function position(el: { left: number; top: number; width: number; height: number }, dx: number, dy: number): PptxGenJS.PositionProps {
  return { x: inches(el.left + dx), y: inches(el.top + dy), w: inches(el.width), h: inches(el.height) };
}

function inches(pt: number): number {
  return Math.round((pt / PT_PER_INCH) * 10000) / 10000;
}

function rgb(color: string): string {
  return color.replace('#', '').toUpperCase();
}

function imageData(src: string): string {
  return src.startsWith('data:') ? src.slice(5) : src;
}

function fillProps(fill: Fill | null, fidelity: FidelityCollector, where: string): PptxGenJS.ShapeFillProps | undefined {
  if (!fill) return undefined;
  if (fill.type === 'color') {
    const props: PptxGenJS.ShapeFillProps = { color: rgb(fill.color) };
    if (fill.opacity !== undefined && fill.opacity < 1) props.transparency = Math.round((1 - fill.opacity) * 100);
    return props;
  }
  if (fill.type === 'gradient') {
    fidelity.warn('Gradient fills flattened to their first colour', where);
    return { color: rgb(fill.stops[0]?.color ?? '#FFFFFF') };
  }
  fidelity.warn('Picture fills flattened', where);
  return undefined;
}

function lineProps(border: Border | null): PptxGenJS.ShapeLineProps | undefined {
  if (!border) return undefined;
  return {
    color: rgb(border.color),
    width: border.width,
    dashType: border.style === 'dashed' ? 'dash' : border.style === 'dotted' ? 'sysDot' : 'solid',
  };
}

function textOptions(body: TextBody): Partial<PptxGenJS.TextPropsOptions> {
  return {
    valign: body.vAlign,
    margin: [body.inset[0], body.inset[2], body.inset[1], body.inset[3]].map(v => v) as [number, number, number, number],
    wrap: body.wrap,
    fit: body.autoFit === 'shrink' ? 'shrink' : body.autoFit === 'resize' ? 'resize' : 'none',
  };
}

function writeShape(
  pptx: PptxGenJS,
  slide: PptxGenJS.Slide,
  el: ShapeElement,
  dx: number,
  dy: number,
  fidelity: FidelityCollector,
  where: string,
): void {
  const shapeTypes = pptx.ShapeType as unknown as Record<string, PptxGenJS.SHAPE_NAME>;
  let shape = shapeTypes[el.shapeType];
  if (!shape) {
    if (el.shapeType !== 'rect') fidelity.warn('Custom shapes exported as rectangles', where);
    shape = shapeTypes['rect'];
  }
  const common: PptxGenJS.ShapeProps = {
    ...position(el, dx, dy),
    rotate: el.rotate || undefined,
    flipH: el.flipH || undefined,
    flipV: el.flipV || undefined,
  };
  const fill = fillProps(el.fill, fidelity, where);
  const line = lineProps(el.border);
  if (fill) common.fill = fill;
  if (line) common.line = line;
  if (el.shapeType === 'roundRect' && el.adjustments?.['adj'] !== undefined)
    common.rectRadius = inches(Math.min(el.width, el.height) * el.adjustments['adj']);
  if (el.link) common.hyperlink = { url: el.link };
  if (el.text && el.text.paragraphs.length) {
    slide.addText(paragraphsToRuns(el.text.paragraphs), {
      ...common,
      shape,
      ...textOptions(el.text),
      isTextBox: el.isTextBox || undefined,
    } as PptxGenJS.TextPropsOptions);
  } else {
    slide.addShape(shape, common);
  }
}

function writeLine(pptx: PptxGenJS, slide: PptxGenJS.Slide, el: LineElement, dx: number, dy: number): void {
  const line = lineProps(el.border) ?? { color: '000000', width: 1 };
  if (el.headEnd.type !== 'none') line.beginArrowType = el.headEnd.type;
  if (el.tailEnd.type !== 'none') line.endArrowType = el.tailEnd.type;
  slide.addShape(pptx.ShapeType.line, {
    ...position(el, dx, dy),
    line,
    rotate: el.rotate || undefined,
    flipH: el.flipH || undefined,
    flipV: el.flipV || undefined,
  });
}

function writeImage(slide: PptxGenJS.Slide, el: ImageElement, dx: number, dy: number): void {
  const props: PptxGenJS.ImageProps = {
    ...position(el, dx, dy),
    data: imageData(el.src),
    rotate: el.rotate || undefined,
    flipH: el.flipH || undefined,
    flipV: el.flipV || undefined,
  };
  if (el.link) props.hyperlink = { url: el.link };
  if (el.crop) {
    const w = inches(el.width);
    const h = inches(el.height);
    const fullW = w / Math.max(0.01, 1 - el.crop.left - el.crop.right);
    const fullH = h / Math.max(0.01, 1 - el.crop.top - el.crop.bottom);
    props.sizing = { type: 'crop', x: fullW * el.crop.left, y: fullH * el.crop.top, w, h };
  }
  if (el.geometry === 'ellipse') props.rounding = true;
  slide.addImage(props);
}

function writeTable(slide: PptxGenJS.Slide, el: TableElement, dx: number, dy: number): void {
  const rows: PptxGenJS.TableRow[] = el.rows.map(row =>
    row
      .filter(cell => !cell.covered)
      .map(cell => {
        const options: PptxGenJS.TableCellProps = {
          valign: cell.text.vAlign,
          margin: [cell.text.inset[1], cell.text.inset[2], cell.text.inset[3], cell.text.inset[0]],
        };
        if (cell.fill?.type === 'color') options.fill = { color: rgb(cell.fill.color) };
        if (cell.rowSpan > 1) options.rowspan = cell.rowSpan;
        if (cell.colSpan > 1) options.colspan = cell.colSpan;
        const b = cell.borders;
        options.border = [borderProps(b.top), borderProps(b.right), borderProps(b.bottom), borderProps(b.left)];
        return { text: paragraphsToRuns(cell.text.paragraphs), options };
      }),
  );
  slide.addTable(rows, {
    ...position(el, dx, dy),
    colW: el.colWidths.map(inches),
    rowH: el.rowHeights.map(inches),
    autoPage: false,
  });
}

function borderProps(border: Border | undefined): PptxGenJS.BorderProps {
  if (!border) return { type: 'none' };
  return { type: border.style === 'solid' ? 'solid' : 'dash', color: rgb(border.color), pt: border.width };
}

function writeChart(
  pptx: PptxGenJS,
  slide: PptxGenJS.Slide,
  el: ChartElement,
  dx: number,
  dy: number,
  fidelity: FidelityCollector,
  where: string,
): void {
  const types = pptx.ChartType as unknown as Record<string, PptxGenJS.CHART_NAME>;
  const type = types[el.chartType];
  if (!type || !el.series.length) {
    fidelity.drop('Charts of an unsupported kind', where);
    return;
  }
  const options: PptxGenJS.IChartOpts = { ...position(el, dx, dy), chartColors: el.colors.map(rgb) };
  if (el.chartType === 'bar') options.barDir = el.barDirection ?? 'col';
  if (el.chartType === 'doughnut' && el.holeSize) options.holeSize = el.holeSize;
  if (el.grouping === 'stacked' || el.grouping === 'percentStacked') options.barGrouping = el.grouping;
  if (el.chartType === 'scatter' || el.chartType === 'bubble') {
    const labels = el.series[0]?.labels.map(Number) ?? [];
    const data: PptxGenJS.IChartMulti[] | PptxGenJS.OptsChartData[] = [
      { name: 'X', values: labels },
      ...el.series.map(s => ({ name: s.name, values: s.values })),
    ];
    slide.addChart(type, data as never, options);
    return;
  }
  slide.addChart(type, el.series.map(s => ({ name: s.name, labels: s.labels, values: s.values })) as never, options);
}

function writeGroup(
  pptx: PptxGenJS,
  slide: PptxGenJS.Slide,
  el: GroupElement,
  dx: number,
  dy: number,
  fidelity: FidelityCollector,
  where: string,
): void {
  fidelity.warn('Groups flattened', where);
  for (const child of el.children) writeElement(pptx, slide, child, dx + el.left, dy + el.top, fidelity, where);
}
