/**
 * Excel theme and indexed colours. Cells rarely carry a literal RGB: most fills and fonts point at
 * the workbook theme (accent1…6, text and background pairs) with a tint, or at the legacy indexed
 * palette. Resolving them here is what makes a file look like it does in Excel.
 */
export interface ExcelThemeColors {
  /** dk1, lt1, dk2, lt2, accent1..accent6, hlink, folHlink, as "#RRGGBB". */
  scheme: string[];
}

/** SpreadsheetML theme indices: 0 lt1, 1 dk1, 2 lt2, 3 dk2, 4-9 accents, 10 hyperlink, 11 followed. */
const THEME_INDEX_TO_SCHEME = [1, 0, 3, 2, 4, 5, 6, 7, 8, 9, 10, 11];
const SCHEME_NAMES = ['dk1', 'lt1', 'dk2', 'lt2', 'accent1', 'accent2', 'accent3', 'accent4', 'accent5', 'accent6', 'hlink', 'folHlink'];
const OFFICE_DEFAULT = [
  '#000000',
  '#FFFFFF',
  '#0E2841',
  '#E8E8E8',
  '#156082',
  '#E97132',
  '#196B24',
  '#0F9ED5',
  '#A02B93',
  '#4EA72E',
  '#467886',
  '#96607D',
];

/** Reads the colour scheme out of theme1.xml; the Office 2023 defaults stand in for anything missing. */
export function parseThemeColors(themeXml: string | null | undefined): ExcelThemeColors {
  const scheme = [...OFFICE_DEFAULT];
  if (!themeXml) return { scheme };
  SCHEME_NAMES.forEach((name, i) => {
    const block = new RegExp(`<a:${name}>([\\s\\S]*?)</a:${name}>`).exec(themeXml);
    if (!block) return;
    const srgb = /<a:srgbClr\s+val="([0-9A-Fa-f]{6})"/.exec(block[1]);
    const sys = /<a:sysClr\s+[^>]*lastClr="([0-9A-Fa-f]{6})"/.exec(block[1]);
    const hex = srgb?.[1] ?? sys?.[1];
    if (hex) scheme[i] = `#${hex.toUpperCase()}`;
  });
  return { scheme };
}

/** The legacy 64-colour indexed palette (ECMA-376 18.8.27), indices 0..63. */
const INDEXED = [
  '000000',
  'FFFFFF',
  'FF0000',
  '00FF00',
  '0000FF',
  'FFFF00',
  'FF00FF',
  '00FFFF',
  '000000',
  'FFFFFF',
  'FF0000',
  '00FF00',
  '0000FF',
  'FFFF00',
  'FF00FF',
  '00FFFF',
  '800000',
  '008000',
  '000080',
  '808000',
  '800080',
  '008080',
  'C0C0C0',
  '808080',
  '9999FF',
  '993366',
  'FFFFCC',
  'CCFFFF',
  '660066',
  'FF8080',
  '0066CC',
  'CCCCFF',
  '000080',
  'FF00FF',
  'FFFF00',
  '00FFFF',
  '800080',
  '800000',
  '008080',
  '0000FF',
  '00CCFF',
  'CCFFFF',
  'CCFFCC',
  'FFFF99',
  '99CCFF',
  'FF99CC',
  'CC99FF',
  'FFCC99',
  '3366FF',
  '33CCCC',
  '99CC00',
  'FFCC00',
  'FF9900',
  'FF6600',
  '666699',
  '969696',
  '003366',
  '339966',
  '003300',
  '333300',
  '993300',
  '993366',
  '333399',
  '333333',
];

export interface ExcelColorRef {
  argb?: string;
  theme?: number;
  tint?: number;
  indexed?: number;
}

/** "#RRGGBB" for any Excel colour reference, or null for "auto" and the unknown. */
export function resolveExcelColor(color: ExcelColorRef | undefined | null, theme: ExcelThemeColors): string | null {
  if (!color) return null;
  if (color.argb && color.argb.length >= 6) return applyTint(`#${color.argb.slice(-6).toUpperCase()}`, color.tint);
  if (typeof color.theme === 'number') {
    const base = theme.scheme[THEME_INDEX_TO_SCHEME[color.theme] ?? color.theme] ?? theme.scheme[0];
    return applyTint(base, color.tint);
  }
  if (typeof color.indexed === 'number') {
    // 64 and 65 are the system foreground and background.
    if (color.indexed === 64) return '#000000';
    if (color.indexed === 65) return '#FFFFFF';
    const hex = INDEXED[color.indexed];
    return hex ? `#${hex}` : null;
  }
  return null;
}

/**
 * Excel's tint: positive lightens towards white, negative darkens towards black, applied in HSL
 * on the luminance (ECMA-376 18.8.19). Lightness-only, which matches Excel closely.
 */
export function applyTint(hex: string, tint: number | undefined): string {
  if (!tint) return hex;
  const { h, s, l } = rgbToHsl(hex);
  const l2 = tint < 0 ? l * (1 + tint) : l * (1 - tint) + tint;
  return hslToRgb(h, s, Math.max(0, Math.min(1, l2)));
}

function rgbToHsl(hex: string): { h: number; s: number; l: number } {
  const n = parseInt(hex.replace('#', ''), 16);
  const r = ((n >> 16) & 255) / 255;
  const g = ((n >> 8) & 255) / 255;
  const b = (n & 255) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return { h: 0, s: 0, l };
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let h: number;
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0);
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return { h: h / 6, s, l };
}

function hslToRgb(h: number, s: number, l: number): string {
  const f = (p: number, q: number, t: number) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  let r: number, g: number, b: number;
  if (s === 0) r = g = b = l;
  else {
    const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
    const p = 2 * l - q;
    r = f(p, q, h + 1 / 3);
    g = f(p, q, h);
    b = f(p, q, h - 1 / 3);
  }
  const to = (v: number) =>
    Math.round(v * 255)
      .toString(16)
      .padStart(2, '0');
  return `#${to(r)}${to(g)}${to(b)}`.toUpperCase();
}
