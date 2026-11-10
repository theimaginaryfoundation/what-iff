import type PptxGenJS from 'pptxgenjs';

import { Paragraph, Run, TextBody, emptyTextBody } from './deck-model';

/**
 * Rich text in and out: pptxtojson hands text over as HTML (p/span with inline styles, ul/ol/li
 * for bullets, a for links); pptxgenjs takes it as runs with options. The deck model sits between.
 */

export function htmlToParagraphs(html: string): Paragraph[] {
  if (!html) return [];
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html');
  const out: Paragraph[] = [];
  walk(doc.body, out, undefined, 0);
  return out;
}

function walk(node: Node, out: Paragraph[], bullet: Paragraph['bullet'] | undefined, level: number): void {
  for (const child of Array.from(node.childNodes)) {
    if (child.nodeType !== 1) {
      const text = child.textContent ?? '';
      if (text.trim()) out.push({ align: 'left', runs: [{ text: text.replace(/ /g, ' ') }] });
      continue;
    }
    const el = child as HTMLElement;
    const tag = el.tagName.toLowerCase();
    if (tag === 'ul' || tag === 'ol') {
      walk(el, out, { type: tag === 'ol' ? 'number' : 'bullet', level }, level + 1);
    } else if (tag === 'li') {
      const hasParagraph = Array.from(el.children).some(c => c.tagName.toLowerCase() === 'p');
      if (hasParagraph) walk(el, out, bullet, level);
      else out.push(paragraphFrom(el, bullet));
    } else if (tag === 'p' || tag === 'div') {
      out.push(paragraphFrom(el, bullet));
    } else {
      walk(el, out, bullet, level);
    }
  }
}

function paragraphFrom(el: HTMLElement, bullet: Paragraph['bullet'] | undefined): Paragraph {
  const style = el.style;
  const paragraph: Paragraph = { align: alignOf(style.textAlign), runs: [] };
  if (bullet) paragraph.bullet = { ...bullet };
  const indent = pt(style.marginLeft);
  if (indent) paragraph.indent = indent;
  const before = pt(style.marginTop);
  if (before) paragraph.spaceBefore = before;
  const after = pt(style.marginBottom);
  if (after) paragraph.spaceAfter = after;
  const lh = style.lineHeight;
  if (lh && !lh.endsWith('pt') && !lh.endsWith('px') && Number(lh)) paragraph.lineSpacing = Number(lh);
  collectRuns(el, {}, paragraph.runs);
  paragraph.runs = mergeRuns(paragraph.runs);
  return paragraph;
}

function collectRuns(node: Node, inherited: Omit<Run, 'text'>, out: Run[]): void {
  for (const child of Array.from(node.childNodes)) {
    if (child.nodeType === 3) {
      const text = (child.textContent ?? '').replace(/ /g, ' ');
      if (text) out.push({ ...inherited, text });
      continue;
    }
    if (child.nodeType !== 1) continue;
    const el = child as HTMLElement;
    const tag = el.tagName.toLowerCase();
    if (tag === 'br') {
      out.push({ ...inherited, text: '\n' });
      continue;
    }
    const props: Omit<Run, 'text'> = { ...inherited, ...runStyle(el) };
    if (tag === 'a') {
      const href = el.getAttribute('href');
      if (href) props.link = href;
    }
    if (tag === 'b' || tag === 'strong') props.bold = true;
    if (tag === 'i' || tag === 'em') props.italic = true;
    if (tag === 'u') props.underline = true;
    if (tag === 's' || tag === 'strike' || tag === 'del') props.strike = true;
    if (tag === 'sup') props.baseline = 'super';
    if (tag === 'sub') props.baseline = 'sub';
    collectRuns(el, props, out);
  }
}

function runStyle(el: HTMLElement): Omit<Run, 'text'> {
  const s = el.style;
  const out: Omit<Run, 'text'> = {};
  const color = hex(s.color);
  if (color) out.color = color;
  const size = pt(s.fontSize);
  if (size) out.fontSize = size;
  const family = s.fontFamily?.replace(/["']/g, '').split(',')[0]?.trim();
  if (family) out.fontFace = family;
  if (s.fontWeight === 'bold' || Number(s.fontWeight) >= 600) out.bold = true;
  if (s.fontStyle === 'italic') out.italic = true;
  const decoration = `${s.textDecoration} ${s.textDecorationLine}`;
  if (decoration.includes('underline')) out.underline = true;
  if (decoration.includes('line-through')) out.strike = true;
  const bg = hex(s.backgroundColor);
  if (bg) out.highlight = bg;
  if (s.verticalAlign === 'super') out.baseline = 'super';
  if (s.verticalAlign === 'sub') out.baseline = 'sub';
  return out;
}

function mergeRuns(runs: Run[]): Run[] {
  const out: Run[] = [];
  for (const run of runs) {
    const last = out[out.length - 1];
    if (last && sameRunStyle(last, run)) last.text += run.text;
    else out.push({ ...run });
  }
  return out;
}

function sameRunStyle(a: Run, b: Run): boolean {
  const { text: _a, ...sa } = a;
  const { text: _b, ...sb } = b;
  void _a;
  void _b;
  return JSON.stringify(sa, Object.keys(sa).sort()) === JSON.stringify(sb, Object.keys(sb).sort());
}

function alignOf(value: string | undefined): Paragraph['align'] {
  switch (value) {
    case 'center':
      return 'center';
    case 'right':
    case 'end':
      return 'right';
    case 'justify':
      return 'justify';
    default:
      return 'left';
  }
}

function pt(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const m = /^(-?[\d.]+)(pt|px)?$/.exec(value.trim());
  if (!m) return undefined;
  const n = Number(m[1]);
  if (!Number.isFinite(n)) return undefined;
  return m[2] === 'px' ? Math.round(((n * 72) / 96) * 100) / 100 : n;
}

/** "#RRGGBB" from a CSS colour: hex, or rgb(). */
export function hex(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const v = value.trim();
  const m6 = /^#([0-9a-fA-F]{6})$/.exec(v);
  if (m6) return `#${m6[1].toUpperCase()}`;
  const m3 = /^#([0-9a-fA-F]{3})$/.exec(v);
  if (m3)
    return `#${m3[1]
      .split('')
      .map(c => c + c)
      .join('')
      .toUpperCase()}`;
  const rgb = /^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/.exec(v);
  if (rgb)
    return `#${[rgb[1], rgb[2], rgb[3]]
      .map(n => Number(n).toString(16).padStart(2, '0'))
      .join('')
      .toUpperCase()}`;
  return undefined;
}

/** Plain text from pptxtojson's note HTML. */
export function htmlToPlainText(html: string): string {
  return htmlToParagraphs(html)
    .map(p => p.runs.map(r => r.text).join(''))
    .join('\n')
    .trim();
}

export function textBodyFromHtml(
  html: string,
  vAlign: string | undefined,
  inset?: { l: number; t: number; r: number; b: number },
  wrap = true,
  autoFit?: { type: 'shape' | 'text' },
): TextBody {
  const body = emptyTextBody(htmlToParagraphs(html));
  body.vAlign =
    vAlign === 'mid' || vAlign === 'middle' || vAlign === 'ctr'
      ? 'middle'
      : vAlign === 'down' || vAlign === 'bottom' || vAlign === 'b'
        ? 'bottom'
        : 'top';
  if (inset) body.inset = [inset.l, inset.t, inset.r, inset.b];
  body.wrap = wrap;
  body.autoFit = autoFit?.type === 'shape' ? 'resize' : autoFit?.type === 'text' ? 'shrink' : 'none';
  return body;
}

// --- to pptxgenjs ------------------------------------------------------------------------------

type GenRun = PptxGenJS.TextProps;

const NUMBER_TYPES = new Set([
  'alphaLcParenBoth',
  'alphaLcParenR',
  'alphaLcPeriod',
  'alphaUcParenBoth',
  'alphaUcParenR',
  'alphaUcPeriod',
  'arabicParenBoth',
  'arabicParenR',
  'arabicPeriod',
  'arabicPlain',
  'romanLcParenBoth',
  'romanLcParenR',
  'romanLcPeriod',
  'romanUcParenBoth',
  'romanUcParenR',
  'romanUcPeriod',
]);

/** Paragraphs as pptxgenjs runs; paragraph options ride on each run and the last run breaks the line. */
export function paragraphsToRuns(paragraphs: Paragraph[]): GenRun[] {
  const out: GenRun[] = [];
  paragraphs.forEach((p, pi) => {
    const paragraphOptions: PptxGenJS.TextPropsOptions = { align: p.align };
    if (p.bullet) {
      if (p.bullet.type === 'number')
        paragraphOptions.bullet = {
          type: 'number',
          numberType: (NUMBER_TYPES.has(p.bullet.numberType ?? '') ? p.bullet.numberType : 'arabicPeriod') as never,
        };
      else
        paragraphOptions.bullet = p.bullet.char
          ? { characterCode: p.bullet.char.codePointAt(0)!.toString(16).toUpperCase().padStart(4, '0') }
          : true;
      if (p.bullet.level) paragraphOptions.indentLevel = p.bullet.level;
    }
    if (p.spaceBefore) paragraphOptions.paraSpaceBefore = p.spaceBefore;
    if (p.spaceAfter) paragraphOptions.paraSpaceAfter = p.spaceAfter;
    if (p.lineSpacing) paragraphOptions.lineSpacingMultiple = p.lineSpacing;
    const runs = p.runs.length ? p.runs : [{ text: '' }];
    runs.forEach((r, ri) => {
      const pieces = r.text.split('\n');
      pieces.forEach((piece, k) => {
        const options: PptxGenJS.TextPropsOptions = { ...paragraphOptions };
        if (r.bold) options.bold = true;
        if (r.italic) options.italic = true;
        if (r.underline) options.underline = { style: 'sng' };
        if (r.strike) options.strike = 'sngStrike';
        if (r.color) options.color = r.color.replace('#', '');
        if (r.fontSize) options.fontSize = r.fontSize;
        if (r.fontFace) options.fontFace = r.fontFace;
        if (r.highlight) options.highlight = r.highlight.replace('#', '');
        if (r.baseline === 'super') options.superscript = true;
        if (r.baseline === 'sub') options.subscript = true;
        if (r.link) options.hyperlink = { url: r.link };
        const isLast = ri === runs.length - 1 && k === pieces.length - 1;
        if (k < pieces.length - 1) options.softBreakBefore = false;
        if (isLast && pi < paragraphs.length - 1) options.breakLine = true;
        if (k > 0) options.softBreakBefore = true;
        out.push({ text: piece, options });
      });
    });
  });
  return out;
}

export function runsPlainText(runs: GenRun[]): string {
  return runs.map(r => r.text ?? '').join('');
}
