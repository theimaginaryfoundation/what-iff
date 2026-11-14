/**
 * The script of ours that runs first in every served page (ADR 0x050 §5), injected at the start of
 * its head by withSourcePositions. It talks to its parent window only (the preview site's host
 * page, which relays to the app, or the app itself for a page on its own), in messages
 * `{ wiBridge: ... }` out and `{ wiApp: ... }` in.
 *
 * - Picks an element for a new comment: an outline follows the pointer, with the element's tag and
 *   size, as a browser's element picker does; a click picks, Esc leaves.
 * - Describes the picked element: a selector chosen from the most stable thing it has, a
 *   fingerprint, its source range (`data-wi-src`) when the HTML parser read it from a file, and,
 *   for one a script made, where in the project's scripts that happened.
 * - Marks the elements threads are on, the active one more strongly, and keeps the marks in place
 *   through scrolling, resizing and the page's own changes; a click on a mark chooses its thread.
 *
 * It is plain JavaScript in a string, as it runs in pages we do not build. It must not contain the
 * sequence that ends a script element.
 */
export const PAGE_BRIDGE_SCRIPT = String.raw`(function () {
  'use strict';
  if (window.__whatiffBridge) return;
  window.__whatiffBridge = true;
  var SRC = 'data-wi-src';
  var Z = 2147483647;
  function post(message) {
    try { window.parent.postMessage({ wiBridge: message }, '*'); } catch (e) {}
  }

  // --- Where a script made an element ------------------------------------------------------
  var made = new WeakMap();
  var base = location.pathname.replace(/[^/]*$/, '');
  var tokenRoot = (location.pathname.match(/^(.*?\/p\/[^/]+\/)/) || [])[1] || base;
  function caller() {
    var stack = String((new Error()).stack || '').split('\n');
    for (var i = 0; i < stack.length; i++) {
      var m = /(https?:\/\/[^\s)]+):(\d+):(\d+)/.exec(stack[i]);
      if (!m) continue;
      var url;
      try { url = new URL(m[1]); } catch (e) { continue; }
      if (url.origin !== location.origin || url.pathname.indexOf(tokenRoot) !== 0) continue;
      return { path: decodeURIComponent(url.pathname.slice(tokenRoot.length)), line: +m[2], column: +m[3] };
    }
    return null;
  }
  function remember(el, at) {
    if (!at || !el || el.nodeType !== 1 || el.hasAttribute(SRC) || made.has(el)) return;
    made.set(el, at);
  }
  function rememberTree(root, at) {
    if (!at || !root) return;
    if (root.nodeType === 1) remember(root, at);
    if (root.querySelectorAll) {
      var all = root.querySelectorAll('*');
      for (var i = 0; i < all.length; i++) remember(all[i], at);
    }
  }
  (function wrap() {
    var D = Document.prototype;
    ['createElement', 'createElementNS'].forEach(function (name) {
      var original = D[name];
      D[name] = function () {
        var el = original.apply(this, arguments);
        remember(el, caller());
        return el;
      };
    });
    var E = Element.prototype;
    ['innerHTML', 'outerHTML'].forEach(function (name) {
      var d = Object.getOwnPropertyDescriptor(E, name);
      if (!d || !d.set) return;
      Object.defineProperty(E, name, {
        configurable: true, enumerable: d.enumerable, get: d.get,
        set: function (value) {
          var parent = this.parentNode;
          d.set.call(this, value);
          rememberTree(name === 'innerHTML' ? this : parent, caller());
        }
      });
    });
    var insert = E.insertAdjacentHTML;
    E.insertAdjacentHTML = function () {
      var r = insert.apply(this, arguments);
      rememberTree(this.parentNode || this, caller());
      return r;
    };
  })();

  // --- Selectors and fingerprints ------------------------------------------------------------
  var STABLE_ATTRS = ['data-testid', 'data-test', 'data-cy', 'data-id', 'name', 'aria-label'];
  var UTILITY = /^(-?(p|m)[trblxyse]?|w|h|min|max|size|text|bg|from|to|via|flex|grid|gap|items|justify|content|self|place|rounded|border|ring|shadow|outline|font|leading|tracking|space|inset|top|left|right|bottom|start|end|z|opacity|col|row|order|overflow|object|cursor|select|transition|duration|ease|delay|animate|transform|scale|rotate|translate|skew|origin|fill|stroke|underline|line|decoration|list|whitespace|break|truncate|sr|not|hidden|block|inline|table|contents|absolute|relative|fixed|sticky|static|visible|invisible|container|aspect|columns|basis|grow|shrink|float|clear|isolate|mix|blur|brightness|contrast|drop|grayscale|invert|saturate|sepia|backdrop|appearance|pointer|resize|scroll|snap|touch|will|accent|caret)(-|$)/;
  function generated(value) {
    return !/^[A-Za-z][\w-]{0,60}$/.test(value) || /\d{4,}/.test(value) || /^[a-f0-9]{6,}$/i.test(value) ||
      /^(r|radix|headlessui|mui|ember|ng|react|vue|svelte|chakra|mantine)[-:_]/i.test(value) || /[_-][a-zA-Z0-9]{5,}$/.test(value) && /\d/.test(value);
  }
  function unique(selector, el) {
    try {
      var found = document.querySelectorAll(selector);
      return found.length === 1 && found[0] === el;
    } catch (e) { return false; }
  }
  function esc(value) { return window.CSS && CSS.escape ? CSS.escape(value) : value.replace(/[^\w-]/g, '\\$&'); }
  function attrSelector(name, value) { return '[' + name + '="' + String(value).replace(/["\\]/g, '\\$&') + '"]'; }
  function stableClasses(el) {
    return Array.prototype.filter.call(el.classList, function (c) {
      return c.indexOf(':') < 0 && c.indexOf('[') < 0 && c.indexOf('/') < 0 && !UTILITY.test(c) && !generated(c);
    });
  }
  /** The selector and the attributes it used, from the most stable thing the element has. */
  function describe(el) {
    var tag = el.tagName.toLowerCase();
    var used = {};
    if (el.id && !generated(el.id) && unique('#' + esc(el.id), el)) {
      used.id = el.id;
      return { selector: '#' + esc(el.id), used: used };
    }
    for (var i = 0; i < STABLE_ATTRS.length; i++) {
      var v = el.getAttribute(STABLE_ATTRS[i]);
      if (!v) continue;
      var s = tag + attrSelector(STABLE_ATTRS[i], v);
      if (unique(s, el)) { used[STABLE_ATTRS[i]] = v; return { selector: s, used: used }; }
    }
    var classes = stableClasses(el);
    for (var a = 0; a < classes.length; a++) {
      var one = tag + '.' + esc(classes[a]);
      if (unique(one, el)) { used['class'] = classes[a]; return { selector: one, used: used }; }
      for (var b = a + 1; b < classes.length; b++) {
        var two = one + '.' + esc(classes[b]);
        if (unique(two, el)) { used['class'] = classes[a] + ' ' + classes[b]; return { selector: two, used: used }; }
      }
    }
    // A path of positions, from the nearest ancestor with a stable id (or the body).
    var parts = [];
    var node = el;
    while (node && node.nodeType === 1 && node !== document.documentElement) {
      if (node !== el && node.id && !generated(node.id) && unique('#' + esc(node.id), node)) {
        parts.unshift('#' + esc(node.id));
        break;
      }
      var name = node.tagName.toLowerCase();
      if (name === 'body') { parts.unshift('body'); break; }
      var n = 1;
      for (var sib = node.previousElementSibling; sib; sib = sib.previousElementSibling) if (sib.tagName === node.tagName) n++;
      parts.unshift(name + ':nth-of-type(' + n + ')');
      node = node.parentElement;
    }
    return { selector: parts.join(' > '), used: used };
  }
  function textOf(el) { return String(el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 80); }
  function fingerprint(el, used) { return { tag: el.tagName.toLowerCase(), text: textOf(el), attrs: used }; }
  function label(el) {
    var s = el.tagName.toLowerCase();
    if (el.id) s += '#' + el.id;
    var classes = stableClasses(el).slice(0, 2);
    if (classes.length) s += '.' + classes.join('.');
    return s;
  }

  /** How the comments panel names a picked element: its short label and the start of its text. */
  function pickLabel(el) {
    var text = textOf(el).slice(0, 40);
    return text ? label(el) + ' \u201c' + text + (textOf(el).length > 40 ? '\u2026' : '') + '\u201d' : label(el);
  }

  // --- The overlay: a layer of ours over the page, out of the page's styles -----------------
  var host, layer;
  function ensureLayer() {
    if (layer) return layer;
    host = document.createElement('whatiff-overlay');
    host.setAttribute('style', 'position:fixed;inset:0;pointer-events:none;z-index:' + Z + ';');
    var root = host.attachShadow ? host.attachShadow({ mode: 'closed' }) : host;
    var style = document.createElement('style');
    style.textContent =
      '.box{position:fixed;border:2px solid #7c5cff;background:rgba(124,92,255,.12);border-radius:2px;pointer-events:none;box-sizing:border-box}' +
      '.tip{position:fixed;font:600 11px/1.4 system-ui,sans-serif;color:#fff;background:#1f1a33;padding:2px 6px;border-radius:4px;pointer-events:none;white-space:nowrap}' +
      '.mark{position:fixed;width:20px;height:20px;margin:-10px 0 0 -10px;border-radius:50%;border:2px solid #fff;background:#f5a524;box-shadow:0 1px 4px rgba(0,0,0,.35);pointer-events:auto;cursor:pointer;padding:0}' +
      '.mark.active{background:#7c5cff;width:24px;height:24px;margin:-12px 0 0 -12px}' +
      '.ring{position:fixed;border:2px solid #7c5cff;border-radius:3px;pointer-events:none;box-sizing:border-box}' +
      '.ring.open{border-color:rgba(245,165,36,.7);border-style:dashed}';
    root.appendChild(style);
    layer = document.createElement('div');
    root.appendChild(layer);
    document.documentElement.appendChild(host);
    return layer;
  }
  function isOurs(el) { return !!el && (el === host || (host && host.contains(el))); }

  // --- Picking --------------------------------------------------------------------------------
  var picking = false, box = null, tip = null, current = null;
  function startPicking() {
    if (picking) return;
    picking = true;
    ensureLayer();
    box = document.createElement('div'); box.className = 'box';
    tip = document.createElement('div'); tip.className = 'tip';
    layer.appendChild(box); layer.appendChild(tip);
    box.style.display = tip.style.display = 'none';
    document.documentElement.style.cursor = 'crosshair';
    window.addEventListener('mousemove', onMove, true);
    window.addEventListener('click', onClick, true);
    window.addEventListener('keydown', onKey, true);
  }
  function stopPicking() {
    if (!picking) return;
    picking = false;
    if (box) box.remove();
    if (tip) tip.remove();
    box = tip = current = null;
    document.documentElement.style.cursor = '';
    window.removeEventListener('mousemove', onMove, true);
    window.removeEventListener('click', onClick, true);
    window.removeEventListener('keydown', onKey, true);
  }
  function onMove(event) {
    var el = document.elementFromPoint(event.clientX, event.clientY);
    if (!el || isOurs(el) || el === document.documentElement) return;
    current = el;
    var r = el.getBoundingClientRect();
    box.style.display = tip.style.display = 'block';
    box.style.left = r.left + 'px'; box.style.top = r.top + 'px';
    box.style.width = r.width + 'px'; box.style.height = r.height + 'px';
    tip.textContent = label(el) + '  ' + Math.round(r.width) + '×' + Math.round(r.height);
    tip.style.left = Math.max(4, r.left) + 'px';
    tip.style.top = (r.top > 24 ? r.top - 22 : r.bottom + 4) + 'px';
  }
  function onClick(event) {
    event.preventDefault();
    event.stopPropagation();
    var el = current || document.elementFromPoint(event.clientX, event.clientY);
    stopPicking();
    if (!el || isOurs(el)) { post({ type: 'pick-cancelled' }); return; }
    var d = describe(el);
    post({
      type: 'picked',
      selector: d.selector,
      fingerprint: fingerprint(el, d.used),
      src: el.getAttribute(SRC),
      created: made.get(el) || null,
      label: pickLabel(el)
    });
  }
  function onKey(event) {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    event.stopPropagation();
    stopPicking();
    post({ type: 'pick-cancelled' });
  }

  // --- Marks ----------------------------------------------------------------------------------
  var marks = [];
  var shown = [];
  /** The element a mark is on: by its source range, then its selector if the fingerprint still fits, then its fingerprint. */
  function find(mark) {
    if (mark.src) {
      var m = /^(.+):(\d+)-(\d+)$/.exec(mark.src);
      if (m) {
        var file = m[1], start = +m[2], end = +m[3];
        var all = document.querySelectorAll('[' + SRC + ']');
        // The tag whose range holds the anchor's start (a thread made on the tag), else the first
        // tag that starts inside the anchor (one made on a stretch of the source).
        var best = null, holds = -1, inside = null;
        for (var i = 0; i < all.length; i++) {
          var r = /^(.+):(\d+)-(\d+)$/.exec(all[i].getAttribute(SRC));
          if (!r || r[1] !== file) continue;
          var s = +r[2], e = +r[3];
          if (s <= start && start < e && s > holds) { best = all[i]; holds = s; }
          else if (!inside && s >= start && s < end) inside = all[i];
        }
        best = best || inside;
        if (best) return best;
      }
    }
    if (mark.selector) {
      var el = null;
      try { el = document.querySelector(mark.selector); } catch (e) {}
      if (el && fits(el, mark.fingerprint)) return el;
      if (mark.fingerprint && mark.fingerprint.tag) {
        var same = document.getElementsByTagName(mark.fingerprint.tag);
        for (var j = 0; j < same.length; j++) if (fits(same[j], mark.fingerprint, true)) return same[j];
      }
      if (el && !mark.fingerprint) return el;
    }
    return null;
  }
  function fits(el, fp, strict) {
    if (!fp) return true;
    if (fp.tag && el.tagName.toLowerCase() !== fp.tag) return false;
    if (!fp.text) return !strict;
    var text = textOf(el);
    return strict ? text === fp.text : text.slice(0, 20) === fp.text.slice(0, 20);
  }
  function render() {
    scheduled = false;
    if (!marks.length && !shown.length) return;
    ensureLayer();
    for (var k = 0; k < shown.length; k++) shown[k].remove();
    shown = [];
    var unplaced = [];
    for (var i = 0; i < marks.length; i++) {
      var mark = marks[i];
      var el = find(mark);
      if (!el) { unplaced.push(mark.id); continue; }
      var r = el.getBoundingClientRect();
      if (r.bottom < 0 || r.top > innerHeight || r.right < 0 || r.left > innerWidth || (!r.width && !r.height)) continue;
      var ring = document.createElement('div');
      ring.className = 'ring ' + mark.state;
      ring.style.left = r.left + 'px'; ring.style.top = r.top + 'px';
      ring.style.width = r.width + 'px'; ring.style.height = r.height + 'px';
      var dot = document.createElement('button');
      dot.className = 'mark' + (mark.state === 'active' ? ' active' : '');
      dot.title = 'Comment';
      dot.style.left = Math.min(innerWidth - 12, r.right) + 'px';
      dot.style.top = Math.max(12, r.top) + 'px';
      dot.addEventListener('click', (function (id) { return function (e) { e.preventDefault(); e.stopPropagation(); post({ type: 'select', id: id }); }; })(mark.id));
      layer.appendChild(ring); layer.appendChild(dot);
      shown.push(ring, dot);
    }
    if (unplaced.join() !== lastUnplaced) { lastUnplaced = unplaced.join(); post({ type: 'unplaced', ids: unplaced }); }
  }
  var lastUnplaced = '';
  var scheduled = false;
  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(render);
  }
  window.addEventListener('scroll', schedule, true);
  window.addEventListener('resize', schedule);
  new MutationObserver(function (records) {
    for (var i = 0; i < records.length; i++) if (!isOurs(records[i].target)) { schedule(); return; }
  }).observe(document.documentElement, { childList: true, subtree: true, attributes: true, characterData: true });

  function reveal(id) {
    for (var i = 0; i < marks.length; i++) {
      if (marks[i].id !== id) continue;
      var el = find(marks[i]);
      if (el && el.scrollIntoView) el.scrollIntoView({ block: 'center', behavior: 'smooth' });
      return;
    }
  }

  window.addEventListener('message', function (event) {
    if (event.source !== window.parent || !event.data || !event.data.wiApp) return;
    var m = event.data.wiApp;
    if (m.type === 'marks' && Array.isArray(m.marks)) { marks = m.marks; schedule(); }
    else if (m.type === 'pick') startPicking();
    else if (m.type === 'stop-pick') stopPicking();
    else if (m.type === 'reveal') reveal(m.id);
  });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', function () { post({ type: 'ready' }); });
  else post({ type: 'ready' });
})();`;

/** What the bridge reports (ADR 0x050 §5). */
export type PageBridgeEvent =
  | { type: 'ready' }
  | {
      type: 'picked';
      selector: string;
      fingerprint: { tag: string; text?: string; attrs?: Record<string, string> };
      /** The element's source range (`<file>:<start>-<end>`), when the HTML parser read it from a file. */
      src: string | null;
      /** Where a script made it, in the project (best effort). */
      created: { path: string; line: number; column: number } | null;
      label: string;
    }
  | { type: 'pick-cancelled' }
  | { type: 'select'; id: string }
  | { type: 'unplaced'; ids: string[] };

/** A thread's mark on the page: its code anchor as a source range, its element anchor, or both. */
export interface PageMark {
  id: string;
  state: 'open' | 'active';
  src?: string;
  selector?: string;
  fingerprint?: { tag: string; text?: string; attrs?: Record<string, string> };
}

/** What the app tells the bridge. */
export type PageBridgeAction =
  { type: 'marks'; marks: PageMark[] } | { type: 'pick' } | { type: 'stop-pick' } | { type: 'reveal'; id: string };

/** A bridge event from a message's data, or null for anything else. */
export function readBridgeEvent(data: unknown): PageBridgeEvent | null {
  const event = data && typeof data === 'object' ? (data as { type?: unknown }) : null;
  return event && typeof event.type === 'string' ? (event as PageBridgeEvent) : null;
}
