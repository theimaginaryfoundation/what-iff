import { MarkupStroke, StrokeContext, drawStrokes, fitScale, hasTool, markupStem, toImagePoint, traceStroke } from './markup-strokes';

/** Records the drawing calls a stroke makes. */
function recorder(): StrokeContext & { calls: string[] } {
  const calls: string[] = [];
  return {
    calls,
    lineCap: 'butt',
    lineJoin: 'miter',
    lineWidth: 1,
    strokeStyle: '',
    fillStyle: '',
    beginPath: () => calls.push('begin'),
    moveTo: (x: number, y: number) => calls.push(`move ${x},${y}`),
    lineTo: (x: number, y: number) => calls.push(`line ${x},${y}`),
    arc: (x: number, y: number, r: number) => calls.push(`arc ${x},${y} r${r}`),
    stroke: () => calls.push('stroke'),
    fill: () => calls.push('fill'),
  } as unknown as StrokeContext & { calls: string[] };
}

const area = (points: number[]): MarkupStroke => ({ tool: 'area', color: '#ef4444', width: 10, points });
const pen = (points: number[], color = '#3b82f6'): MarkupStroke => ({ tool: 'pen', color, width: 4, points });

describe('markup strokes', () => {
  it('traces a stroke as a round line, and a single point as a dot', () => {
    const ctx = recorder();
    traceStroke(ctx, pen([1, 2, 3, 4, 5, 6]), '#000');
    expect(ctx.calls).toEqual(['begin', 'move 1,2', 'line 3,4', 'line 5,6', 'stroke']);
    expect(ctx.lineCap).toBe('round');
    expect(ctx.lineWidth).toBe(4);

    const dot = recorder();
    traceStroke(dot, area([7, 8]), '#fff');
    expect(dot.calls).toEqual(['begin', 'arc 7,8 r5', 'fill']);
  });

  it('draws only the strokes of one tool, in a given colour or their own', () => {
    const ctx = recorder();
    const strokes = [area([0, 0, 1, 1]), pen([2, 2, 3, 3], '#22c55e')];
    drawStrokes(ctx, strokes, 'area', '#ffffff');
    expect(ctx.calls).toEqual(['begin', 'move 0,0', 'line 1,1', 'stroke']);
    expect(ctx.strokeStyle).toBe('#ffffff');

    const penCtx = recorder();
    drawStrokes(penCtx, strokes, 'pen');
    expect(penCtx.strokeStyle).toBe('#22c55e');
    expect(hasTool(strokes, 'pen')).toBe(true);
    expect(hasTool([pen([0, 0])], 'area')).toBe(false);
  });

  it('maps a pointer to image pixels and fits an image to its box', () => {
    expect(toImagePoint(150, 70, { left: 50, top: 20 }, 0.5)).toEqual([200, 100]);
    expect(fitScale(2000, 1000, 500, 500)).toBe(0.25);
    expect(fitScale(100, 100, 400, 300)).toBe(3);
    expect(fitScale(100, 100, 0, 300)).toBe(0);
  });

  it('names derived files after the image', () => {
    expect(markupStem('photo.final.png')).toBe('photo.final');
    expect(markupStem('.png')).toBe('.png');
    expect(markupStem('')).toBe('image');
  });
});
