/**
 * Clipboard helpers used for copy-out payload generation.
 *
 * Sanitized HTML is written to the system clipboard for interoperability with other apps only.
 * In-app consumers must not bind this HTML into the DOM.
 */

import DOMPurify from 'dompurify';
export function buildClipboardPayload(doc: Document, selection: Selection): { plainText: string; html: string } {
  if (selection.rangeCount < 1) {
    return { plainText: selection.toString(), html: '' };
  }
  const container = doc.createElement('div');
  for (let i = 0; i < selection.rangeCount; i++) {
    container.append(selection.getRangeAt(i).cloneContents());
  }
  sanitizeClipboardContainer(container);
  const html = container.innerHTML;
  const plainText = containerToPlainText(container);
  return { plainText, html };
}

export function sanitizeClipboardContainer(container: HTMLElement): void {
  replaceTaskCheckboxInputs(container);
  for (const element of containerElementsBottomUp(container)) {
    sanitizeClipboardElement(container, element);
  }
  const purifier = clipboardDomPurify();
  if (purifier) {
    container.innerHTML = purifier.sanitize(container.innerHTML, {
      ALLOWED_TAGS: [...CLIPBOARD_ALLOWED_TAGS].map(tag => tag.toLowerCase()),
      ALLOWED_ATTR: ['href', 'title', 'start', 'reversed', 'type', 'value'],
      ALLOW_DATA_ATTR: false,
    });
  }
  replaceTaskCheckboxInputs(container);
  restoreTaskCheckboxMarkers(container);
  for (const element of containerElementsBottomUp(container)) {
    stripClipboardElementAttributes(element);
  }
}

const CLIPBOARD_ALLOWED_TAGS = new Set([
  'A',
  'B',
  'BLOCKQUOTE',
  'BR',
  'CODE',
  'DIV',
  'EM',
  'FONT',
  'H1',
  'H2',
  'H3',
  'H4',
  'H5',
  'H6',
  'I',
  'LI',
  'OL',
  'P',
  'PRE',
  'SPAN',
  'STRONG',
  'TABLE',
  'TBODY',
  'TD',
  'TH',
  'THEAD',
  'TR',
  'UL',
]);

const CLIPBOARD_BLOCKED_TAGS = new Set([
  'SCRIPT',
  'STYLE',
  'NOSCRIPT',
  'IFRAME',
  'OBJECT',
  'META',
  'HEAD',
  'SVG',
  'MATH',
  'FORM',
  'BUTTON',
  'VIDEO',
  'AUDIO',
  'EMBED',
  'LINK',
  'BASE',
  'TEMPLATE',
  'FRAME',
  'FRAMESET',
]);

const CLIPBOARD_ALLOWED_ATTRS_BY_TAG = new Map<string, ReadonlySet<string>>([
  ['a', new Set(['href', 'title'])],
  ['ol', new Set(['start', 'reversed', 'type'])],
  ['li', new Set(['value'])],
]);

const EVENT_HANDLER_ATTR = /^on/i;
const UNSAFE_HREF_PATTERN = /^(javascript|data|vbscript):/i;
const CHECKED_TASK_MARKER_TOKEN = '::clipboard-task-checked::';
const UNCHECKED_TASK_MARKER_TOKEN = '::clipboard-task-unchecked::';

function clipboardDomPurify(): ReturnType<typeof DOMPurify> | null {
  if (typeof window === 'undefined') {
    return null;
  }
  return DOMPurify(window);
}

function sanitizeClipboardElement(container: HTMLElement, element: HTMLElement): void {
  if (!element.isConnected) {
    return;
  }

  const tagName = element.tagName;
  const lowerTag = tagName.toLowerCase();

  if (CLIPBOARD_BLOCKED_TAGS.has(tagName)) {
    element.remove();
    return;
  }

  if (tagName === 'IMG') {
    const alt = element.getAttribute('alt')?.trim();
    if (alt) {
      element.replaceWith(container.ownerDocument.createTextNode(alt));
    } else {
      element.remove();
    }
    return;
  }

  if (!CLIPBOARD_ALLOWED_TAGS.has(tagName)) {
    unwrapElement(element);
    return;
  }

  stripClipboardElementAttributes(element);
}

function stripClipboardElementAttributes(element: HTMLElement): void {
  const tagName = element.tagName;
  const lowerTag = tagName.toLowerCase();

  for (const attr of Array.from(element.attributes)) {
    const attrName = attr.name.toLowerCase();
    if (EVENT_HANDLER_ATTR.test(attrName)) {
      element.removeAttribute(attr.name);
      continue;
    }
    const allowed = CLIPBOARD_ALLOWED_ATTRS_BY_TAG.get(lowerTag);
    if (!allowed?.has(attrName)) {
      element.removeAttribute(attr.name);
    }
  }

  const style = element.getAttribute('style');
  if (style !== null && !CLIPBOARD_ALLOWED_ATTRS_BY_TAG.get(lowerTag)?.has('style')) {
    element.removeAttribute('style');
  }

  if (tagName === 'A') {
    const href = element.getAttribute('href')?.trim() ?? '';
    if (!href || UNSAFE_HREF_PATTERN.test(href) || (!/^https?:\/\//i.test(href) && !/^mailto:/i.test(href))) {
      element.removeAttribute('href');
    }
  }
}

function replaceTaskCheckboxInputs(container: HTMLElement): void {
  for (const element of Array.from(container.querySelectorAll('input'))) {
    if (!(element instanceof HTMLInputElement)) {
      continue;
    }
    const isCheckbox = (element.getAttribute('type') ?? '').toLowerCase() === 'checkbox';
    if (!isCheckbox) {
      element.remove();
      continue;
    }
    const marker = element.hasAttribute('checked') ? CHECKED_TASK_MARKER_TOKEN : UNCHECKED_TASK_MARKER_TOKEN;
    element.replaceWith(container.ownerDocument.createTextNode(marker));
  }
}

function restoreTaskCheckboxMarkers(container: HTMLElement): void {
  const walker = container.ownerDocument.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const text = node.textContent ?? '';
    if (text.includes(CHECKED_TASK_MARKER_TOKEN) || text.includes(UNCHECKED_TASK_MARKER_TOKEN)) {
      node.textContent = text
        .replaceAll(CHECKED_TASK_MARKER_TOKEN, '[x]')
        .replaceAll(UNCHECKED_TASK_MARKER_TOKEN, '[ ]');
    }
    node = walker.nextNode();
  }
}

function unwrapElement(element: HTMLElement): void {
  const parent = element.parentNode;
  if (!parent) {
    element.remove();
    return;
  }
  while (element.firstChild) {
    parent.insertBefore(element.firstChild, element);
  }
  element.remove();
}

function containerElementsBottomUp(root: HTMLElement): HTMLElement[] {
  const elements: HTMLElement[] = [];
  const walk = (node: Node): void => {
    for (const child of Array.from(node.childNodes)) {
      walk(child);
    }
    if (node instanceof HTMLElement && node !== root) {
      elements.push(node);
    }
  };
  walk(root);
  return elements;
}

function containerToPlainText(container: HTMLElement): string {
  const raw = renderPlainNodes(Array.from(container.childNodes), 0, false);
  return raw.replace(/\r\n/g, '\n').replace(/\n{3,}/g, '\n\n').trimEnd();
}

function renderPlainNodes(nodes: Node[], depth: number, inPre: boolean): string {
  let out = '';
  for (const node of nodes) {
    if (node.nodeType === Node.TEXT_NODE) {
      const text = node.textContent ?? '';
      out += inPre ? text : normalizeInlineWhitespace(text);
      continue;
    }
    if (!(node instanceof HTMLElement)) {
      continue;
    }
    const tag = node.tagName;
    if (tag === 'BR') {
      out += '\n';
      continue;
    }
    if (tag === 'UL' || tag === 'OL') {
      out = appendBlock(out, renderPlainList(node), true);
      continue;
    }
    if (tag === 'PRE') {
      out = appendBlock(out, renderPlainNodes(Array.from(node.childNodes), depth, true));
      continue;
    }
    if (tag === 'TD' || tag === 'TH') {
      out = appendBlock(out, renderPlainNodes(Array.from(node.childNodes), depth, inPre), true);
      continue;
    }
    if (isBlockTag(tag)) {
      out = appendBlock(out, renderPlainNodes(Array.from(node.childNodes), depth, inPre), true);
      continue;
    }
    out += renderPlainNodes(Array.from(node.childNodes), depth, inPre);
  }
  return out;
}


function renderPlainList(list: HTMLElement): string {
  return collectPlainListLines(list, 0).join('\n');
}


function listIndent(depth: number): string {
  return '  '.repeat(depth);
}

function prefixListLine(depth: number, marker: string, content: string): string {
  return `${listIndent(depth)}${marker} ${content}`;
}

function collectPlainListLines(list: HTMLElement, depth: number): string[] {
  const lines: string[] = [];
  const isOrdered = list.tagName === 'OL';
  let index = listStartIndex(list);
  let hadPreviousLi = false;
  for (const child of Array.from(list.childNodes)) {
    if (!(child instanceof HTMLElement)) {
      continue;
    }
    if (child.tagName === 'LI') {
      lines.push(...plainListItemLines(child, isOrdered ? `${index++}.` : '•', depth));
      hadPreviousLi = true;
      continue;
    }
    if (child.tagName === 'UL' || child.tagName === 'OL') {
      const orphanDepth = hadPreviousLi ? depth + 1 : depth;
      lines.push(...collectPlainListLines(child, orphanDepth));
    }
  }
  return lines;
}


function plainListItemLines(item: HTMLElement, marker: string, depth: number): string[] {
  const lines: string[] = [];
  const inlineParts: Node[] = [];
  for (const child of Array.from(item.childNodes)) {
    if (child instanceof HTMLElement && (child.tagName === 'UL' || child.tagName === 'OL')) {
      flushPlainListInline(inlineParts, marker, depth, lines);
      inlineParts.length = 0;
      lines.push(...collectPlainListLines(child, depth + 1));
      continue;
    }
    inlineParts.push(child);
  }
  flushPlainListInline(inlineParts, marker, depth, lines);
  if (!lines.length) {
    const fallback = elementTextContent(item);
    if (fallback) {
      lines.push(prefixListLine(depth, marker, fallback));
    }
  }
  return lines;
}


function flushPlainListInline(nodes: Node[], marker: string, depth: number, lines: string[]): void {
  const inline = renderPlainNodes(nodes, 0, false).replace(/\n+/g, ' ').trim();
  if (inline) {
    lines.push(prefixListLine(depth, marker, inline));
  }
}


function listStartIndex(list: HTMLElement): number {
  if (list.tagName !== 'OL') {
    return 1;
  }
  const start = Number.parseInt(list.getAttribute('start') ?? '', 10);
  return Number.isFinite(start) ? start : 1;
}

function elementTextContent(el: HTMLElement): string {
  return (el.textContent ?? '').replace(/\s+/g, ' ').trim();
}

function appendBlock(current: string, block: string, addBlankLine = false): string {
  const next = block.trim();
  if (!next) {
    return current;
  }
  if (!current) {
    return next;
  }
  const separator = addBlankLine ? '\n\n' : '\n';
  if (current.endsWith(separator)) {
    return `${current}${next}`;
  }
  if (current.endsWith('\n') && separator === '\n') {
    return `${current}${next}`;
  }
  return `${current}${separator}${next}`;
}

function normalizeInlineWhitespace(text: string): string {
  return text.replace(/\s+/g, ' ');
}

function isBlockTag(tag: string): boolean {
  return (
    tag === 'P' ||
    tag === 'DIV' ||
    tag === 'SECTION' ||
    tag === 'ARTICLE' ||
    tag === 'BLOCKQUOTE' ||
    tag === 'H1' ||
    tag === 'H2' ||
    tag === 'H3' ||
    tag === 'H4' ||
    tag === 'H5' ||
    tag === 'H6' ||
    tag === 'TABLE' ||
    tag === 'THEAD' ||
    tag === 'TBODY' ||
    tag === 'TR'
  );
}

