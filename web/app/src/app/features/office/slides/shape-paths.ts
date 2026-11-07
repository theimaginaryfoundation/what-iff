/**
 * Preset geometry for the Slides editor: DrawingML preset names to SVG paths in the element's own
 * coordinates (0,0 to width,height). A preset not listed draws as a rectangle; an imported shape
 * with its own path is drawn from that path instead.
 */

export interface ShapePreset {
  /** The DrawingML name, as pptxgenjs and pptxtojson use it. */
  type: string;
  label: string;
}

/** The shapes offered by the insert menu. */
export const SHAPE_PRESETS: readonly ShapePreset[] = [
  { type: 'rect', label: 'Rectangle' },
  { type: 'roundRect', label: 'Rounded rectangle' },
  { type: 'ellipse', label: 'Ellipse' },
  { type: 'triangle', label: 'Triangle' },
  { type: 'rtTriangle', label: 'Right triangle' },
  { type: 'diamond', label: 'Diamond' },
  { type: 'parallelogram', label: 'Parallelogram' },
  { type: 'trapezoid', label: 'Trapezoid' },
  { type: 'pentagon', label: 'Pentagon' },
  { type: 'hexagon', label: 'Hexagon' },
  { type: 'octagon', label: 'Octagon' },
  { type: 'star5', label: 'Star' },
  { type: 'rightArrow', label: 'Right arrow' },
  { type: 'leftArrow', label: 'Left arrow' },
  { type: 'upArrow', label: 'Up arrow' },
  { type: 'downArrow', label: 'Down arrow' },
  { type: 'chevron', label: 'Chevron' },
  { type: 'homePlate', label: 'Pentagon arrow' },
  { type: 'flowChartTerminator', label: 'Stadium' },
  { type: 'wedgeRectCallout', label: 'Callout' },
  { type: 'cloud', label: 'Cloud' },
  { type: 'heart', label: 'Heart' },
];

const r = (n: number) => Math.round(n * 100) / 100;

function polygon(points: [number, number][]): string {
  return `M${points.map(([x, y]) => `${r(x)},${r(y)}`).join('L')}Z`;
}

function regular(sides: number, w: number, h: number, rotation = -Math.PI / 2): string {
  const cx = w / 2;
  const cy = h / 2;
  const points: [number, number][] = [];
  for (let i = 0; i < sides; i++) {
    const a = rotation + (i * 2 * Math.PI) / sides;
    points.push([cx + (w / 2) * Math.cos(a), cy + (h / 2) * Math.sin(a)]);
  }
  return polygon(points);
}

export function shapePath(type: string, w: number, h: number, adjustments: Record<string, number> = {}): string {
  const m = Math.min(w, h);
  switch (type) {
    case 'ellipse':
    case 'flowChartConnector': {
      const rx = w / 2;
      const ry = h / 2;
      return `M${r(rx)},0A${r(rx)},${r(ry)} 0 1,1 ${r(rx)},${r(h)}A${r(rx)},${r(ry)} 0 1,1 ${r(rx)},0Z`;
    }
    case 'roundRect':
    case 'flowChartAlternateProcess': {
      const rad = m * (adjustments['adj'] ?? 0.16667);
      return rounded(w, h, rad);
    }
    case 'flowChartTerminator':
      return rounded(w, h, h / 2);
    case 'triangle':
    case 'flowChartExtract':
      return polygon([
        [w / 2, 0],
        [w, h],
        [0, h],
      ]);
    case 'rtTriangle':
      return polygon([
        [0, 0],
        [w, h],
        [0, h],
      ]);
    case 'diamond':
    case 'flowChartDecision':
      return polygon([
        [w / 2, 0],
        [w, h / 2],
        [w / 2, h],
        [0, h / 2],
      ]);
    case 'parallelogram': {
      const d = w * (adjustments['adj'] ?? 0.25);
      return polygon([
        [d, 0],
        [w, 0],
        [w - d, h],
        [0, h],
      ]);
    }
    case 'trapezoid': {
      const d = w * (adjustments['adj'] ?? 0.25);
      return polygon([
        [d, 0],
        [w - d, 0],
        [w, h],
        [0, h],
      ]);
    }
    case 'pentagon':
      return regular(5, w, h);
    case 'hexagon':
      return regular(6, w, h, 0);
    case 'octagon':
      return regular(8, w, h, Math.PI / 8);
    case 'star5': {
      const points: [number, number][] = [];
      for (let i = 0; i < 10; i++) {
        const a = -Math.PI / 2 + (i * Math.PI) / 5;
        const f = i % 2 === 0 ? 1 : 0.4;
        points.push([w / 2 + (w / 2) * f * Math.cos(a), h / 2 + (h / 2) * f * Math.sin(a)]);
      }
      return polygon(points);
    }
    case 'rightArrow': {
      const head = w * 0.35;
      const t = h * 0.25;
      return polygon([
        [0, t],
        [w - head, t],
        [w - head, 0],
        [w, h / 2],
        [w - head, h],
        [w - head, h - t],
        [0, h - t],
      ]);
    }
    case 'leftArrow': {
      const head = w * 0.35;
      const t = h * 0.25;
      return polygon([
        [w, t],
        [head, t],
        [head, 0],
        [0, h / 2],
        [head, h],
        [head, h - t],
        [w, h - t],
      ]);
    }
    case 'upArrow': {
      const head = h * 0.35;
      const t = w * 0.25;
      return polygon([
        [t, h],
        [t, head],
        [0, head],
        [w / 2, 0],
        [w, head],
        [w - t, head],
        [w - t, h],
      ]);
    }
    case 'downArrow': {
      const head = h * 0.35;
      const t = w * 0.25;
      return polygon([
        [t, 0],
        [t, h - head],
        [0, h - head],
        [w / 2, h],
        [w, h - head],
        [w - t, h - head],
        [w - t, 0],
      ]);
    }
    case 'chevron': {
      const d = w * 0.3;
      return polygon([
        [0, 0],
        [w - d, 0],
        [w, h / 2],
        [w - d, h],
        [0, h],
        [d, h / 2],
      ]);
    }
    case 'homePlate': {
      const d = w * 0.3;
      return polygon([
        [0, 0],
        [w - d, 0],
        [w, h / 2],
        [w - d, h],
        [0, h],
      ]);
    }
    case 'wedgeRectCallout': {
      const bh = h * 0.75;
      return polygon([
        [0, 0],
        [w, 0],
        [w, bh],
        [w * 0.4, bh],
        [w * 0.2, h],
        [w * 0.25, bh],
        [0, bh],
      ]);
    }
    case 'cloud': {
      const cx = w / 2;
      const cy = h / 2;
      return `M${r(w * 0.25)},${r(h * 0.75)}A${r(w * 0.2)},${r(h * 0.2)} 0 0,1 ${r(w * 0.15)},${r(h * 0.4)}A${r(w * 0.25)},${r(h * 0.3)} 0 0,1 ${r(cx)},${r(h * 0.15)}A${r(w * 0.22)},${r(h * 0.25)} 0 0,1 ${r(w * 0.85)},${r(h * 0.4)}A${r(w * 0.2)},${r(h * 0.2)} 0 0,1 ${r(w * 0.78)},${r(h * 0.78)}A${r(w * 0.3)},${r(h * 0.22)} 0 0,1 ${r(w * 0.25)},${r(h * 0.75)}Z`;
    }
    case 'heart':
      return `M${r(w / 2)},${r(h)}C${r(w * 0.1)},${r(h * 0.6)} 0,${r(h * 0.4)} 0,${r(h * 0.25)}C0,${r(h * 0.1)} ${r(w * 0.15)},0 ${r(w * 0.3)},0C${r(w * 0.4)},0 ${r(w / 2)},${r(h * 0.1)} ${r(w / 2)},${r(h * 0.2)}C${r(w / 2)},${r(h * 0.1)} ${r(w * 0.6)},0 ${r(w * 0.7)},0C${r(w * 0.85)},0 ${r(w)},${r(h * 0.1)} ${r(w)},${r(h * 0.25)}C${r(w)},${r(h * 0.4)} ${r(w * 0.9)},${r(h * 0.6)} ${r(w / 2)},${r(h)}Z`;
    case 'line':
      return `M0,0L${r(w)},${r(h)}`;
    default:
      return `M0,0H${r(w)}V${r(h)}H0Z`;
  }
}

function rounded(w: number, h: number, rad: number): string {
  const k = Math.max(0, Math.min(rad, Math.min(w, h) / 2));
  return `M${r(k)},0H${r(w - k)}Q${r(w)},0 ${r(w)},${r(k)}V${r(h - k)}Q${r(w)},${r(h)} ${r(w - k)},${r(h)}H${r(k)}Q0,${r(h)} 0,${r(h - k)}V${r(k)}Q0,0 ${r(k)},0Z`;
}

/** Whether the preset is drawn only by its outline (a connector). */
export function isLinePreset(type: string): boolean {
  return type === 'line' || type === 'straightConnector1';
}
