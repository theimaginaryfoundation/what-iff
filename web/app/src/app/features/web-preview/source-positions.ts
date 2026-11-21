import { parse } from 'parse5';
import type { DefaultTreeAdapterMap } from 'parse5';

/**
 * Where each element of a served page came from (ADR 0x050 §5): every element the HTML parser read
 * from the file gets `data-wi-src="<file>:<start>-<end>"`, its opening tag's range in the file's
 * text, in the units comment anchors use (UTF-16, after a leading BOM). A pick on the rendered page
 * reads it back as the code anchor of a new thread, and a thread's code anchor finds its element
 * by it. Elements the parser made up (an implied <body>) and elements a script made have none.
 *
 * The attributes are inserted into the text as it is, from the last tag to the first, so nothing
 * else in the page changes. A script of ours can be put at the start of the head the same way.
 */

/** The attribute that carries an element's source range. */
export const SOURCE_ATTRIBUTE = 'data-wi-src';

type Node = DefaultTreeAdapterMap['node'];
type Element = DefaultTreeAdapterMap['element'];

/** A source range as the attribute writes it: `<file>:<start>-<end>`. */
export interface SourceRange {
  file: string;
  start: number;
  end: number;
}

export function formatSourceRange(range: SourceRange): string {
  return `${range.file}:${range.start}-${range.end}`;
}

export function parseSourceRange(value: string | null | undefined): SourceRange | null {
  const match = /^(.+):(\d+)-(\d+)$/.exec(value ?? '');
  if (!match) return null;
  const start = Number(match[2]);
  const end = Number(match[3]);
  return end > start ? { file: match[1], start, end } : null;
}

/** The page's text as comment anchors count it: without a leading byte order mark. */
export function anchorText(html: string): string {
  return html.startsWith('﻿') ? html.slice(1) : html;
}

/**
 * The page with each element's source range on it, and `headScript` (a script's code) first in its
 * head when given. `file` names the file the text is (its id).
 */
export function withSourcePositions(html: string, file: string, headScript?: string): string {
  const text = anchorText(html);
  const doc = parse(text, { sourceCodeLocationInfo: true });
  const inserts: { at: number; text: string }[] = [];
  let head: Element | null = null;
  let root: Element | null = null;
  let doctypeEnd = 0;

  const visit = (node: Node) => {
    if ('tagName' in node) {
      const tag = node.sourceCodeLocation?.startTag;
      if (tag) {
        // Right after the tag's name, as written: never inside an attribute's value, as before the
        // closing `>` can be (`<a href=/docs/>` would lose the URL's last slash).
        const name = /^[^\s/>]+/.exec(text.slice(tag.startOffset + 1))?.[0] ?? node.tagName;
        const at = tag.startOffset + 1 + name.length;
        // Not on a script: widgets read their own script's data attributes (currentScript.dataset)
        // as their settings, and a script is no element a thread could be shown on.
        if (node.tagName !== 'script' && !node.attrs.some(a => a.name === SOURCE_ATTRIBUTE)) {
          inserts.push({
            at,
            text: ` ${SOURCE_ATTRIBUTE}="${escapeAttribute(formatSourceRange({ file, start: tag.startOffset, end: tag.endOffset }))}"`,
          });
        }
        if (node.tagName === 'head' && !head) head = node;
        if (node.tagName === 'html' && !root) root = node;
      }
      // A <template>'s elements are in its content.
      const content = (node as Element & { content?: { childNodes: Node[] } }).content;
      for (const child of content?.childNodes ?? []) visit(child);
    } else if (node.nodeName === '#documentType' && node.sourceCodeLocation) {
      doctypeEnd = node.sourceCodeLocation.endOffset;
    }
    for (const child of 'childNodes' in node ? node.childNodes : []) visit(child);
  };
  visit(doc);

  if (headScript !== undefined) {
    const after = (el: Element | null) => el?.sourceCodeLocation?.startTag?.endOffset;
    const at = after(head) ?? after(root) ?? doctypeEnd;
    // `</script` in the code would end the element early; the code is ours, but be sure.
    inserts.push({ at, text: `<script>${headScript.replace(/<\/script/gi, '<\\/script')}</script>` });
  }

  inserts.sort((a, b) => b.at - a.at);
  let out = text;
  for (const insert of inserts) out = out.slice(0, insert.at) + insert.text + out.slice(insert.at);
  return out;
}

function escapeAttribute(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;');
}
