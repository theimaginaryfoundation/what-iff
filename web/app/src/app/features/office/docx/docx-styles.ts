import {
  NS,
  attr,
  child,
  children,
  colorHex,
  eighthsToPx,
  escapeXml,
  halfPointsToPt,
  hexColor,
  highlightColor,
  highlightName,
  num,
  ptToHalfPoints,
  serializeXml,
  pxToEighths,
  pxToTwips,
  toggle,
  twipsToPx,
  wval,
} from '../shared/ooxml';
import { UNDERLINE_DOUBLE, UNDERLINE_SINGLE } from '../xlsx/xlsx-styles';
import {
  DOC_HALIGN,
  DocList,
  DocListLevel,
  DocNamedStyle,
  DocParagraphBorder,
  DocParagraphStyle,
  DocTextStyle,
  GLYPH,
  NAMED_STYLE,
  NumberUnit,
} from './univer-doc-types';

/**
 * WordprocessingML run and paragraph properties to and from Univer's text and paragraph styles,
 * and the styles and numbering parts. Lengths become 96-dpi pixels, font sizes points.
 */

// --- run properties -----------------------------------------------------------------------------

const UNDERLINE_TO_UNIVER: Record<string, number> = {
  single: UNDERLINE_SINGLE,
  double: UNDERLINE_DOUBLE,
  words: 17,
  dotted: 8,
  dash: 0,
  dotDash: 6,
  dotDotDash: 7,
  wave: 14,
  thick: 13,
  dashLong: 4,
  dashedHeavy: 3,
  dottedHeavy: 9,
  wavyDouble: 15,
  wavyHeavy: 16,
  dashDotHeavy: 2,
  dashDotDotHeavy: 1,
  dashLongHeavy: 5,
};
const UNDERLINE_FROM_UNIVER: Record<number, string> = Object.fromEntries(Object.entries(UNDERLINE_TO_UNIVER).map(([k, v]) => [v, k]));

export function textStyleFromRPr(rPr: Element | null | undefined): DocTextStyle | undefined {
  if (!rPr) return undefined;
  const out: DocTextStyle = {};
  const fonts = child(rPr, NS.w, 'rFonts');
  const ff = attr(fonts, NS.w, 'ascii') ?? attr(fonts, NS.w, 'hAnsi') ?? attr(fonts, NS.w, 'cs');
  if (ff) out.ff = ff;
  const sz = num(wval(child(rPr, NS.w, 'sz')));
  if (sz !== undefined) out.fs = halfPointsToPt(sz);
  const b = toggle(child(rPr, NS.w, 'b'));
  if (b !== undefined) out.bl = b ? 1 : 0;
  const i = toggle(child(rPr, NS.w, 'i'));
  if (i !== undefined) out.it = i ? 1 : 0;
  const u = wval(child(rPr, NS.w, 'u'));
  if (u !== null) out.ul = u === 'none' ? { s: 0 } : { s: 1, t: UNDERLINE_TO_UNIVER[u] ?? UNDERLINE_SINGLE };
  const strike = toggle(child(rPr, NS.w, 'strike')) ?? toggle(child(rPr, NS.w, 'dstrike'));
  if (strike !== undefined) out.st = { s: strike ? 1 : 0 };
  const color = hexColor(wval(child(rPr, NS.w, 'color')));
  if (color) out.cl = { rgb: color };
  const highlight = highlightColor(wval(child(rPr, NS.w, 'highlight')));
  const shd = child(rPr, NS.w, 'shd');
  const bg = highlight ?? hexColor(attr(shd, NS.w, 'fill'));
  if (bg) out.bg = { rgb: bg };
  const va = wval(child(rPr, NS.w, 'vertAlign'));
  if (va === 'superscript') out.va = 3;
  else if (va === 'subscript') out.va = 2;
  else if (va === 'baseline') out.va = 1;
  const caps = toggle(child(rPr, NS.w, 'caps'));
  if (caps !== undefined) out.caps = caps;
  const smallCaps = toggle(child(rPr, NS.w, 'smallCaps'));
  if (smallCaps !== undefined) out.smallCaps = smallCaps;
  const vanish = toggle(child(rPr, NS.w, 'vanish'));
  if (vanish !== undefined) out.hidden = vanish;
  const spacing = num(wval(child(rPr, NS.w, 'spacing')));
  if (spacing !== undefined) out.sc = spacing / 20;
  return Object.keys(out).length ? out : undefined;
}

export function rPrXml(ts: DocTextStyle | undefined, styleId?: string): string {
  const parts: string[] = [];
  if (styleId) parts.push(`<w:rStyle w:val="${escapeXml(styleId)}"/>`);
  if (!ts && !styleId) return '';
  if (ts) {
    if (ts.ff) parts.push(`<w:rFonts w:ascii="${escapeXml(ts.ff)}" w:hAnsi="${escapeXml(ts.ff)}" w:cs="${escapeXml(ts.ff)}"/>`);
    if (ts.bl !== undefined) parts.push(ts.bl ? '<w:b/><w:bCs/>' : '<w:b w:val="0"/><w:bCs w:val="0"/>');
    if (ts.it !== undefined) parts.push(ts.it ? '<w:i/><w:iCs/>' : '<w:i w:val="0"/><w:iCs w:val="0"/>');
    if (ts.caps !== undefined) parts.push(ts.caps ? '<w:caps/>' : '<w:caps w:val="0"/>');
    if (ts.smallCaps !== undefined) parts.push(ts.smallCaps ? '<w:smallCaps/>' : '<w:smallCaps w:val="0"/>');
    if (ts.st) parts.push(ts.st.s ? '<w:strike/>' : '<w:strike w:val="0"/>');
    if (ts.hidden !== undefined) parts.push(ts.hidden ? '<w:vanish/>' : '<w:vanish w:val="0"/>');
    const cl = colorHex(ts.cl?.rgb);
    if (cl) parts.push(`<w:color w:val="${cl}"/>`);
    if (ts.sc !== undefined) parts.push(`<w:spacing w:val="${Math.round(ts.sc * 20)}"/>`);
    if (ts.fs !== undefined) parts.push(`<w:sz w:val="${ptToHalfPoints(ts.fs)}"/><w:szCs w:val="${ptToHalfPoints(ts.fs)}"/>`);
    const bg = ts.bg?.rgb ?? null;
    if (bg) {
      const name = highlightName(bg);
      parts.push(name ? `<w:highlight w:val="${name}"/>` : `<w:shd w:val="clear" w:color="auto" w:fill="${colorHex(bg)}"/>`);
    }
    if (ts.ul) parts.push(`<w:u w:val="${ts.ul.s ? (UNDERLINE_FROM_UNIVER[ts.ul.t ?? UNDERLINE_SINGLE] ?? 'single') : 'none'}"/>`);
    if (ts.va === 3) parts.push('<w:vertAlign w:val="superscript"/>');
    else if (ts.va === 2) parts.push('<w:vertAlign w:val="subscript"/>');
    else if (ts.va === 1) parts.push('<w:vertAlign w:val="baseline"/>');
  }
  return parts.length ? `<w:rPr>${parts.join('')}</w:rPr>` : '';
}

// --- paragraph properties -----------------------------------------------------------------------

const JC_TO_UNIVER: Record<string, number> = {
  left: DOC_HALIGN.left,
  start: DOC_HALIGN.left,
  center: DOC_HALIGN.center,
  right: DOC_HALIGN.right,
  end: DOC_HALIGN.right,
  both: DOC_HALIGN.justified,
  distribute: DOC_HALIGN.distributed,
};
const JC_FROM_UNIVER: Record<number, string> = {
  [DOC_HALIGN.left]: 'left',
  [DOC_HALIGN.center]: 'center',
  [DOC_HALIGN.right]: 'right',
  [DOC_HALIGN.justified]: 'both',
  5: 'both',
  [DOC_HALIGN.distributed]: 'distribute',
};

const BORDER_DASH: Record<string, number> = {
  single: 1,
  thick: 1,
  double: 1,
  dotted: 2,
  dashed: 3,
  dashSmallGap: 3,
  dotDash: 3,
  dotDotDash: 3,
};

function paragraphBorder(el: Element | null): DocParagraphBorder | undefined {
  if (!el) return undefined;
  const val = wval(el);
  if (!val || val === 'nil' || val === 'none') return undefined;
  return {
    color: { rgb: hexColor(attr(el, NS.w, 'color')) ?? '#000000' },
    width: eighthsToPx(num(attr(el, NS.w, 'sz')) ?? 4),
    dashStyle: BORDER_DASH[val] ?? 1,
    padding: num(attr(el, NS.w, 'space')) ?? 1,
  };
}

function borderXml(name: string, b: DocParagraphBorder | undefined): string {
  if (!b) return '';
  const val = b.dashStyle === 2 ? 'dotted' : b.dashStyle === 3 ? 'dashed' : 'single';
  return `<w:${name} w:val="${val}" w:sz="${pxToEighths(b.width)}" w:space="${Math.round(b.padding)}" w:color="${colorHex(b.color.rgb) ?? 'auto'}"/>`;
}

export interface ParagraphProps {
  styleId?: string;
  style?: DocParagraphStyle;
  numbering?: { numId: string; level: number };
  /** The paragraph mark's own run properties (w:pPr/w:rPr). */
  markTextStyle?: DocTextStyle;
  sectPr?: Element | null;
}

export function paragraphPropsFromPPr(pPr: Element | null | undefined): ParagraphProps {
  const out: ParagraphProps = {};
  if (!pPr) return out;
  const style: DocParagraphStyle = {};
  const styleId = wval(child(pPr, NS.w, 'pStyle'));
  if (styleId) out.styleId = styleId;
  const jc = wval(child(pPr, NS.w, 'jc'));
  if (jc && JC_TO_UNIVER[jc]) style.horizontalAlign = JC_TO_UNIVER[jc];
  const spacing = child(pPr, NS.w, 'spacing');
  if (spacing) {
    const before = num(attr(spacing, NS.w, 'before'));
    const after = num(attr(spacing, NS.w, 'after'));
    if (before !== undefined) style.spaceAbove = { v: twipsToPx(before) };
    if (after !== undefined) style.spaceBelow = { v: twipsToPx(after) };
    const line = num(attr(spacing, NS.w, 'line'));
    const rule = attr(spacing, NS.w, 'lineRule') ?? 'auto';
    if (line !== undefined) {
      if (rule === 'auto') {
        style.lineSpacing = Math.round((line / 240) * 100) / 100;
        style.spacingRule = 0;
      } else {
        style.lineSpacing = twipsToPx(line);
        style.spacingRule = rule === 'exact' ? 2 : 1;
      }
    }
  }
  const ind = child(pPr, NS.w, 'ind');
  if (ind) {
    const left = num(attr(ind, NS.w, 'left') ?? attr(ind, NS.w, 'start'));
    const right = num(attr(ind, NS.w, 'right') ?? attr(ind, NS.w, 'end'));
    const first = num(attr(ind, NS.w, 'firstLine'));
    const hanging = num(attr(ind, NS.w, 'hanging'));
    if (left !== undefined) style.indentStart = { v: twipsToPx(left) };
    if (right !== undefined) style.indentEnd = { v: twipsToPx(right) };
    if (first !== undefined) style.indentFirstLine = { v: twipsToPx(first) };
    if (hanging !== undefined) style.hanging = { v: twipsToPx(hanging) };
  }
  const flag = (name: keyof DocParagraphStyle & ('keepLines' | 'keepNext' | 'pageBreakBefore' | 'widowControl' | 'contextualSpacing')) => {
    const t = toggle(child(pPr, NS.w, name));
    if (t !== undefined) style[name] = t ? 1 : 0;
  };
  flag('keepLines');
  flag('keepNext');
  flag('pageBreakBefore');
  flag('widowControl');
  flag('contextualSpacing');
  const outline = num(wval(child(pPr, NS.w, 'outlineLvl')));
  if (outline !== undefined) style.outlineLevel = outline;
  const shd = hexColor(attr(child(pPr, NS.w, 'shd'), NS.w, 'fill'));
  if (shd) style.shading = { backgroundColor: { rgb: shd } };
  const pBdr = child(pPr, NS.w, 'pBdr');
  if (pBdr) {
    style.borderTop = paragraphBorder(child(pBdr, NS.w, 'top'));
    style.borderBottom = paragraphBorder(child(pBdr, NS.w, 'bottom'));
    style.borderLeft = paragraphBorder(child(pBdr, NS.w, 'left'));
    style.borderRight = paragraphBorder(child(pBdr, NS.w, 'right'));
    for (const k of ['borderTop', 'borderBottom', 'borderLeft', 'borderRight'] as const) if (!style[k]) delete style[k];
  }
  const bidi = toggle(child(pPr, NS.w, 'bidi'));
  if (bidi) style.direction = 2;
  const numPr = child(pPr, NS.w, 'numPr');
  if (numPr) {
    const numId = wval(child(numPr, NS.w, 'numId'));
    const level = num(wval(child(numPr, NS.w, 'ilvl'))) ?? 0;
    if (numId && numId !== '0') out.numbering = { numId, level };
  }
  const markTs = textStyleFromRPr(child(pPr, NS.w, 'rPr'));
  if (markTs) out.markTextStyle = markTs;
  out.sectPr = child(pPr, NS.w, 'sectPr');
  if (Object.keys(style).length) out.style = style;
  return out;
}

export interface PPrXmlOptions {
  styleId?: string;
  numbering?: { numId: string; level: number };
  markTextStyle?: DocTextStyle;
  sectPrXml?: string;
  /** Properties kept from the original (see DocParagraph.docxPPr), merged in schema order. */
  extras?: string;
}

/** CT_PPr child order; Word rejects properties out of order. */
const PPR_ORDER = [
  'pStyle',
  'keepNext',
  'keepLines',
  'pageBreakBefore',
  'framePr',
  'widowControl',
  'numPr',
  'suppressLineNumbers',
  'pBdr',
  'shd',
  'tabs',
  'suppressAutoHyphens',
  'kinsoku',
  'wordWrap',
  'overflowPunct',
  'topLinePunct',
  'autoSpaceDE',
  'autoSpaceDN',
  'bidi',
  'adjustRightInd',
  'snapToGrid',
  'spacing',
  'ind',
  'contextualSpacing',
  'mirrorIndents',
  'suppressOverlap',
  'jc',
  'textDirection',
  'textAlignment',
  'textboxTightWrap',
  'outlineLvl',
  'divId',
  'cnfStyle',
  'rPr',
  'sectPr',
  'pPrChange',
];
export const TCPR_ORDER = [
  'cnfStyle',
  'tcW',
  'gridSpan',
  'hMerge',
  'vMerge',
  'tcBorders',
  'shd',
  'noWrap',
  'tcMar',
  'textDirection',
  'tcFitText',
  'vAlign',
  'hideMark',
  'headers',
  'cellIns',
  'cellDel',
  'cellMerge',
  'tcPrChange',
];
export const TRPR_ORDER = [
  'cnfStyle',
  'divId',
  'gridBefore',
  'gridAfter',
  'wBefore',
  'wAfter',
  'cantSplit',
  'trHeight',
  'tblHeader',
  'tblCellSpacing',
  'jc',
  'hidden',
  'ins',
  'del',
  'trPrChange',
];
export const SECTPR_ORDER = [
  'headerReference',
  'footerReference',
  'footnotePr',
  'endnotePr',
  'type',
  'pgSz',
  'pgMar',
  'paperSrc',
  'pgBorders',
  'lnNumType',
  'pgNumType',
  'cols',
  'formProt',
  'vAlign',
  'noEndnote',
  'titlePg',
  'textDirection',
  'bidi',
  'rtlGutter',
  'docGrid',
  'printerSettings',
  'sectPrChange',
];

/** The namespaces every part we write declares on its root; fragments need not repeat them. */
const ROOT_NAMESPACES: [string, string][] = [
  ['w', NS.w],
  ['r', NS.r],
  ['wp', NS.wp],
  ['a', NS.a],
  ['pic', NS.pic],
  ['mc', NS.mc],
  ['v', NS.v],
];

/** Drops xmlns declarations the serialiser adds for prefixes the part's root already declares. */
export function stripRootNamespaces(xml: string): string {
  let out = xml;
  for (const [prefix, uri] of ROOT_NAMESPACES) out = out.split(` xmlns:${prefix}="${uri}"`).join('');
  return out;
}

/** Top-level elements of a serialised fragment, by local name, so kept properties can be slotted into schema order. */
export function splitExtras(xml: string | undefined): { name: string; xml: string }[] {
  if (!xml) return [];
  const doc = new DOMParser().parseFromString(
    `<x xmlns:w="${NS.w}" xmlns:r="${NS.r}" xmlns:mc="${NS.mc}" xmlns:v="${NS.v}">${xml}</x>`,
    'application/xml',
  );
  if (doc.getElementsByTagName('parsererror').length) return [];
  return Array.from(doc.documentElement.childNodes)
    .filter((n): n is Element => n.nodeType === 1)
    .map(el => ({ name: el.localName, xml: stripRootNamespaces(serializeXml(el)) }));
}

/** Property children in CT order: the ones we build, then the kept extras, each where the schema puts it. */
export function orderedProperties(built: { name: string; xml: string }[], extras: string | undefined, order: string[]): string {
  const all = [...built, ...splitExtras(extras)];
  const rank = (name: string) => {
    const i = order.indexOf(name);
    return i === -1 ? order.length : i;
  };
  return all
    .map((p, i) => ({ ...p, i }))
    .sort((a, b) => rank(a.name) - rank(b.name) || a.i - b.i)
    .map(p => p.xml)
    .join('');
}

export function pPrXml(style: DocParagraphStyle | undefined, opts: PPrXmlOptions = {}): string {
  const parts: { name: string; xml: string }[] = [];
  const add = (name: string, xml: string) => parts.push({ name, xml });
  if (opts.styleId) add('pStyle', `<w:pStyle w:val="${escapeXml(opts.styleId)}"/>`);
  const s = style ?? {};
  if (s.keepNext !== undefined) add('keepNext', s.keepNext ? '<w:keepNext/>' : '<w:keepNext w:val="0"/>');
  if (s.keepLines !== undefined) add('keepLines', s.keepLines ? '<w:keepLines/>' : '<w:keepLines w:val="0"/>');
  if (s.pageBreakBefore !== undefined)
    add('pageBreakBefore', s.pageBreakBefore ? '<w:pageBreakBefore/>' : '<w:pageBreakBefore w:val="0"/>');
  if (s.widowControl !== undefined) add('widowControl', s.widowControl ? '<w:widowControl/>' : '<w:widowControl w:val="0"/>');
  if (opts.numbering)
    add('numPr', `<w:numPr><w:ilvl w:val="${opts.numbering.level}"/><w:numId w:val="${escapeXml(opts.numbering.numId)}"/></w:numPr>`);
  const borders = [
    borderXml('top', s.borderTop),
    borderXml('left', s.borderLeft),
    borderXml('bottom', s.borderBottom),
    borderXml('right', s.borderRight),
  ].join('');
  if (borders) add('pBdr', `<w:pBdr>${borders}</w:pBdr>`);
  const shd = colorHex(s.shading?.backgroundColor.rgb);
  if (shd) add('shd', `<w:shd w:val="clear" w:color="auto" w:fill="${shd}"/>`);
  const spacing: string[] = [];
  if (s.spaceAbove) spacing.push(`w:before="${pxToTwips(s.spaceAbove.v)}"`);
  if (s.spaceBelow) spacing.push(`w:after="${pxToTwips(s.spaceBelow.v)}"`);
  if (s.lineSpacing !== undefined) {
    if (!s.spacingRule) spacing.push(`w:line="${Math.round(s.lineSpacing * 240)}" w:lineRule="auto"`);
    else spacing.push(`w:line="${pxToTwips(s.lineSpacing)}" w:lineRule="${s.spacingRule === 2 ? 'exact' : 'atLeast'}"`);
  }
  if (spacing.length) add('spacing', `<w:spacing ${spacing.join(' ')}/>`);
  if (s.contextualSpacing !== undefined)
    add('contextualSpacing', s.contextualSpacing ? '<w:contextualSpacing/>' : '<w:contextualSpacing w:val="0"/>');
  const ind: string[] = [];
  if (s.indentStart) ind.push(`w:left="${pxToTwips(s.indentStart.v)}"`);
  if (s.indentEnd) ind.push(`w:right="${pxToTwips(s.indentEnd.v)}"`);
  if (s.indentFirstLine) ind.push(`w:firstLine="${pxToTwips(s.indentFirstLine.v)}"`);
  if (s.hanging) ind.push(`w:hanging="${pxToTwips(s.hanging.v)}"`);
  if (ind.length) add('ind', `<w:ind ${ind.join(' ')}/>`);
  if (s.direction === 2) add('bidi', '<w:bidi/>');
  const jc = s.horizontalAlign !== undefined ? JC_FROM_UNIVER[s.horizontalAlign] : undefined;
  if (jc) add('jc', `<w:jc w:val="${jc}"/>`);
  if (s.outlineLevel !== undefined) add('outlineLvl', `<w:outlineLvl w:val="${s.outlineLevel}"/>`);
  const mark = rPrXml(opts.markTextStyle);
  if (mark) add('rPr', mark);
  if (opts.sectPrXml) add('sectPr', opts.sectPrXml);
  if (!parts.length && !opts.extras) return '';
  return `<w:pPr>${orderedProperties(parts, opts.extras, PPR_ORDER)}</w:pPr>`;
}

// --- styles part ---------------------------------------------------------------------------------

export interface ParsedStyles {
  styles: Record<string, DocNamedStyle>;
  defaultTextStyle?: DocTextStyle;
  defaultParagraphStyle?: DocParagraphStyle;
  /** Word's default paragraph style id (w:default="1"), usually "Normal". */
  defaultParagraphStyleId?: string;
}

const NAMED_BY_ID: Record<string, number> = {
  Title: NAMED_STYLE.title,
  Subtitle: NAMED_STYLE.subtitle,
  Heading1: NAMED_STYLE.heading1,
  Heading2: NAMED_STYLE.heading2,
  Heading3: NAMED_STYLE.heading3,
  Heading4: NAMED_STYLE.heading4,
  Heading5: NAMED_STYLE.heading5,
};

export function namedStyleTypeFor(styleId: string | undefined, name: string | undefined): number | undefined {
  if (styleId && NAMED_BY_ID[styleId]) return NAMED_BY_ID[styleId];
  const n = name?.toLowerCase().replace(/\s+/g, '');
  if (n === 'title') return NAMED_STYLE.title;
  if (n === 'subtitle') return NAMED_STYLE.subtitle;
  const m = n ? /^heading([1-5])$/.exec(n) : null;
  return m ? NAMED_STYLE.heading1 + Number(m[1]) - 1 : undefined;
}

export function parseStyles(doc: Document | null): ParsedStyles {
  const out: ParsedStyles = { styles: {} };
  if (!doc) return out;
  const root = doc.documentElement;
  const defaults = child(root, NS.w, 'docDefaults');
  const rPrDefault = child(child(defaults, NS.w, 'rPrDefault'), NS.w, 'rPr');
  const pPrDefault = child(child(defaults, NS.w, 'pPrDefault'), NS.w, 'pPr');
  out.defaultTextStyle = textStyleFromRPr(rPrDefault);
  out.defaultParagraphStyle = paragraphPropsFromPPr(pPrDefault).style;
  for (const el of children(root, NS.w, 'style')) {
    const type = attr(el, NS.w, 'type');
    if (type !== 'paragraph' && type !== 'character') continue;
    const id = attr(el, NS.w, 'styleId');
    if (!id) continue;
    const name = wval(child(el, NS.w, 'name')) ?? id;
    const style: DocNamedStyle = { name, type: type === 'paragraph' ? 1 : 0 };
    const basedOn = wval(child(el, NS.w, 'basedOn'));
    if (basedOn) style.basedOn = basedOn;
    const link = wval(child(el, NS.w, 'link'));
    if (link) style.link = link;
    const ts = textStyleFromRPr(child(el, NS.w, 'rPr'));
    if (ts) style.textStyle = ts;
    if (type === 'paragraph') {
      const ps = paragraphPropsFromPPr(child(el, NS.w, 'pPr')).style ?? {};
      const named = namedStyleTypeFor(id, name);
      if (named !== undefined) ps.namedStyleType = named;
      if (Object.keys(ps).length) style.paragraphStyle = ps;
      if (attr(el, NS.w, 'default') === '1' || attr(el, NS.w, 'default') === 'true') out.defaultParagraphStyleId = id;
    }
    out.styles[id] = style;
  }
  return out;
}

/** The text style a named style resolves to, walking basedOn, under the document default. */
export function resolveTextStyle(
  styles: Record<string, DocNamedStyle> | undefined,
  styleId: string | undefined,
  seen = new Set<string>(),
): DocTextStyle {
  if (!styles || !styleId || seen.has(styleId)) return {};
  const style = styles[styleId];
  if (!style) return {};
  seen.add(styleId);
  return { ...resolveTextStyle(styles, style.basedOn, seen), ...(style.textStyle ?? {}) };
}

/** The keys of `ts` whose values differ from `base`. */
export function diffTextStyle(ts: DocTextStyle | undefined, base: DocTextStyle | undefined): DocTextStyle | undefined {
  if (!ts) return undefined;
  const out: DocTextStyle = {};
  for (const [k, v] of Object.entries(ts) as [keyof DocTextStyle, unknown][]) {
    if (v === undefined) continue;
    const b = base?.[k];
    if (JSON.stringify(v) !== JSON.stringify(b)) (out as Record<string, unknown>)[k] = v;
  }
  return Object.keys(out).length ? out : undefined;
}

export function stylesXml(
  styles: Record<string, DocNamedStyle>,
  defaultTextStyle: DocTextStyle | undefined,
  defaultParagraphStyle: DocParagraphStyle | undefined,
): string {
  const entries = Object.entries(styles);
  const body = entries
    .map(([id, s]) => {
      const parts = [`<w:name w:val="${escapeXml(s.name)}"/>`];
      if (s.basedOn) parts.push(`<w:basedOn w:val="${escapeXml(s.basedOn)}"/>`);
      if (s.link) parts.push(`<w:link w:val="${escapeXml(s.link)}"/>`);
      parts.push('<w:qFormat/>');
      if (s.type === 1) {
        const ps = s.paragraphStyle ? { ...s.paragraphStyle } : undefined;
        if (ps) delete ps.namedStyleType;
        parts.push(pPrXml(ps));
      }
      parts.push(rPrXml(s.textStyle));
      const isDefault = id === 'Normal' ? ' w:default="1"' : '';
      return `<w:style w:type="${s.type === 1 ? 'paragraph' : 'character'}" w:styleId="${escapeXml(id)}"${isDefault}>${parts.join('')}</w:style>`;
    })
    .join('');
  return (
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
    `<w:styles xmlns:w="${NS.w}" xmlns:r="${NS.r}">` +
    `<w:docDefaults><w:rPrDefault>${rPrXml(defaultTextStyle) || '<w:rPr/>'}</w:rPrDefault><w:pPrDefault>${pPrXml(defaultParagraphStyle) || '<w:pPr/>'}</w:pPrDefault></w:docDefaults>` +
    body +
    `</w:styles>`
  );
}

/** Styles every exported document should have, filled in only where the document has none. */
export function defaultNamedStyles(): Record<string, DocNamedStyle> {
  return {
    Normal: { name: 'Normal', type: 1 },
    Title: {
      name: 'Title',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 26, bl: 1 },
      paragraphStyle: { namedStyleType: NAMED_STYLE.title, spaceBelow: { v: 12 } },
    },
    Subtitle: {
      name: 'Subtitle',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 14, cl: { rgb: '#595959' } },
      paragraphStyle: { namedStyleType: NAMED_STYLE.subtitle, spaceBelow: { v: 12 } },
    },
    Heading1: {
      name: 'heading 1',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 20, bl: 1 },
      paragraphStyle: { namedStyleType: NAMED_STYLE.heading1, spaceAbove: { v: 20 }, spaceBelow: { v: 10 }, keepNext: 1, outlineLevel: 0 },
    },
    Heading2: {
      name: 'heading 2',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 18, bl: 1 },
      paragraphStyle: { namedStyleType: NAMED_STYLE.heading2, spaceAbove: { v: 18 }, spaceBelow: { v: 10 }, keepNext: 1, outlineLevel: 1 },
    },
    Heading3: {
      name: 'heading 3',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 16, bl: 1 },
      paragraphStyle: { namedStyleType: NAMED_STYLE.heading3, spaceAbove: { v: 16 }, spaceBelow: { v: 10 }, keepNext: 1, outlineLevel: 2 },
    },
    Heading4: {
      name: 'heading 4',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 14, bl: 1 },
      paragraphStyle: { namedStyleType: NAMED_STYLE.heading4, spaceAbove: { v: 14 }, spaceBelow: { v: 8 }, keepNext: 1, outlineLevel: 3 },
    },
    Heading5: {
      name: 'heading 5',
      type: 1,
      basedOn: 'Normal',
      textStyle: { fs: 12, bl: 1 },
      paragraphStyle: { namedStyleType: NAMED_STYLE.heading5, spaceAbove: { v: 12 }, spaceBelow: { v: 8 }, keepNext: 1, outlineLevel: 4 },
    },
    ListParagraph: { name: 'List Paragraph', type: 1, basedOn: 'Normal', paragraphStyle: { contextualSpacing: 1 } },
    Hyperlink: { name: 'Hyperlink', type: 0, textStyle: { cl: { rgb: '#0563C1' }, ul: { s: 1, t: UNDERLINE_SINGLE } } },
  };
}

// --- numbering part ------------------------------------------------------------------------------

const NUMFMT_TO_GLYPH: Record<string, number> = {
  bullet: GLYPH.bullet,
  none: GLYPH.none,
  decimal: GLYPH.decimal,
  decimalZero: GLYPH.decimalZero,
  upperLetter: GLYPH.upperLetter,
  lowerLetter: GLYPH.lowerLetter,
  upperRoman: GLYPH.upperRoman,
  lowerRoman: GLYPH.lowerRoman,
};
const GLYPH_TO_NUMFMT: Record<number, string> = Object.fromEntries(Object.entries(NUMFMT_TO_GLYPH).map(([k, v]) => [v, k]));

export interface ParsedNumbering {
  /** Univer lists keyed by list type ("num" + numId). */
  lists: Record<string, DocList>;
}

export function listTypeForNum(numId: string): string {
  return `num${numId}`;
}

export function parseNumbering(doc: Document | null): ParsedNumbering {
  const lists: Record<string, DocList> = {};
  if (!doc) return { lists };
  const root = doc.documentElement;
  const abstracts = new Map<string, DocListLevel[]>();
  for (const abs of children(root, NS.w, 'abstractNum')) {
    const id = attr(abs, NS.w, 'abstractNumId');
    if (!id) continue;
    const levels: DocListLevel[] = [];
    for (const lvl of children(abs, NS.w, 'lvl')) {
      const ilvl = num(attr(lvl, NS.w, 'ilvl')) ?? levels.length;
      const fmt = wval(child(lvl, NS.w, 'numFmt')) ?? 'decimal';
      const text = wval(child(lvl, NS.w, 'lvlText')) ?? '';
      const start = num(wval(child(lvl, NS.w, 'start'))) ?? 1;
      const jc = wval(child(lvl, NS.w, 'lvlJc'));
      const glyphType = NUMFMT_TO_GLYPH[fmt] ?? GLYPH.decimal;
      const level: DocListLevel = {
        bulletAlignment: jc === 'center' ? 2 : jc === 'right' || jc === 'end' ? 3 : 1,
        glyphFormat: glyphType === GLYPH.bullet ? ` %${ilvl + 1}` : text || `%${ilvl + 1}.`,
        glyphType,
        startNumber: glyphType === GLYPH.bullet ? 0 : start,
      };
      if (glyphType === GLYPH.bullet) level.glyphSymbol = text || '•';
      const pPr = paragraphPropsFromPPr(child(lvl, NS.w, 'pPr')).style;
      level.paragraphProperties = pPr ?? { indentStart: { v: 48 * (ilvl + 1) }, hanging: { v: 24 } };
      const ts = textStyleFromRPr(child(lvl, NS.w, 'rPr'));
      if (ts) level.textStyle = ts;
      levels[ilvl] = level;
    }
    abstracts.set(id, levels.filter(Boolean));
  }
  for (const n of children(root, NS.w, 'num')) {
    const numId = attr(n, NS.w, 'numId');
    const absId = wval(child(n, NS.w, 'abstractNumId'));
    if (!numId || !absId) continue;
    const levels = (abstracts.get(absId) ?? []).map(l => ({ ...l }));
    for (const override of children(n, NS.w, 'lvlOverride')) {
      const ilvl = num(attr(override, NS.w, 'ilvl'));
      const start = num(wval(child(override, NS.w, 'startOverride')));
      if (ilvl !== undefined && start !== undefined && levels[ilvl]) levels[ilvl] = { ...levels[ilvl], startNumber: start };
    }
    if (!levels.length) continue;
    const listType = listTypeForNum(numId);
    lists[listType] = { listType, nestingLevel: levels };
  }
  return { lists };
}

export interface NumberingXmlOptions {
  /** First num and abstractNum ids to use, when appending to an existing part. */
  firstNumId?: number;
  firstAbstractId?: number;
  /** Only the definitions, without the part wrapper, for appending. */
  fragment?: boolean;
}

export function numberingXml(
  lists: Record<string, DocList>,
  opts: NumberingXmlOptions = {},
): { xml: string; numIdFor: (listType: string) => string } | null {
  const types = Object.keys(lists);
  if (!types.length) return null;
  const numIds = new Map<string, string>();
  const firstNum = opts.firstNumId ?? 1;
  const firstAbstract = opts.firstAbstractId ?? 0;
  types.forEach((t, i) => numIds.set(t, String(firstNum + i)));
  const abstracts = types
    .map((t, i0) => {
      const i = firstAbstract + i0;
      const list = lists[t];
      const levels = list.nestingLevel
        .slice(0, 9)
        .map((level, ilvl) => {
          const glyphType = level.glyphType ?? (level.glyphSymbol ? GLYPH.bullet : GLYPH.decimal);
          const fmt = GLYPH_TO_NUMFMT[glyphType] ?? 'decimal';
          const text = glyphType === GLYPH.bullet ? (level.glyphSymbol ?? '•') : level.glyphFormat.trim() || `%${ilvl + 1}.`;
          const jc = level.bulletAlignment === 2 ? 'center' : level.bulletAlignment === 3 ? 'right' : 'left';
          const props = level.paragraphProperties ?? { indentStart: { v: 48 * (ilvl + 1) }, hanging: { v: 24 } };
          return (
            `<w:lvl w:ilvl="${ilvl}"><w:start w:val="${Math.max(1, level.startNumber || 1)}"/><w:numFmt w:val="${fmt}"/>` +
            `<w:lvlText w:val="${escapeXml(text)}"/><w:lvlJc w:val="${jc}"/>${pPrXml(props)}${rPrXml(level.textStyle)}</w:lvl>`
          );
        })
        .join('');
      return `<w:abstractNum w:abstractNumId="${i}"><w:multiLevelType w:val="hybridMultilevel"/>${levels}</w:abstractNum>`;
    })
    .join('');
  const nums = types.map((t, i) => `<w:num w:numId="${numIds.get(t)}"><w:abstractNumId w:val="${firstAbstract + i}"/></w:num>`).join('');
  return {
    xml: opts.fragment
      ? `${abstracts}${nums}`
      : `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="${NS.w}" xmlns:r="${NS.r}">${abstracts}${nums}</w:numbering>`,
    numIdFor: (listType: string) => numIds.get(listType) ?? String(firstNum),
  };
}

export function numberUnitPx(unit: NumberUnit | undefined): number {
  return unit?.v ?? 0;
}
