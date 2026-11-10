import { NS, attr, child, children, elementChildren, escapeXml, num } from '../shared/ooxml';
import { Border, Fill, ImageElement, LineElement, Paragraph, Run, ShapeElement, SlideElement, TextBody } from './deck-model';

/**
 * PresentationML and DrawingML for one slide (ADR 0x043, office suite): patching the nodes of
 * shapes the editor changed, and writing nodes for the shapes it made. Everything the deck
 * model does not describe stays as it was in the original node: a changed text body is rebuilt
 * from the model's paragraphs with the original paragraph and run properties as templates, a
 * moved shape only gets a new transform, a recoloured one only a new fill.
 */

export const P = 'http://schemas.openxmlformats.org/presentationml/2006/main';
export const A = 'http://schemas.openxmlformats.org/drawingml/2006/main';
export const R = NS.r;
export const MC = NS.mc;

export const EMU_PER_PT = 12700;

export function emu(pt: number): number {
  return Math.round(pt * EMU_PER_PT);
}

/** What a slide needs from the package while nodes are patched: relationships for links and media. */
export interface SlideWriteContext {
  doc: Document;
  /** Adds (or reuses) an external hyperlink relationship and returns its id. */
  hyperlinkRel(url: string): string;
  /** Stores an image and returns the relationship id of the part. */
  imageRel(dataUrl: string): string | null;
  /** The next free `p:cNvPr` id on the slide. */
  nextShapeId(): number;
  warn(feature: string): void;
}

// --- node identity ------------------------------------------------------------------------------

const NV_PROPS = ['nvSpPr', 'nvPicPr', 'nvCxnSpPr', 'nvGrpSpPr', 'nvGraphicFramePr'];

/** The `p:cNvPr@id` of a shape tree child, looking through `mc:AlternateContent`. */
export function nodeId(node: Element): string | null {
  if (node.namespaceURI === MC && node.localName === 'AlternateContent') {
    for (const branch of elementChildren(node)) for (const inner of elementChildren(branch)) {
      const id = nodeId(inner);
      if (id) return id;
    }
    return null;
  }
  if (node.namespaceURI !== P) return null;
  for (const name of NV_PROPS) {
    const nv = child(node, P, name);
    const id = child(nv, P, 'cNvPr')?.getAttribute('id');
    if (id) return id;
  }
  return null;
}

/** The shape nodes a tree child stands for: itself, or every branch of an `mc:AlternateContent`. */
export function shapeNodes(node: Element): Element[] {
  if (node.namespaceURI === MC && node.localName === 'AlternateContent') {
    const out: Element[] = [];
    for (const branch of elementChildren(node)) for (const inner of elementChildren(branch)) if (inner.namespaceURI === P) out.push(inner);
    return out;
  }
  return node.namespaceURI === P ? [node] : [];
}

/** Every `p:cNvPr@id` under a node (groups carry their children's ids). */
export function allShapeIds(root: Element): number[] {
  return Array.from(root.getElementsByTagNameNS(P, 'cNvPr'))
    .map(el => num(el.getAttribute('id')))
    .filter((n): n is number => n !== undefined);
}

// --- children in schema order -------------------------------------------------------------------

const SP_PR_ORDER = ['xfrm', 'custGeom', 'prstGeom', 'noFill', 'solidFill', 'gradFill', 'blipFill', 'pattFill', 'grpFill', 'ln', 'effectLst', 'effectDag', 'scene3d', 'sp3d', 'extLst'];
const R_PR_ORDER = ['ln', 'noFill', 'solidFill', 'gradFill', 'blipFill', 'pattFill', 'grpFill', 'effectLst', 'effectDag', 'highlight', 'uLnTx', 'uLn', 'uFillTx', 'uFill', 'latin', 'ea', 'cs', 'sym', 'hlinkClick', 'hlinkMouseOver', 'rtl', 'extLst'];
const P_PR_ORDER = ['lnSpc', 'spcBef', 'spcAft', 'buClrTx', 'buClr', 'buSzTx', 'buSzPct', 'buSzPts', 'buFontTx', 'buFont', 'buNone', 'buAutoNum', 'buChar', 'buBlip', 'tabLst', 'defRPr', 'extLst'];
const LN_ORDER = ['noFill', 'solidFill', 'gradFill', 'pattFill', 'prstDash', 'custDash', 'round', 'bevel', 'miter', 'headEnd', 'tailEnd', 'extLst'];
const FILLS = ['noFill', 'solidFill', 'gradFill', 'blipFill', 'pattFill', 'grpFill'];
export const TC_PR_ORDER = ['lnL', 'lnR', 'lnT', 'lnB', 'lnTlToBr', 'lnBlToTr', 'cell3D', ...FILLS, 'headers', 'extLst'];

/** Puts an element's DrawingML children back into schema order; unknown ones keep their place at the end. */
function sortChildren(el: Element, order: string[]): void {
  const kids = elementChildren(el);
  const rank = (k: Element) => {
    const i = k.namespaceURI === A ? order.indexOf(k.localName) : -1;
    return i === -1 ? order.length : i;
  };
  const sorted = kids.map((k, i) => ({ k, i })).sort((x, y) => rank(x.k) - rank(y.k) || x.i - y.i);
  for (const { k } of sorted) el.appendChild(k);
}

function removeChildren(el: Element, names: string[]): void {
  for (const k of elementChildren(el)) if (k.namespaceURI === A && names.includes(k.localName)) el.removeChild(k);
}

function ensureChild(parent: Element, ns: string, qualified: string, localName: string): Element {
  return child(parent, ns, localName) ?? parent.appendChild(parent.ownerDocument.createElementNS(ns, qualified));
}

function setOrRemove(el: Element, name: string, value: string | number | null | undefined): void {
  if (value === null || value === undefined || value === '') el.removeAttribute(name);
  else el.setAttribute(name, String(value));
}

export function rgb(color: string): string {
  return color.replace('#', '').toUpperCase();
}

function solidFill(doc: Document, color: string, opacity?: number): Element {
  const fill = doc.createElementNS(A, 'a:solidFill');
  const clr = fill.appendChild(doc.createElementNS(A, 'a:srgbClr'));
  clr.setAttribute('val', rgb(color));
  if (opacity !== undefined && opacity < 1) {
    const alpha = clr.appendChild(doc.createElementNS(A, 'a:alpha'));
    alpha.setAttribute('val', String(Math.round(Math.max(0, opacity) * 100000)));
  }
  return fill;
}

// --- transform, fill, line ----------------------------------------------------------------------

/** The element that holds the shape's transform: `p:spPr`, `p:grpSpPr`, or the graphic frame itself (`p:xfrm`). */
function transformHolder(node: Element): { holder: Element; ns: string; qualified: string } | null {
  switch (node.localName) {
    case 'sp':
    case 'pic':
    case 'cxnSp': {
      const spPr = ensureChild(node, P, 'p:spPr', 'spPr');
      return { holder: spPr, ns: A, qualified: 'a:xfrm' };
    }
    case 'grpSp':
      return { holder: ensureChild(node, P, 'p:grpSpPr', 'grpSpPr'), ns: A, qualified: 'a:xfrm' };
    case 'graphicFrame':
      return { holder: node, ns: P, qualified: 'p:xfrm' };
    default:
      return null;
  }
}

/** Sets position, size, rotation and flips; a shape that inherited its transform from a placeholder gets one. */
export function patchTransform(node: Element, el: SlideElement): void {
  const target = transformHolder(node);
  if (!target) return;
  const { holder, ns, qualified } = target;
  const doc = node.ownerDocument;
  let xfrm = child(holder, ns, 'xfrm');
  if (!xfrm) {
    xfrm = doc.createElementNS(ns, qualified);
    if (node.localName === 'graphicFrame') {
      // p:xfrm follows p:nvGraphicFramePr and precedes a:graphic.
      const graphic = child(node, A, 'graphic');
      node.insertBefore(xfrm, graphic);
    } else holder.insertBefore(xfrm, holder.firstChild);
  }
  const off = ensureChild(xfrm, A, 'a:off', 'off');
  const ext = ensureChild(xfrm, A, 'a:ext', 'ext');
  off.setAttribute('x', String(emu(el.left)));
  off.setAttribute('y', String(emu(el.top)));
  ext.setAttribute('cx', String(emu(el.width)));
  ext.setAttribute('cy', String(emu(el.height)));
  // off and ext lead; a group's chOff/chExt follow.
  xfrm.insertBefore(off, xfrm.firstChild);
  xfrm.insertBefore(ext, off.nextSibling);
  setOrRemove(xfrm, 'rot', el.rotate ? Math.round(el.rotate * 60000) : null);
  setOrRemove(xfrm, 'flipH', el.flipH ? '1' : null);
  setOrRemove(xfrm, 'flipV', el.flipV ? '1' : null);
  if (holder !== node) sortChildren(holder, SP_PR_ORDER);
}

/** Replaces a shape's fill; gradient and picture fills are not written here, so they stay as they were. */
export function patchFill(spPr: Element, fill: Fill | null, ctx: SlideWriteContext, order = SP_PR_ORDER): void {
  if (fill && fill.type !== 'color') {
    ctx.warn('Gradient and picture fills are kept as they were');
    return;
  }
  removeChildren(spPr, FILLS);
  spPr.appendChild(fill ? solidFill(ctx.doc, fill.color, fill.opacity) : ctx.doc.createElementNS(A, 'a:noFill'));
  sortChildren(spPr, order);
}

const DASH: Record<Border['style'], string | null> = { solid: null, dashed: 'dash', dotted: 'sysDot' };

/** Replaces a shape's outline; null draws none. */
export function patchLine(spPr: Element, border: Border | null, ctx: SlideWriteContext): void {
  const doc = ctx.doc;
  const ln = ensureChild(spPr, A, 'a:ln', 'ln');
  removeChildren(ln, FILLS);
  if (!border) {
    ln.appendChild(doc.createElementNS(A, 'a:noFill'));
    removeChildren(ln, ['prstDash']);
  } else {
    ln.setAttribute('w', String(emu(border.width)));
    ln.appendChild(solidFill(doc, border.color));
    removeChildren(ln, ['prstDash', 'custDash']);
    const dash = DASH[border.style];
    if (dash) {
      const prstDash = ln.appendChild(doc.createElementNS(A, 'a:prstDash'));
      prstDash.setAttribute('val', dash);
    }
  }
  sortChildren(ln, LN_ORDER);
  sortChildren(spPr, SP_PR_ORDER);
}

/** The slide background: a colour replaces whatever was there; other fills stay. */
export function patchBackground(cSld: Element, fill: Fill | null, ctx: SlideWriteContext): void {
  const doc = ctx.doc;
  if (fill && fill.type !== 'color') {
    ctx.warn('Gradient and picture backgrounds are kept as they were');
    return;
  }
  const old = child(cSld, P, 'bg');
  if (old) cSld.removeChild(old);
  if (!fill) return;
  const bg = doc.createElementNS(P, 'p:bg');
  const bgPr = bg.appendChild(doc.createElementNS(P, 'p:bgPr'));
  bgPr.appendChild(solidFill(doc, fill.color, fill.opacity));
  bgPr.appendChild(doc.createElementNS(A, 'a:effectLst'));
  cSld.insertBefore(bg, cSld.firstChild);
}

// --- text ---------------------------------------------------------------------------------------

const ALIGN: Record<Paragraph['align'], string> = { left: 'l', center: 'ctr', right: 'r', justify: 'just' };
const ANCHOR: Record<TextBody['vAlign'], string> = { top: 't', middle: 'ctr', bottom: 'b' };

/**
 * Rebuilds the paragraphs of a text body from the model. The original paragraphs are templates:
 * paragraph i keeps the properties of original paragraph i (or the last), run j of it the
 * properties of original run j (or the last, or the paragraph's end-of-paragraph properties),
 * and only what the model describes is set on top. `imported` is the paragraphs as they were
 * read from the file, so paragraph-level properties are only touched where they changed.
 */
export function patchParagraphs(txBody: Element, paragraphs: Paragraph[], imported: Paragraph[] | undefined, ctx: SlideWriteContext): void {
  const doc = ctx.doc;
  const oldPs = children(txBody, A, 'p');
  const fallbackRPr = txBody.getElementsByTagNameNS(A, 'rPr')[0] ?? txBody.getElementsByTagNameNS(A, 'endParaRPr')[0] ?? null;
  for (const p of oldPs) txBody.removeChild(p);
  const list = paragraphs.length ? paragraphs : [{ align: 'left' as const, runs: [] }];
  list.forEach((para, i) => {
    const template = oldPs[Math.min(i, oldPs.length - 1)] ?? null;
    const was = imported?.[i];
    const p = doc.createElementNS(A, 'a:p');
    const pPr = paragraphProperties(doc, child(template, A, 'pPr'), para, was, imported !== undefined && i < (imported?.length ?? 0));
    if (pPr) p.appendChild(pPr);
    const templateRuns = template ? children(template, A, 'r') : [];
    const endRPr = child(template, A, 'endParaRPr');
    para.runs.forEach((run, j) => {
      const base = child(templateRuns[Math.min(j, templateRuns.length - 1)] ?? null, A, 'rPr') ?? endRPr ?? fallbackRPr;
      const pieces = run.text.split('\n');
      pieces.forEach((piece, k) => {
        if (k > 0) {
          const br = p.appendChild(doc.createElementNS(A, 'a:br'));
          br.appendChild(runProperties(doc, base, run, ctx));
        }
        if (piece === '' && pieces.length > 1) return;
        const r = p.appendChild(doc.createElementNS(A, 'a:r'));
        r.appendChild(runProperties(doc, base, run, ctx));
        r.appendChild(doc.createElementNS(A, 'a:t')).textContent = piece;
      });
    });
    if (endRPr) p.appendChild(endRPr.cloneNode(true));
    txBody.appendChild(p);
  });
}

function paragraphProperties(
  doc: Document,
  template: Element | null,
  para: Paragraph,
  was: Paragraph | undefined,
  hadParagraph: boolean,
): Element | null {
  const pPr = template ? (template.cloneNode(true) as Element) : doc.createElementNS(A, 'a:pPr');
  pPr.setAttribute('algn', ALIGN[para.align]);
  setOrRemove(pPr, 'lvl', para.bullet?.level ? para.bullet.level : null);
  const changed = <K extends keyof Paragraph>(key: K) => !was || JSON.stringify(para[key]) !== JSON.stringify(was[key]);
  if (changed('indent')) setOrRemove(pPr, 'marL', para.indent ? emu(para.indent) : null);
  if (changed('spaceBefore')) spacing(doc, pPr, 'spcBef', para.spaceBefore);
  if (changed('spaceAfter')) spacing(doc, pPr, 'spcAft', para.spaceAfter);
  if (changed('lineSpacing')) {
    removeChildren(pPr, ['lnSpc']);
    if (para.lineSpacing) {
      const pct = pPr.appendChild(doc.createElementNS(A, 'a:lnSpc')).appendChild(doc.createElementNS(A, 'a:spcPct'));
      pct.setAttribute('val', String(Math.round(para.lineSpacing * 100000)));
    }
  }
  const BULLETS = ['buNone', 'buAutoNum', 'buChar', 'buBlip'];
  const hasOwnBullet = elementChildren(pPr).some(k => k.namespaceURI === A && ['buAutoNum', 'buChar', 'buBlip'].includes(k.localName));
  if (para.bullet) {
    removeChildren(pPr, ['buNone']);
    const typeChanged = was?.bullet && was.bullet.type !== para.bullet.type;
    if (!hasOwnBullet || typeChanged) {
      // A bullet the paragraph inherits from its placeholder stays inherited; a new one is written.
      if (!was?.bullet || typeChanged || !hadParagraph) {
        removeChildren(pPr, BULLETS);
        if (para.bullet.type === 'number') {
          const bu = pPr.appendChild(doc.createElementNS(A, 'a:buAutoNum'));
          bu.setAttribute('type', para.bullet.numberType || 'arabicPeriod');
        } else {
          const bu = pPr.appendChild(doc.createElementNS(A, 'a:buChar'));
          bu.setAttribute('char', para.bullet.char || '•');
        }
        if (!pPr.hasAttribute('marL')) pPr.setAttribute('marL', '342900');
        if (!pPr.hasAttribute('indent')) pPr.setAttribute('indent', '-342900');
      }
    }
  } else if (hasOwnBullet || was?.bullet) {
    removeChildren(pPr, BULLETS);
    pPr.appendChild(doc.createElementNS(A, 'a:buNone'));
  }
  sortChildren(pPr, P_PR_ORDER);
  return pPr.attributes.length || pPr.childNodes.length ? pPr : null;
}

function spacing(doc: Document, pPr: Element, name: 'spcBef' | 'spcAft', points: number | undefined): void {
  removeChildren(pPr, [name]);
  if (!points) return;
  const pts = pPr.appendChild(doc.createElementNS(A, `a:${name}`)).appendChild(doc.createElementNS(A, 'a:spcPts'));
  pts.setAttribute('val', String(Math.round(points * 100)));
}

/** Run properties from a template with the model run's formatting set on top. */
export function runProperties(doc: Document, template: Element | null, run: Run, ctx: SlideWriteContext): Element {
  const rPr = template ? (template.cloneNode(true) as Element) : doc.createElementNS(A, 'a:rPr');
  if (rPr.localName !== 'rPr') {
    // An endParaRPr template becomes a run's rPr.
    const fresh = doc.createElementNS(A, 'a:rPr');
    for (const a of Array.from(rPr.attributes)) fresh.setAttribute(a.name, a.value);
    for (const k of Array.from(rPr.childNodes)) fresh.appendChild(k.cloneNode(true));
    return runProperties(doc, fresh, run, ctx);
  }
  if (!rPr.hasAttribute('lang')) rPr.setAttribute('lang', 'en-US');
  rPr.removeAttribute('dirty');
  rPr.removeAttribute('err');
  setOrRemove(rPr, 'b', run.bold ? '1' : null);
  setOrRemove(rPr, 'i', run.italic ? '1' : null);
  setOrRemove(rPr, 'u', run.underline ? 'sng' : null);
  setOrRemove(rPr, 'strike', run.strike ? 'sngStrike' : null);
  setOrRemove(rPr, 'sz', run.fontSize ? Math.round(run.fontSize * 100) : null);
  setOrRemove(rPr, 'baseline', run.baseline === 'super' ? '30000' : run.baseline === 'sub' ? '-25000' : null);
  if (run.color) {
    removeChildren(rPr, FILLS);
    rPr.appendChild(solidFill(doc, run.color));
  }
  removeChildren(rPr, ['highlight']);
  if (run.highlight) rPr.appendChild(doc.createElementNS(A, 'a:highlight')).appendChild(solidFill(doc, run.highlight).firstChild!);
  if (run.fontFace) {
    removeChildren(rPr, ['latin']);
    const latin = rPr.appendChild(doc.createElementNS(A, 'a:latin'));
    latin.setAttribute('typeface', run.fontFace);
  }
  removeChildren(rPr, ['hlinkClick']);
  if (run.link) {
    const link = rPr.appendChild(doc.createElementNS(A, 'a:hlinkClick'));
    link.setAttributeNS(R, 'r:id', ctx.hyperlinkRel(run.link));
  }
  sortChildren(rPr, R_PR_ORDER);
  return rPr;
}

/** Body properties the editor can change: anchor, insets, wrap and autofit; each only when it differs from the import. */
export function patchBodyProperties(txBody: Element, body: TextBody, was: TextBody | undefined): void {
  const doc = txBody.ownerDocument;
  const bodyPr = ensureChild(txBody, A, 'a:bodyPr', 'bodyPr');
  txBody.insertBefore(bodyPr, txBody.firstChild);
  if (!was || was.vAlign !== body.vAlign) bodyPr.setAttribute('anchor', ANCHOR[body.vAlign]);
  if (!was || was.wrap !== body.wrap) bodyPr.setAttribute('wrap', body.wrap ? 'square' : 'none');
  if (!was || JSON.stringify(was.inset) !== JSON.stringify(body.inset)) {
    const [l, t, r, b] = body.inset;
    bodyPr.setAttribute('lIns', String(emu(l)));
    bodyPr.setAttribute('tIns', String(emu(t)));
    bodyPr.setAttribute('rIns', String(emu(r)));
    bodyPr.setAttribute('bIns', String(emu(b)));
  }
  if (!was || was.autoFit !== body.autoFit) {
    removeChildren(bodyPr, ['noAutofit', 'normAutofit', 'spAutoFit']);
    if (body.autoFit === 'resize') bodyPr.appendChild(doc.createElementNS(A, 'a:spAutoFit'));
    else if (body.autoFit === 'shrink') bodyPr.appendChild(doc.createElementNS(A, 'a:normAutofit'));
  }
  if (!child(txBody, A, 'lstStyle')) txBody.insertBefore(doc.createElementNS(A, 'a:lstStyle'), bodyPr.nextSibling);
}

/** The text body of a shape, created after `p:spPr` and `p:style` when the shape had none. */
export function ensureTextBody(sp: Element): Element {
  const existing = child(sp, P, 'txBody');
  if (existing) return existing;
  const doc = sp.ownerDocument;
  const txBody = doc.createElementNS(P, 'p:txBody');
  const after = child(sp, P, 'style') ?? child(sp, P, 'spPr');
  sp.insertBefore(txBody, after ? after.nextSibling : null);
  return txBody;
}

// --- new shapes ---------------------------------------------------------------------------------

const DECLARATIONS = `xmlns:a="${A}" xmlns:p="${P}" xmlns:r="${R}"`;

function parseFragment(doc: Document, xml: string): Element {
  const parsed = new DOMParser().parseFromString(xml, 'application/xml');
  const error = parsed.getElementsByTagName('parsererror')[0];
  if (error) throw new Error(`Invalid shape XML: ${error.textContent?.slice(0, 200)}`);
  return doc.importNode(parsed.documentElement, true);
}

function xfrmXml(el: { left: number; top: number; width: number; height: number; rotate: number; flipH: boolean; flipV: boolean }): string {
  const rot = el.rotate ? ` rot="${Math.round(el.rotate * 60000)}"` : '';
  const flip = `${el.flipH ? ' flipH="1"' : ''}${el.flipV ? ' flipV="1"' : ''}`;
  return `<a:xfrm${rot}${flip}><a:off x="${emu(el.left)}" y="${emu(el.top)}"/><a:ext cx="${emu(el.width)}" cy="${emu(el.height)}"/></a:xfrm>`;
}

function geometryXml(el: ShapeElement): string {
  const preset = el.shapeType && el.shapeType !== 'custom' ? el.shapeType : 'rect';
  const guides = Object.entries(el.adjustments ?? {})
    .map(([name, value]) => `<a:gd name="${escapeXml(name)}" fmla="val ${Math.round(value * 100000)}"/>`)
    .join('');
  return `<a:prstGeom prst="${escapeXml(preset)}"><a:avLst>${guides}</a:avLst></a:prstGeom>`;
}

/** A new `p:sp`: a text box or a preset shape with its fill, outline and text. */
export function newShapeNode(el: ShapeElement, ctx: SlideWriteContext): Element {
  const id = ctx.nextShapeId();
  const link = el.link ? `<a:hlinkClick r:id="${ctx.hyperlinkRel(el.link)}"/>` : '';
  const xml =
    `<p:sp ${DECLARATIONS}><p:nvSpPr><p:cNvPr id="${id}" name="${escapeXml(el.name || `Shape ${id}`)}">${link}</p:cNvPr>` +
    `<p:cNvSpPr${el.isTextBox ? ' txBox="1"' : ''}/><p:nvPr/></p:nvSpPr>` +
    `<p:spPr>${xfrmXml(el)}${geometryXml(el)}</p:spPr></p:sp>`;
  const sp = parseFragment(ctx.doc, xml);
  const spPr = child(sp, P, 'spPr')!;
  patchFill(spPr, el.fill, ctx);
  patchLine(spPr, el.border, ctx);
  if (el.text) {
    const txBody = ensureTextBody(sp);
    patchBodyProperties(txBody, el.text, undefined);
    patchParagraphs(txBody, el.text.paragraphs, undefined, ctx);
  }
  return sp;
}

/** A new `p:pic` over a media part written through the context. */
export function newPictureNode(el: ImageElement, ctx: SlideWriteContext): Element | null {
  const relId = ctx.imageRel(el.src);
  if (!relId) return null;
  const id = ctx.nextShapeId();
  const crop = el.crop
    ? `<a:srcRect l="${pct(el.crop.left)}" t="${pct(el.crop.top)}" r="${pct(el.crop.right)}" b="${pct(el.crop.bottom)}"/>`
    : '';
  const link = el.link ? `<a:hlinkClick r:id="${ctx.hyperlinkRel(el.link)}"/>` : '';
  const geometry = el.geometry && el.geometry !== 'rect' ? el.geometry : 'rect';
  const xml =
    `<p:pic ${DECLARATIONS}><p:nvPicPr><p:cNvPr id="${id}" name="${escapeXml(el.name || `Picture ${id}`)}">${link}</p:cNvPr>` +
    `<p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr>` +
    `<p:blipFill><a:blip r:embed="${relId}"/>${crop}<a:stretch><a:fillRect/></a:stretch></p:blipFill>` +
    `<p:spPr>${xfrmXml(el)}<a:prstGeom prst="${escapeXml(geometry)}"><a:avLst/></a:prstGeom></p:spPr></p:pic>`;
  const pic = parseFragment(ctx.doc, xml);
  if (el.border) patchLine(child(pic, P, 'spPr')!, el.border, ctx);
  return pic;
}

function pct(fraction: number): number {
  return Math.round(fraction * 100000);
}

/** A new connector for a line, with its arrow ends. */
export function newLineNode(el: LineElement, ctx: SlideWriteContext): Element {
  const id = ctx.nextShapeId();
  const xml =
    `<p:cxnSp ${DECLARATIONS}><p:nvCxnSpPr><p:cNvPr id="${id}" name="${escapeXml(el.name || `Connector ${id}`)}"/><p:cNvCxnSpPr/><p:nvPr/></p:nvCxnSpPr>` +
    `<p:spPr>${xfrmXml(el)}<a:prstGeom prst="line"><a:avLst/></a:prstGeom></p:spPr></p:cxnSp>`;
  const cxn = parseFragment(ctx.doc, xml);
  const spPr = child(cxn, P, 'spPr')!;
  patchLine(spPr, el.border, ctx);
  const ln = child(spPr, A, 'ln')!;
  for (const [tag, end] of [
    ['headEnd', el.headEnd],
    ['tailEnd', el.tailEnd],
  ] as const) {
    if (end.type === 'none') continue;
    const node = ln.appendChild(ctx.doc.createElementNS(A, `a:${tag}`));
    node.setAttribute('type', end.type);
  }
  sortChildren(ln, LN_ORDER);
  return cxn;
}

/** Plain-text paragraphs for speaker notes: one `a:p` per line, the first original paragraph as template. */
export function patchNotesText(txBody: Element, notes: string, ctx: SlideWriteContext): void {
  const paragraphs: Paragraph[] = notes.split(/\r?\n/).map(line => ({ align: 'left', runs: line ? [{ text: line }] : [] }));
  patchParagraphs(txBody, paragraphs, undefined, ctx);
  // Notes keep their placeholder's alignment; the model has none to set.
  for (const pPr of Array.from(txBody.getElementsByTagNameNS(A, 'pPr'))) pPr.removeAttribute('algn');
}
