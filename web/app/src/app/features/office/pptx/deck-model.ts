/**
 * The deck model our Slides editor edits (ADR 0x043, office suite): a presentation as slides of
 * positioned elements, in points, with rich text as paragraphs of runs. pptxtojson fills it from a
 * .pptx and pptxgenjs writes it back; the editor never sees either library.
 */

export interface Deck {
  id: string;
  name: string;
  /** Slide size in points (16:9 is 720 x 405). */
  size: { width: number; height: number };
  themeColors: string[];
  slides: Slide[];
}

export interface Slide {
  id: string;
  /** The part this slide came from (such as "ppt/slides/slide3.xml"); absent for a slide made here. */
  sourcePath?: string;
  /** For a duplicate: the part of the slide it was copied from, so the save can copy that part. */
  clonedFrom?: string;
  background: Fill | null;
  /** Speaker notes as plain text. */
  notes: string;
  /** Shapes from the slide's layout and master, shown behind the slide's own elements, not editable. */
  layoutElements: SlideElement[];
  elements: SlideElement[];
}

export type Fill =
  | { type: 'color'; color: string; opacity?: number }
  | { type: 'gradient'; angle: number; stops: { pos: number; color: string }[] }
  | { type: 'image'; src: string; opacity?: number };

export interface Border {
  color: string;
  /** Points. */
  width: number;
  style: 'solid' | 'dashed' | 'dotted';
}

export interface LineEnd {
  type: 'none' | 'triangle' | 'stealth' | 'diamond' | 'oval' | 'arrow';
}

export interface ElementBase {
  id: string;
  /** The shape id (`p:cNvPr@id`) in the slide part this element came from; absent for an element made here. */
  sourceId?: string;
  name: string;
  left: number;
  top: number;
  width: number;
  height: number;
  rotate: number;
  flipH: boolean;
  flipV: boolean;
  link?: string;
}

export interface Run {
  text: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  /** "#RRGGBB". */
  color?: string;
  /** Points. */
  fontSize?: number;
  fontFace?: string;
  highlight?: string;
  baseline?: 'super' | 'sub';
  link?: string;
}

export interface Paragraph {
  align: 'left' | 'center' | 'right' | 'justify';
  bullet?: { type: 'bullet' | 'number'; char?: string; numberType?: string; level: number };
  /** Points. */
  indent?: number;
  spaceBefore?: number;
  spaceAfter?: number;
  /** A multiple of the font size, such as 1.5. */
  lineSpacing?: number;
  runs: Run[];
}

export interface TextBody {
  paragraphs: Paragraph[];
  vAlign: 'top' | 'middle' | 'bottom';
  /** Points: left, top, right, bottom. */
  inset: [number, number, number, number];
  wrap: boolean;
  autoFit: 'none' | 'shrink' | 'resize';
}

export interface ShapeElement extends ElementBase {
  kind: 'shape';
  /** A DrawingML preset name such as "rect", "roundRect", "ellipse"; "custom" when only a path is known. */
  shapeType: string;
  /** The outline as an SVG path in the element's own point coordinates, for rendering custom and preset shapes alike. */
  path?: string;
  viewBox?: { x: number; y: number; width: number; height: number };
  /** Preset adjustments such as the corner radius ratio of a rounded rectangle. */
  adjustments?: Record<string, number>;
  fill: Fill | null;
  border: Border | null;
  text: TextBody | null;
  /** True for a text box: a shape drawn only for its text. */
  isTextBox: boolean;
}

export interface LineElement extends ElementBase {
  kind: 'line';
  border: Border;
  headEnd: LineEnd;
  tailEnd: LineEnd;
}

export interface ImageElement extends ElementBase {
  kind: 'image';
  /** A data: URL. */
  src: string;
  /** Crop as fractions of the source, each 0..1. */
  crop?: { top: number; bottom: number; left: number; right: number };
  border: Border | null;
  /** A preset geometry the picture is clipped to, "rect" when plain. */
  geometry: string;
}

export interface TableCell {
  text: TextBody;
  fill: Fill | null;
  borders: { top?: Border; bottom?: Border; left?: Border; right?: Border };
  rowSpan: number;
  colSpan: number;
  /** A cell covered by another cell's span; it is drawn by the owner and kept for the grid. */
  covered: boolean;
}

export interface TableElement extends ElementBase {
  kind: 'table';
  rows: TableCell[][];
  /** Points. */
  colWidths: number[];
  rowHeights: number[];
}

export interface ChartSeries {
  name: string;
  labels: string[];
  values: number[];
}

export interface ChartElement extends ElementBase {
  kind: 'chart';
  chartType: 'bar' | 'line' | 'pie' | 'doughnut' | 'area' | 'scatter' | 'radar' | 'bubble';
  barDirection?: 'bar' | 'col';
  series: ChartSeries[];
  colors: string[];
  holeSize?: number;
  grouping?: string;
}

export interface GroupElement extends ElementBase {
  kind: 'group';
  /** Children positioned relative to the group's own top-left. */
  children: SlideElement[];
}

export type SlideElement = ShapeElement | LineElement | ImageElement | TableElement | ChartElement | GroupElement;

export const DEFAULT_INSET: [number, number, number, number] = [7.2, 3.6, 7.2, 3.6];

export function emptyTextBody(paragraphs: Paragraph[] = []): TextBody {
  return { paragraphs, vAlign: 'top', inset: [...DEFAULT_INSET] as [number, number, number, number], wrap: true, autoFit: 'none' };
}

export function plainText(body: TextBody | null | undefined): string {
  return (body?.paragraphs ?? []).map(p => p.runs.map(r => r.text).join('')).join('\n');
}

export function newId(prefix = 'el'): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`;
}

/** A new deck with one blank 16:9 slide. */
export function blankDeck(id: string, name: string): Deck {
  return {
    id,
    name,
    size: { width: 720, height: 405 },
    themeColors: ['#4472C4', '#ED7D31', '#A5A5A5', '#FFC000', '#5B9BD5', '#70AD47'],
    slides: [blankSlide()],
  };
}

export function blankSlide(): Slide {
  return { id: newId('slide'), background: null, notes: '', layoutElements: [], elements: [] };
}
