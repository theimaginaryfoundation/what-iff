/**
 * The marks a person draws on an image to steer image generation (ADR 0x047), and how they become
 * files: a mask (white where the image may change) from the area brush, and a markup copy (the image
 * with the pen strokes on it) from the pen. Strokes are kept in the image's own pixels, so the view
 * can resize without losing them and exports are full resolution.
 */

/** Area paints the region to change (the mask); pen draws instructions (the markup copy). */
export type MarkupTool = 'area' | 'pen';

export interface MarkupStroke {
  tool: MarkupTool;
  /** CSS colour of a pen stroke; area strokes are drawn in the area colour. */
  color: string;
  /** Line width in image pixels. */
  width: number;
  /** Flat x, y pairs in image pixels. */
  points: number[];
}

/** The pen colours offered, with names for their buttons. Red first: it reads as "change this". */
export const PEN_COLORS: readonly { value: string; name: string }[] = [
  { value: '#ef4444', name: 'Red' },
  { value: '#facc15', name: 'Yellow' },
  { value: '#22c55e', name: 'Green' },
  { value: '#3b82f6', name: 'Blue' },
  { value: '#111827', name: 'Black' },
  { value: '#ffffff', name: 'White' },
];

/** How the area brush shows on screen: the same magenta the image model's guide uses, see-through. */
export const AREA_DISPLAY_COLOR = '#ff00ff';
export const AREA_DISPLAY_ALPHA = 0.45;

/** The page a sketch is drawn on: square, at the size image models work at. */
export const SKETCH_SIZE = 1024;

/** The 2D context calls stroke drawing needs; a narrow type so tests can pass a recorder. */
export type StrokeContext = Pick<
  CanvasRenderingContext2D,
  'beginPath' | 'moveTo' | 'lineTo' | 'arc' | 'stroke' | 'fill' | 'lineCap' | 'lineJoin' | 'lineWidth' | 'strokeStyle' | 'fillStyle'
>;

export function hasTool(strokes: readonly MarkupStroke[], tool: MarkupTool): boolean {
  return strokes.some(s => s.tool === tool);
}

/** Draws strokes of one tool in color (or each stroke's own colour when color is omitted). */
export function drawStrokes(ctx: StrokeContext, strokes: readonly MarkupStroke[], tool: MarkupTool, color?: string): void {
  for (const stroke of strokes) {
    if (stroke.tool === tool) traceStroke(ctx, stroke, color ?? stroke.color);
  }
}

/** One stroke as a round-capped line; a single point is a dot. */
export function traceStroke(ctx: StrokeContext, stroke: MarkupStroke, color: string): void {
  const { points, width } = stroke;
  if (points.length < 2) return;
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.lineWidth = width;
  ctx.strokeStyle = color;
  ctx.fillStyle = color;
  ctx.beginPath();
  if (points.length === 2) {
    ctx.arc(points[0], points[1], width / 2, 0, Math.PI * 2);
    ctx.fill();
    return;
  }
  ctx.moveTo(points[0], points[1]);
  for (let i = 2; i < points.length; i += 2) ctx.lineTo(points[i], points[i + 1]);
  ctx.stroke();
}

/** Makes a canvas; a seam so tests without a canvas implementation can supply one. */
export type CanvasFactory = (width: number, height: number) => HTMLCanvasElement;

export function documentCanvasFactory(doc: Document): CanvasFactory {
  return (width, height) => {
    const canvas = doc.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    return canvas;
  };
}

function context2d(canvas: HTMLCanvasElement): CanvasRenderingContext2D {
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('This browser cannot draw on a canvas.');
  return ctx;
}

/** The mask: black, with the area strokes in white (the image model's "may change" area). */
export function renderMask(make: CanvasFactory, strokes: readonly MarkupStroke[], width: number, height: number): HTMLCanvasElement {
  const canvas = make(width, height);
  const ctx = context2d(canvas);
  ctx.fillStyle = '#000000';
  ctx.fillRect(0, 0, width, height);
  drawStrokes(ctx, strokes, 'area', '#ffffff');
  return canvas;
}

/** The markup copy: the image (or a white page for a sketch) with the pen strokes on it. */
export function renderMarkup(
  make: CanvasFactory,
  image: CanvasImageSource | null,
  strokes: readonly MarkupStroke[],
  width: number,
  height: number,
): HTMLCanvasElement {
  const canvas = make(width, height);
  const ctx = context2d(canvas);
  if (image) {
    ctx.drawImage(image, 0, 0, width, height);
  } else {
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, width, height);
  }
  drawStrokes(ctx, strokes, 'pen');
  return canvas;
}

export function canvasToPng(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) =>
    canvas.toBlob(blob => (blob ? resolve(blob) : reject(new Error('The drawing could not be saved as an image.'))), 'image/png'),
  );
}

/** Where a pointer is in image pixels, for a view of the image drawn into rect at scale. */
export function toImagePoint(clientX: number, clientY: number, rect: { left: number; top: number }, scale: number): [number, number] {
  return [(clientX - rect.left) / scale, (clientY - rect.top) / scale];
}

/** The largest scale at which a width × height image fits the box. Small images grow, to be drawn on. */
export function fitScale(width: number, height: number, boxWidth: number, boxHeight: number): number {
  if (width <= 0 || height <= 0 || boxWidth <= 0 || boxHeight <= 0) return 0;
  return Math.min(boxWidth / width, boxHeight / height);
}

/** A file name's stem, for the derived files' names. */
export function markupStem(name: string): string {
  const dot = name.lastIndexOf('.');
  return (dot > 0 ? name.slice(0, dot) : name) || 'image';
}
