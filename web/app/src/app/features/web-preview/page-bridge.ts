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
 *   through scrolling, resizing and the page's own changes; a click on a mark chooses its thread, and
 *   right-clicking a marked element (or its mark) asks the app for the thread's menu.
 *
 * - Draws a picture of the page on request (the file's preview, for agents and the gallery: ADR
 *   0x050 §8), with a pinned modern-screenshot (MIT) loaded from jsDelivr when it is first asked for.
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
  // Each element a call made keeps the Error of that call; its stack is only read (formatted) when
  // the element is picked, as formatting a stack for every element a page makes would be slow.
  var made = new WeakMap();
  var base = location.pathname.replace(/[^/]*$/, '');
  var served = location.pathname.match(/^(.*?\/p\/[^/]+\/)/);
  var tokenRoot = (served || [])[1] || base;
  /** The page's own path in its project, or null for a page shown on its own. */
  var pagePath = served ? decodeURIComponent(location.pathname.slice(tokenRoot.length)) : null;
  function siteOf(error) {
    var stack = String((error && error.stack) || '').split('\n');
    for (var i = 0; i < stack.length; i++) {
      var m = /(https?:\/\/[^\s)]+):(\d+):(\d+)/.exec(stack[i]);
      if (!m) continue;
      var url;
      try { url = new URL(m[1]); } catch (e) { continue; }
      if (url.origin !== location.origin || url.pathname.indexOf(tokenRoot) !== 0) continue;
      // Frames in the page itself are this bridge's own (it is inline in the page) or the page's
      // inline scripts, whose code has no place a thread can be kept on: the first project file
      // after them is where the element was made.
      if (url.pathname === location.pathname) continue;
      return { path: decodeURIComponent(url.pathname.slice(tokenRoot.length)), line: +m[2], column: +m[3] };
    }
    return null;
  }
  function remember(el, at) {
    if (!el || el.nodeType !== 1 || el.hasAttribute(SRC) || made.has(el)) return;
    made.set(el, at);
  }
  function rememberTree(root, at) {
    if (!root) return;
    if (root.nodeType === 1) remember(root, at);
    if (root.querySelectorAll) {
      var all = root.querySelectorAll('*');
      for (var i = 0; i < all.length; i++) remember(all[i], at);
    }
  }
  /** The nodes of parent from (exclusive; null: its first child) until until (exclusive; null: the end). */
  function rememberBetween(parent, from, until, at) {
    if (!parent) return;
    for (var node = from ? from.nextSibling : parent.firstChild; node && node !== until; node = node.nextSibling) rememberTree(node, at);
  }
  (function wrap() {
    var D = Document.prototype;
    ['createElement', 'createElementNS'].forEach(function (name) {
      var original = D[name];
      D[name] = function () {
        var el = original.apply(this, arguments);
        remember(el, new Error());
        return el;
      };
    });
    var importNode = D.importNode;
    D.importNode = function () {
      var node = importNode.apply(this, arguments);
      rememberTree(node, new Error());
      return node;
    };
    var cloneNode = Node.prototype.cloneNode;
    Node.prototype.cloneNode = function () {
      var node = cloneNode.apply(this, arguments);
      if (node && (node.nodeType === 1 || node.nodeType === 11)) rememberTree(node, new Error());
      return node;
    };
    var E = Element.prototype;
    var inner = Object.getOwnPropertyDescriptor(E, 'innerHTML');
    if (inner && inner.set) {
      Object.defineProperty(E, 'innerHTML', {
        configurable: true, enumerable: inner.enumerable, get: inner.get,
        set: function (value) {
          inner.set.call(this, value);
          rememberBetween(this, null, null, new Error());
        }
      });
    }
    var outer = Object.getOwnPropertyDescriptor(E, 'outerHTML');
    if (outer && outer.set) {
      Object.defineProperty(E, 'outerHTML', {
        configurable: true, enumerable: outer.enumerable, get: outer.get,
        set: function (value) {
          var parent = this.parentNode, before = this.previousSibling, after = this.nextSibling;
          outer.set.call(this, value);
          rememberBetween(parent, before, after, new Error());
        }
      });
    }
    var insert = E.insertAdjacentHTML;
    E.insertAdjacentHTML = function (position) {
      // Only what this call inserted: not the siblings or the element it was inserted beside.
      var where = String(position).toLowerCase();
      var parent = where === 'afterbegin' || where === 'beforeend' ? this : this.parentNode;
      var from = where === 'afterbegin' ? null : where === 'beforeend' ? this.lastChild : where === 'beforebegin' ? this.previousSibling : this;
      var until = where === 'afterbegin' ? this.firstChild : where === 'beforeend' ? null : where === 'beforebegin' ? this : this.nextSibling;
      var r = insert.apply(this, arguments);
      rememberBetween(parent, from, until, new Error());
      return r;
    };
  })();

  // --- Selectors and fingerprints ------------------------------------------------------------
  var STABLE_ATTRS = ['data-testid', 'data-test', 'data-cy', 'data-id', 'name', 'aria-label'];
  var UTILITY = /^(-?(p|m)[trblxyse]?|w|h|min|max|size|text|bg|from|to|via|flex|grid|gap|items|justify|content|self|place|rounded|border|ring|shadow|outline|font|leading|tracking|space|inset|top|left|right|bottom|start|end|z|opacity|col|row|order|overflow|object|cursor|select|transition|duration|ease|delay|animate|transform|scale|rotate|translate|skew|origin|fill|stroke|underline|line|decoration|list|whitespace|break|truncate|sr|not|hidden|block|inline|table|contents|absolute|relative|fixed|sticky|static|visible|invisible|container|aspect|columns|basis|grow|shrink|float|clear|isolate|mix|blur|brightness|contrast|drop|grayscale|invert|saturate|sepia|backdrop|appearance|pointer|resize|scroll|snap|touch|will|accent|caret)(-|$)/;
  /** A selector longer than this is cut to its last steps (the server keeps at most 1000 characters). */
  var MAX_SELECTOR = 900;
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
  /** An element's name as selectors and the server keep it: lower case in HTML, as written in SVG (foreignObject). */
  function tagOf(el) { return el.localName || el.tagName.toLowerCase(); }
  function esc(value) { return window.CSS && CSS.escape ? CSS.escape(value) : value.replace(/[^\w-]/g, '\\$&'); }
  function attrSelector(name, value) { return '[' + name + '="' + String(value).replace(/["\\]/g, '\\$&') + '"]'; }
  function stableClasses(el) {
    return Array.prototype.filter.call(el.classList, function (c) {
      return c.indexOf(':') < 0 && c.indexOf('[') < 0 && c.indexOf('/') < 0 && !UTILITY.test(c) && !generated(c);
    });
  }
  /** The selector and the attributes it used, from the most stable thing the element has. */
  function describe(el) {
    var tag = esc(tagOf(el));
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
    // A path of positions, from the nearest ancestor with a stable id (or the body), its last steps
    // only when it would be too long to keep.
    var parts = [];
    var length = 0;
    var node = el;
    while (node && node.nodeType === 1 && node !== document.documentElement) {
      if (node !== el && node.id && !generated(node.id) && unique('#' + esc(node.id), node)) {
        parts.unshift('#' + esc(node.id));
        break;
      }
      var name = tagOf(node);
      if (name === 'body') { parts.unshift('body'); break; }
      var n = 1;
      for (var sib = node.previousElementSibling; sib; sib = sib.previousElementSibling) if (sib.localName === node.localName) n++;
      var step = esc(name) + ':nth-of-type(' + n + ')';
      if (length + step.length + 3 > MAX_SELECTOR) break;
      parts.unshift(step);
      length += step.length + 3;
      node = node.parentElement;
    }
    return { selector: parts.join(' > '), used: used };
  }
  /** Whether a selector names the element by an id or a stable attribute, which outlast its text. */
  function stableSelector(selector) {
    return /^#[^\s>+~]+$/.test(selector) || STABLE_ATTRS.some(function (a) { return selector.indexOf('[' + a + '=') >= 0; }) && !/\s/.test(selector.replace(/"[^"]*"/g, ''));
  }
  function textOf(el) { return String(el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 80); }
  function fingerprint(el, used) { return { tag: tagOf(el), text: textOf(el), attrs: used }; }
  function label(el) {
    var s = tagOf(el);
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
  // In the document only while it shows something (marks, or the picker), so a page's own
  // structure (html > :last-child, lastElementChild) is its own the rest of the time.
  var host, layer;
  function ensureLayer() {
    if (!layer) {
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
    }
    if (!host.isConnected) document.documentElement.appendChild(host);
    return layer;
  }
  function releaseLayer() {
    if (host && host.isConnected && !picking && !drawn.size) host.remove();
  }
  function isOurs(el) { return !!el && (el === host || (host && host.contains(el))); }

  // --- Picking --------------------------------------------------------------------------------
  var picking = false, box = null, tip = null, current = null, cursorBefore = '';
  // The page's own handlers for these never run while picking: a pick must not open its menus or
  // start its drags.
  var SWALLOWED = ['pointerdown', 'pointerup', 'mousedown', 'mouseup', 'touchstart', 'touchend', 'dblclick', 'auxclick'];
  function swallow(event) { event.preventDefault(); event.stopPropagation(); event.stopImmediatePropagation(); }
  function startPicking() {
    if (picking) return;
    picking = true;
    ensureLayer();
    box = document.createElement('div'); box.className = 'box';
    tip = document.createElement('div'); tip.className = 'tip';
    layer.appendChild(box); layer.appendChild(tip);
    box.style.display = tip.style.display = 'none';
    cursorBefore = document.documentElement.style.cursor;
    document.documentElement.style.cursor = 'crosshair';
    window.addEventListener('mousemove', onMove, true);
    window.addEventListener('click', onClick, true);
    window.addEventListener('keydown', onKey, true);
    SWALLOWED.forEach(function (type) { window.addEventListener(type, swallow, { capture: true, passive: false }); });
  }
  function stopPicking() {
    if (!picking) return;
    picking = false;
    if (box) box.remove();
    if (tip) tip.remove();
    box = tip = current = null;
    document.documentElement.style.cursor = cursorBefore;
    window.removeEventListener('mousemove', onMove, true);
    window.removeEventListener('click', onClick, true);
    window.removeEventListener('keydown', onKey, true);
    SWALLOWED.forEach(function (type) { window.removeEventListener(type, swallow, { capture: true }); });
    releaseLayer();
  }
  function onMove(event) {
    var el = document.elementFromPoint ? document.elementFromPoint(event.clientX, event.clientY) : event.target;
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
    swallow(event);
    // The element the outline was on, else the one clicked (our layer takes no pointer events).
    var el = current || (event.target && event.target.nodeType === 1 ? event.target : null);
    stopPicking();
    if (!el || isOurs(el)) { post({ type: 'pick-cancelled' }); return; }
    var d = describe(el);
    var error = made.get(el);
    post({
      type: 'picked',
      selector: d.selector,
      fingerprint: fingerprint(el, d.used),
      src: el.getAttribute(SRC),
      created: error ? siteOf(error) : null,
      label: pickLabel(el)
    });
  }
  function onKey(event) {
    if (event.key !== 'Escape') return;
    swallow(event);
    stopPicking();
    post({ type: 'pick-cancelled' });
  }

  // --- Marks ----------------------------------------------------------------------------------
  var marks = [];
  /** The element each mark is on, found once and kept until the page's structure changes. */
  var places = new Map();
  /** The ring and dot drawn for each mark, by its id, reused as the page scrolls and changes. */
  var drawn = new Map();
  /** The thread each marked element shows, as last drawn (for its right-click menu). */
  var markedElements = new Map();
  var RANGE = /^(.+):(\d+)-(\d+)$/;
  /**
   * The element a mark is on: by its source range (among elements that share it, as the rows of a
   * list rendered from one tag do, the one its selector or fingerprint names), then its selector
   * (if the fingerprint still fits, or the selector is an id or a stable attribute that names one
   * element whatever its text says now), then its fingerprint.
   */
  function find(mark) {
    if (mark.src) {
      var m = RANGE.exec(mark.src);
      if (m) {
        var file = m[1], start = +m[2], end = +m[3];
        var all = document.querySelectorAll('[' + SRC + ']');
        // The tag whose range holds the anchor's start (a thread made on the tag), else the tags
        // that start inside the anchor (one made on a stretch of the source).
        var holds = -1, inside = -1, best = [], within = [];
        for (var i = 0; i < all.length; i++) {
          var r = RANGE.exec(all[i].getAttribute(SRC));
          if (!r || r[1] !== file) continue;
          var s = +r[2], e = +r[3];
          if (s <= start && start < e) {
            if (s > holds) { holds = s; best = [all[i]]; }
            else if (s === holds) best.push(all[i]);
          } else if (s >= start && s < end) {
            if (inside < 0 || s < inside) { inside = s; within = [all[i]]; }
            else if (s === inside) within.push(all[i]);
          }
        }
        var found = best.length ? best : within;
        if (found.length === 1) return found[0];
        if (found.length > 1) return pickAmong(found, mark) || found[0];
      }
    }
    if (mark.selector) {
      var el = null, count = 0;
      try { var hits = document.querySelectorAll(mark.selector); el = hits[0] || null; count = hits.length; } catch (e) {}
      if (el && (fits(el, mark.fingerprint) || (count === 1 && stableSelector(mark.selector)))) return el;
      if (mark.fingerprint && mark.fingerprint.tag) {
        var same = document.getElementsByTagName(mark.fingerprint.tag);
        for (var j = 0; j < same.length; j++) if (fits(same[j], mark.fingerprint, true)) return same[j];
      }
      if (el && !mark.fingerprint) return el;
    }
    return null;
  }
  /** Of elements that share a source range, the one the mark's selector names, else the one its fingerprint fits. */
  function pickAmong(elements, mark) {
    if (mark.selector) {
      try {
        var named = document.querySelectorAll(mark.selector);
        for (var i = 0; i < named.length; i++) if (elements.indexOf(named[i]) >= 0 && fits(named[i], mark.fingerprint)) return named[i];
      } catch (e) {}
    }
    for (var j = 0; j < elements.length; j++) if (fits(elements[j], mark.fingerprint, true)) return elements[j];
    return null;
  }
  function fits(el, fp, strict) {
    if (!fp) return true;
    if (fp.tag && tagOf(el) !== fp.tag) return false;
    if (!fp.text) return !strict;
    var text = textOf(el);
    return strict ? text === fp.text : text.slice(0, 20) === fp.text.slice(0, 20);
  }
  function placeOf(mark) {
    var el = places.get(mark.id);
    if (el === undefined || (el && !el.isConnected)) {
      el = find(mark);
      places.set(mark.id, el);
    }
    return el;
  }
  function onDot(event) {
    swallow(event);
    var id = event.currentTarget.getAttribute('data-id');
    if (event.type === 'click') post({ type: 'select', id: id });
    else post({ type: 'menu', id: id, x: event.clientX, y: event.clientY });
  }
  function render() {
    scheduled = false;
    if (!marks.length && !drawn.size) return;
    markedElements = new Map();
    var keep = new Set();
    for (var i = 0; i < marks.length; i++) {
      var mark = marks[i];
      var el = placeOf(mark);
      if (!el) continue;
      if (!markedElements.has(el) || mark.state === 'active') markedElements.set(el, mark.id);
      var r = el.getBoundingClientRect();
      if (r.bottom < 0 || r.top > innerHeight || r.right < 0 || r.left > innerWidth || (!r.width && !r.height)) continue;
      keep.add(mark.id);
      var pair = drawn.get(mark.id);
      if (!pair) {
        ensureLayer();
        pair = { ring: document.createElement('div'), dot: document.createElement('button') };
        pair.dot.title = 'Comment';
        pair.dot.setAttribute('data-id', mark.id);
        pair.dot.addEventListener('click', onDot);
        pair.dot.addEventListener('contextmenu', onDot);
        layer.appendChild(pair.ring); layer.appendChild(pair.dot);
        drawn.set(mark.id, pair);
      }
      pair.ring.className = 'ring ' + mark.state;
      pair.ring.style.left = r.left + 'px'; pair.ring.style.top = r.top + 'px';
      pair.ring.style.width = r.width + 'px'; pair.ring.style.height = r.height + 'px';
      pair.dot.className = 'mark' + (mark.state === 'active' ? ' active' : '');
      pair.dot.style.left = Math.min(innerWidth - 12, r.right) + 'px';
      pair.dot.style.top = Math.max(12, r.top) + 'px';
    }
    drawn.forEach(function (pair, id) {
      if (keep.has(id)) return;
      pair.ring.remove(); pair.dot.remove();
      drawn.delete(id);
    });
    releaseLayer();
  }
  var scheduled = false;
  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(render);
  }
  // Right-clicking a marked element offers its thread's Reply and Delete, as every other format does.
  window.addEventListener('contextmenu', function (event) {
    if (picking) return;
    for (var node = event.target; node && node.nodeType === 1; node = node.parentElement) {
      var id = markedElements.get(node);
      if (id) {
        event.preventDefault();
        post({ type: 'menu', id: id, x: event.clientX, y: event.clientY });
        return;
      }
    }
  }, true);
  window.addEventListener('scroll', schedule, true);
  window.addEventListener('resize', schedule);
  // The page's structure, an element's text or what identifies it changing can move a mark to
  // another element, so its place is found again; anything else (a style animating) only moves it.
  var IDENTITY = ['id', 'class', SRC].concat(STABLE_ATTRS);
  new MutationObserver(function (records) {
    var moved = false;
    for (var i = 0; i < records.length; i++) {
      var record = records[i];
      if (isOurs(record.target)) continue;
      moved = true;
      if (record.type !== 'attributes' || IDENTITY.indexOf(record.attributeName) >= 0) { places.clear(); break; }
    }
    if (moved && marks.length) schedule();
  }).observe(document.documentElement, { childList: true, subtree: true, attributes: true, characterData: true });

  function reveal(id) {
    for (var i = 0; i < marks.length; i++) {
      if (marks[i].id !== id) continue;
      var el = placeOf(marks[i]);
      if (el && el.scrollIntoView) el.scrollIntoView({ block: 'center', behavior: 'smooth' });
      return;
    }
  }

  // --- A picture of the page --------------------------------------------------------------------
  var SCREENSHOT = 'https://cdn.jsdelivr.net/npm/modern-screenshot@4.7.0/+esm';
  // Within what the server stores: 4096 pixels a side, 8 megapixels, 8 MB.
  var MAX_PICTURE_SIDE = 4096, MAX_PICTURE_HEIGHT = 3000, MAX_PICTURE_PIXELS = 8000000, MAX_PICTURE_BYTES = 8 * 1024 * 1024;
  function snapshot() {
    var root = document.documentElement;
    var width = Math.max(1, Math.min(innerWidth, MAX_PICTURE_SIDE));
    var height = Math.max(1, Math.min(Math.max(root.scrollHeight, innerHeight), MAX_PICTURE_HEIGHT, Math.floor(MAX_PICTURE_PIXELS / width)));
    var background = getComputedStyle(document.body || root).backgroundColor;
    if (!background || background === 'rgba(0, 0, 0, 0)' || background === 'transparent') background = '#ffffff';
    function draw(lib, scale) {
      return lib.domToPng(root, {
        width: width, height: height, scale: scale, backgroundColor: background,
        // Our marks and picker are not the page.
        filter: function (node) { return !isOurs(node); }
      });
    }
    import(SCREENSHOT)
      .then(function (lib) {
        return draw(lib, 1).then(function (png) {
          // A busy page can be larger as a PNG than the server keeps: half the size then.
          return png.length * 0.75 > MAX_PICTURE_BYTES ? draw(lib, 0.5) : png;
        });
      })
      .then(function (png) { post({ type: 'snapshot', png: png }); })
      .catch(function (error) { post({ type: 'snapshot-failed', message: String(error && error.message || error) }); });
  }

  window.addEventListener('message', function (event) {
    if (event.source !== window.parent || !event.data || !event.data.wiApp) return;
    var m = event.data.wiApp;
    if (m.type === 'marks' && Array.isArray(m.marks)) { marks = m.marks; places.clear(); schedule(); }
    else if (m.type === 'pick') startPicking();
    else if (m.type === 'stop-pick') stopPicking();
    else if (m.type === 'reveal') reveal(m.id);
    else if (m.type === 'snapshot') snapshot();
  });
  function ready() { post({ type: 'ready', path: pagePath }); }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', ready);
  else ready();
})();`;

/** What the bridge reports (ADR 0x050 §5). */
export type PageBridgeEvent =
  /** The page loaded; `path` is its path in its project (null for a page shown on its own). */
  | { type: 'ready'; path: string | null }
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
  /** A picture of the page as a PNG data URL, as asked for; or why there is none. */
  | { type: 'snapshot'; png: string }
  | { type: 'snapshot-failed'; message: string }
  /** A marked element (or its mark) was right-clicked, at x, y in the page's viewport. */
  | { type: 'menu'; id: string; x: number; y: number };

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
  { type: 'marks'; marks: PageMark[] } | { type: 'pick' } | { type: 'stop-pick' } | { type: 'reveal'; id: string } | { type: 'snapshot' };

/**
 * A bridge event from a message's data, or null for anything else. The page sends it, and a page is
 * code the user or an agent wrote, so every field is checked to be what its type says.
 */
export function readBridgeEvent(data: unknown): PageBridgeEvent | null {
  if (!data || typeof data !== 'object') return null;
  const e = data as Record<string, unknown>;
  const str = (v: unknown): v is string => typeof v === 'string';
  const num = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
  switch (e['type']) {
    case 'ready':
      return { type: 'ready', path: str(e['path']) ? e['path'] : null };
    case 'picked': {
      const fp = e['fingerprint'] as Record<string, unknown> | null | undefined;
      const created = e['created'] as Record<string, unknown> | null | undefined;
      if (!str(e['selector']) || !str(e['label']) || !fp || typeof fp !== 'object' || !str(fp['tag'])) return null;
      const attrs: Record<string, string> = {};
      if (fp['attrs'] && typeof fp['attrs'] === 'object') {
        for (const [k, v] of Object.entries(fp['attrs'] as Record<string, unknown>)) if (str(v)) attrs[k] = v;
      }
      return {
        type: 'picked',
        selector: e['selector'],
        fingerprint: { tag: fp['tag'], ...(str(fp['text']) ? { text: fp['text'] } : {}), attrs },
        src: str(e['src']) ? e['src'] : null,
        created:
          created && typeof created === 'object' && str(created['path']) && num(created['line']) && num(created['column'])
            ? { path: created['path'], line: created['line'], column: created['column'] }
            : null,
        label: e['label'],
      };
    }
    case 'pick-cancelled':
      return { type: 'pick-cancelled' };
    case 'select':
      return str(e['id']) ? { type: 'select', id: e['id'] } : null;
    case 'snapshot':
      return str(e['png']) ? { type: 'snapshot', png: e['png'] } : null;
    case 'snapshot-failed':
      return { type: 'snapshot-failed', message: str(e['message']) ? e['message'] : '' };
    case 'menu':
      return str(e['id']) && num(e['x']) && num(e['y']) ? { type: 'menu', id: e['id'], x: e['x'], y: e['y'] } : null;
    default:
      return null;
  }
}

/**
 * The project a page is served from, as its comments need it (ADR 0x050 §5): the files the page
 * loaded, a project path's file, and a file's text. A page on its own has none.
 */
export interface PageProject {
  servedFiles(): ReadonlyMap<string, import('../../core/models/file-attachment.model').FileAttachment>;
  fileAt(path: string): Promise<import('../../core/models/file-attachment.model').FileAttachment | null>;
  text(file: import('../../core/models/file-attachment.model').FileAttachment): Promise<string>;
  /** The line of the file the user wrote that a place in a served module is: through its source map when it was compiled (§4); null when unknown. */
  sourceLine(path: string, line: number, column: number): Promise<number | null>;
}
