import { safeLinkUrl } from '../shared/safe-link';
import { Paragraph, Run } from '../pptx/deck-model';
import { htmlToParagraphs } from '../pptx/pptx-text';

/**
 * Rich text on the slide canvas: paragraphs rendered as HTML the browser can edit in place, and
 * read back with the same parser that reads pptxtojson's HTML, so what the user typed and what a
 * .pptx held travel the same road. Font sizes are points; the slide is laid out in points too.
 */

export function paragraphsToHtml(paragraphs: Paragraph[]): string {
  if (!paragraphs.length) return '<p></p>';
  const out: string[] = [];
  let openList: { type: 'bullet' | 'number'; level: number }[] = [];
  const closeLists = (toLevel: number) => {
    while (openList.length > toLevel) {
      const list = openList.pop()!;
      out.push(list.type === 'number' ? '</ol>' : '</ul>');
    }
  };
  for (const p of paragraphs) {
    if (p.bullet) {
      const level = p.bullet.level + 1;
      if (openList.length > level || (openList.length === level && openList[level - 1].type !== p.bullet.type)) closeLists(level - 1);
      while (openList.length < level) {
        const type = openList.length === level - 1 ? p.bullet.type : 'bullet';
        openList.push({ type, level: openList.length });
        out.push(type === 'number' ? '<ol>' : '<ul>');
      }
      out.push(`<li>${paragraphHtml(p)}</li>`);
    } else {
      closeLists(0);
      out.push(paragraphHtml(p));
    }
  }
  closeLists(0);
  return out.join('');
}

function paragraphHtml(p: Paragraph): string {
  const style: string[] = [];
  if (p.align && p.align !== 'left') style.push(`text-align: ${p.align}`);
  if (p.indent) style.push(`margin-left: ${p.indent}pt`);
  if (p.spaceBefore) style.push(`margin-top: ${p.spaceBefore}pt`);
  if (p.spaceAfter) style.push(`margin-bottom: ${p.spaceAfter}pt`);
  if (p.lineSpacing) style.push(`line-height: ${p.lineSpacing}`);
  const runs = p.runs.map(runHtml).join('');
  return `<p${style.length ? ` style="${style.join(';')}"` : ''}>${runs || '<br>'}</p>`;
}

function runHtml(run: Run): string {
  const style: string[] = [];
  if (run.color) style.push(`color: ${run.color}`);
  if (run.fontSize) style.push(`font-size: ${run.fontSize}pt`);
  if (run.fontFace) style.push(`font-family: ${run.fontFace}`);
  if (run.bold) style.push('font-weight: bold');
  if (run.italic) style.push('font-style: italic');
  const decoration = [run.underline ? 'underline' : '', run.strike ? 'line-through' : ''].filter(Boolean).join(' ');
  if (decoration) style.push(`text-decoration: ${decoration}`);
  if (run.highlight) style.push(`background-color: ${run.highlight}`);
  if (run.baseline === 'super') style.push('vertical-align: super;font-size: smaller');
  if (run.baseline === 'sub') style.push('vertical-align: sub;font-size: smaller');
  const text = escapeHtml(run.text).replace(/\n/g, '<br>');
  const span = style.length ? `<span style="${style.join(';')}">${text}</span>` : `<span>${text}</span>`;
  const link = safeLinkUrl(run.link);
  return link ? `<a href="${escapeHtml(link)}">${span}</a>` : span;
}

export function escapeHtml(text: string): string {
  return text.replace(/[&<>"]/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[ch] as string);
}

/**
 * The paragraphs an edited element now holds. Browsers wrap edits in b/i/u, font and div tags;
 * those are normalised to the span-with-style dialect first.
 */
export function domToParagraphs(root: HTMLElement): Paragraph[] {
  const clone = root.cloneNode(true) as HTMLElement;
  normalise(clone);
  return htmlToParagraphs(clone.innerHTML);
}

function normalise(root: HTMLElement): void {
  for (const font of Array.from(root.querySelectorAll('font'))) {
    const span = root.ownerDocument.createElement('span');
    const color = font.getAttribute('color');
    const size = font.getAttribute('size');
    if (color) span.style.color = color;
    if (size) span.style.fontSize = `${FONT_SIZES[Number(size)] ?? 12}pt`;
    span.append(...Array.from(font.childNodes));
    font.replaceWith(span);
  }
  for (const div of Array.from(root.querySelectorAll('div'))) {
    const p = root.ownerDocument.createElement('p');
    p.setAttribute('style', div.getAttribute('style') ?? '');
    p.append(...Array.from(div.childNodes));
    div.replaceWith(p);
  }
  // A paragraph the browser left as bare text at the root becomes a paragraph of its own.
  const loose = Array.from(root.childNodes).filter(n => n.nodeType === 3 && n.textContent?.trim());
  for (const node of loose) {
    const p = root.ownerDocument.createElement('p');
    node.replaceWith(p);
    p.append(node);
  }
}

/** The legacy font size scale 1..7 the font tag uses, in points. */
const FONT_SIZES: Record<number, number> = { 1: 8, 2: 10, 3: 12, 4: 14, 5: 18, 6: 24, 7: 36 };

/** Keyword sizes execCommand uses with styleWithCSS, in points. */
export function keywordSizePt(value: string): number | undefined {
  const map: Record<string, number> = {
    'xx-small': 7,
    'x-small': 8,
    small: 10,
    medium: 12,
    large: 14,
    'x-large': 18,
    'xx-large': 24,
    'xxx-large': 36,
  };
  return map[value];
}
