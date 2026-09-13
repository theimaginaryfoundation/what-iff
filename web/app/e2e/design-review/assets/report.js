/*
 * Design review report — behaviour.
 *
 * Plain DOM, no framework, no build step. The whole page is one file that has
 * to open from a Downloads folder in five years, so the dependency count is
 * the feature.
 *
 * The one genuinely interesting thing in here is that pixel comparison is
 * done in the browser (see `computeDiff`) rather than baked into the report
 * by the generator. Diffing in Node would have meant shipping a PNG decoder
 * and producing a single fixed diff image; doing it on a canvas over images
 * the page has already loaded gives the slider, the onion-skin and an
 * adjustable sensitivity for free, and lets a reviewer answer "is that a real
 * change or an antialiasing seam?" by dragging a control instead of by
 * re-running anything.
 */

(() => {
  'use strict';

  const data = JSON.parse(document.getElementById('design-data').textContent);
  const root = document.getElementById('app');

  const state = {
    filter: data.summary.changed + data.summary.added + data.summary.removed > 0 ? 'touched' : 'all',
    viewport: 'both',
    mode: 'slider',
    // Must equal DEFAULT_THRESHOLD in lib/diff.mjs. The generator computes
    // each screen's changed-pixel figure in Node so a PR comment can quote
    // it; this page recomputes only when the slider moves. Starting from a
    // different value would make the page silently disagree with the number
    // that brought the reader here.
    threshold: 0.08,
    checker: false,
  };

  /** Mirrors DEFAULT_THRESHOLD in lib/diff.mjs — see `state.threshold`. */
  const DEFAULT_THRESHOLD = 0.08;

  /** Stage controllers, so a mode change updates in place instead of re-rendering. */
  let stages = [];

  // ---------------------------------------------------------------- helpers

  function el(tag, props = {}, ...children) {
    const node = document.createElement(tag);
    for (const [key, value] of Object.entries(props)) {
      // `aria-*`, `data-*` and `role` are attributes, not properties.
      // Object.assign would hang them off the element as inert expandos and
      // the accessibility tree would never see them.
      if (key.includes('-') || key === 'role') node.setAttribute(key, value);
      else node[key] = value;
    }
    for (const child of children.flat()) {
      if (child == null || child === false) continue;
      node.append(child.nodeType ? child : document.createTextNode(String(child)));
    }
    return node;
  }

  const pct = value => `${(value * 100).toFixed(value < 0.01 && value > 0 ? 2 : 1)}%`;
  const shortSha = sha => (sha ? sha.slice(0, 8) : '—');

  /** `a/b/c.ts` → `['a/b/', 'c.ts']`, for dimming the directory in file lists. */
  function splitPath(filePath) {
    const cut = filePath.lastIndexOf('/');
    return cut === -1 ? ['', filePath] : [filePath.slice(0, cut + 1), filePath.slice(cut + 1)];
  }

  /**
   * Longest directory prefix shared by every path given.
   *
   * Hoisted out of the rows and shown once above them. In this repo every
   * frontend path starts `web/app/src/app/…`, which is thirteen characters
   * of nothing repeated on every line — and it is precisely the part that
   * pushes the filename, the part a reader is actually scanning for, off
   * the end of the row.
   */
  function commonPrefix(paths) {
    if (paths.length < 2) return '';
    const segments = paths[0].split('/').slice(0, -1);
    let shared = segments.length;
    for (const candidate of paths.slice(1)) {
      const parts = candidate.split('/').slice(0, -1);
      let index = 0;
      while (index < shared && index < parts.length && parts[index] === segments[index]) index++;
      shared = index;
      if (!shared) return '';
    }
    return shared ? `${segments.slice(0, shared).join('/')}/` : '';
  }

  const STATUS_LABEL = { changed: 'Changed', added: 'New', removed: 'Removed', unchanged: 'Unchanged' };

  /** Screens matching the current filter. */
  function visibleScreens() {
    return data.screens.filter(screen => {
      if (state.filter === 'all') return true;
      if (state.filter === 'touched') return screen.status !== 'unchanged';
      return screen.status === state.filter;
    });
  }

  /** Variants of a screen matching the current viewport filter. */
  function visibleVariants(screen) {
    return screen.variants.filter(variant => state.viewport === 'both' || variant.form === state.viewport);
  }

  // ------------------------------------------------------------- masthead

  function renderMasthead() {
    const heading = data.pr
      ? el('h1', {}, el('a', { href: data.pr.url, target: '_blank', rel: 'noopener' }, `#${data.pr.number} ${data.pr.title}`))
      : el('h1', {}, data.head.branch || data.head.ref);

    const refs = el(
      'div',
      { className: 'refline' },
      el('span', {}, data.base.ref),
      el('span', { className: 'sha' }, shortSha(data.base.sha)),
      el('span', { className: 'arrow' }, '→'),
      el('span', {}, data.head.worktree ? 'working tree' : data.head.ref),
      el('span', { className: 'sha' }, shortSha(data.head.sha)),
      data.head.subject ? el('span', {}, `· ${data.head.subject}`) : null,
      data.pr?.author ? el('span', {}, `· @${data.pr.author}`) : null,
    );

    const summary = data.summary;
    const tiles = el(
      'div',
      { className: 'tiles' },
      tile('changed', summary.changed, 'screens changed'),
      summary.added ? tile('added', summary.added, 'new screens') : null,
      summary.removed ? tile('removed', summary.removed, 'screens removed') : null,
      tile('unchanged', summary.unchanged, 'unchanged'),
      tile('surface', pct(data.impact.designShare), 'of frontend files are design surface'),
      tile('', data.impact.frontendFileCount, 'frontend files touched'),
    );

    return el(
      'header',
      { className: 'masthead' },
      el('p', { className: 'eyebrow' }, 'Visual design review'),
      heading,
      refs,
      tiles,
    );
  }

  function tile(kind, value, label) {
    return el('div', { className: `tile ${kind}` }, el('div', { className: 'n' }, value), el('div', { className: 'k' }, label));
  }

  // -------------------------------------------------------------- toolbar

  function segmented(label, options, current, onPick) {
    const seg = el('div', { className: 'seg', role: 'group', 'aria-label': label });
    for (const option of options) {
      const button = el(
        'button',
        { type: 'button' },
        option.label,
        option.count != null ? el('span', { className: 'count' }, option.count) : null,
      );
      button.setAttribute('aria-pressed', String(option.id === current()));
      button.addEventListener('click', () => {
        onPick(option.id);
        for (const sibling of seg.children) sibling.setAttribute('aria-pressed', String(sibling === button));
      });
      seg.append(button);
    }
    return el('div', { className: 'group' }, el('span', { className: 'lbl' }, label), seg);
  }

  function countBy(predicate) {
    return data.screens.filter(predicate).length;
  }

  function renderToolbar() {
    const touched = countBy(screen => screen.status !== 'unchanged');

    const filters = segmented(
      'Show',
      [
        { id: 'touched', label: 'Changed', count: touched },
        { id: 'all', label: 'All', count: data.screens.length },
        { id: 'unchanged', label: 'Unchanged', count: countBy(screen => screen.status === 'unchanged') },
      ],
      () => state.filter,
      value => {
        state.filter = value;
        renderScreens();
      },
    );

    const viewports = segmented(
      'Viewport',
      [
        { id: 'both', label: 'Both' },
        { id: 'desktop', label: 'Desktop' },
        { id: 'mobile', label: 'Mobile' },
      ],
      () => state.viewport,
      value => {
        state.viewport = value;
        renderScreens();
      },
    );

    const modes = segmented(
      'Compare',
      [
        { id: 'slider', label: 'Slider' },
        { id: 'sbs', label: 'Side by side' },
        { id: 'onion', label: 'Onion' },
        { id: 'diff', label: 'Difference' },
      ],
      () => state.mode,
      value => {
        state.mode = value;
        for (const stage of stages) stage.setMode(value);
      },
    );

    const sensitivity = el('input', { type: 'range', min: '0', max: '40', value: String(state.threshold * 100), title: 'Diff sensitivity' });
    // Debounced: `input` fires for every pixel of a drag, and each one would
    // otherwise re-compare every visible screen at full resolution. A short
    // delay still feels live while collapsing a whole gesture into one pass.
    let pending;
    sensitivity.addEventListener('input', () => {
      state.threshold = Number(sensitivity.value) / 100;
      clearTimeout(pending);
      pending = setTimeout(() => {
        for (const stage of stages) stage.setThreshold(state.threshold);
      }, 120);
    });

    const checkerSeg = segmented(
      'Backdrop',
      [
        { id: false, label: 'Plain' },
        { id: true, label: 'Checker' },
      ],
      () => state.checker,
      value => {
        state.checker = value;
        for (const stage of stages) stage.setChecker(value);
      },
    );

    const theme = segmented(
      'Theme',
      [
        { id: 'dark', label: 'Dark' },
        { id: 'light', label: 'Light' },
      ],
      () => document.documentElement.dataset.theme,
      value => {
        document.documentElement.dataset.theme = value;
      },
    );

    return el(
      'div',
      { className: 'toolbar' },
      filters,
      viewports,
      modes,
      el('div', { className: 'group' }, el('span', { className: 'lbl' }, 'Sensitivity'), sensitivity),
      checkerSeg,
      el('div', { className: 'spacer' }),
      theme,
    );
  }

  // --------------------------------------------------------------- screens

  function renderScreens() {
    stages = [];
    const host = document.getElementById('screens');
    host.textContent = '';
    const screens = visibleScreens();
    if (!screens.length) {
      host.append(el('div', { className: 'nothing' }, 'No screens match this filter.'));
      return;
    }
    for (const screen of screens) host.append(renderCard(screen));
  }

  function renderCard(screen) {
    const variants = visibleVariants(screen);
    const header = el(
      'header',
      {},
      el('h2', {}, screen.title),
      screen.group ? el('span', { className: 'group-name' }, screen.group) : null,
      el('span', { className: `badge ${screen.status}` }, STATUS_LABEL[screen.status]),
      el('span', { className: 'spec' }, splitPath(screen.specPath)[1]),
    );

    const viewports = el('div', { className: `viewports${variants.length > 1 ? ' pair' : ''}` });
    for (const variant of variants) viewports.append(renderViewport(screen, variant));

    return el(
      'section',
      { className: 'card' },
      header,
      screen.note ? el('div', { className: 'note' }, screen.note) : null,
      screen.related.length ? renderRelated(screen) : null,
      variants.length ? viewports : el('div', { className: 'nothing' }, 'No baseline for this viewport.'),
    );
  }

  function renderRelated(screen) {
    return el(
      'details',
      { className: 'related' },
      el('summary', {}, `${screen.related.length} changed file${screen.related.length === 1 ? '' : 's'} mention this screen`),
      el('ul', {}, screen.related.map(filePath => el('li', {}, el('span', { className: 'dir' }, splitPath(filePath)[0]), splitPath(filePath)[1]))),
      el('p', { className: 'hint' }, 'Matched by name, not by imports — a shared component that renders this screen may not be listed.'),
    );
  }

  function renderViewport(screen, variant) {
    const delta = el('span', { className: 'delta' }, variant.status === 'unchanged' ? 'identical' : '');
    const head = el(
      'div',
      { className: 'vhead' },
      el('strong', {}, variant.label),
      variant.device ? el('span', { className: 'device' }, variant.device) : null,
      el('span', { className: `badge ${variant.status}` }, STATUS_LABEL[variant.status]),
      delta,
    );

    const holder = el('div', {});
    const controller = mountStage(holder, variant, delta);
    stages.push(controller);
    return el('div', { className: 'viewport', 'data-form': variant.form }, head, holder);
  }

  // ------------------------------------------------------- comparison stage

  /**
   * Positions one image inside a box sized to the *larger* of the two sides,
   * anchored top-left at its true scale.
   *
   * Stretching both images to fill the box would be the easy thing and the
   * wrong thing: when a change makes a page taller, stretching hides exactly
   * that — the two would overlay perfectly and the diff would light up every
   * element below the insertion point as if it had been restyled. Anchored
   * top-left, a height change reads as what it is, with the shorter side
   * ending early against the backdrop.
   */
  function placeLayer(node, image, boxWidth, boxHeight) {
    node.style.inset = '0 auto auto 0';
    node.style.width = `${((image.width ?? boxWidth) / boxWidth) * 100}%`;
    node.style.height = `${((image.height ?? boxHeight) / boxHeight) * 100}%`;
  }

  function imageLayer(image, boxWidth, boxHeight, extraClass) {
    // `draggable` as well as the CSS rule: the attribute is what actually
    // stops a native image drag in Firefox, where `-webkit-user-drag` does
    // nothing, and a started image drag cancels the comparison gesture.
    const node = el('img', { src: data.assets[image.asset], alt: '', loading: 'lazy', decoding: 'async', draggable: false });
    if (extraClass) node.className = extraClass;
    placeLayer(node, image, boxWidth, boxHeight);
    return node;
  }

  /** A hatched placeholder for the side of a comparison that does not exist. */
  function missingLayer(text) {
    return el('div', { className: 'empty-side' }, text);
  }

  /**
   * Decodes both sides into pixel buffers once, on a canvas sized to the
   * union of the two.
   *
   * Both are drawn at natural size into the top-left of a common box, so a
   * page that grew taller compares like-for-like down to where the shorter
   * side ends, and the region only one side covers is counted as changed —
   * which is true, and is usually the most important part of the change.
   */
  function pixelReader(variant) {
    let pending = null;
    return () => {
      if (pending) return pending;
      pending = (async () => {
        const boxWidth = Math.max(variant.before?.width ?? 0, variant.after?.width ?? 0);
        const boxHeight = Math.max(variant.before?.height ?? 0, variant.after?.height ?? 0);
        if (!boxWidth || !boxHeight) throw new Error('unknown image dimensions');

        const read = async image => {
          const canvas = document.createElement('canvas');
          canvas.width = boxWidth;
          canvas.height = boxHeight;
          const context = canvas.getContext('2d', { willReadFrequently: true });
          if (image) {
            const bitmap = new Image();
            bitmap.src = data.assets[image.asset];
            await bitmap.decode();
            context.drawImage(bitmap, 0, 0);
          }
          return context.getImageData(0, 0, boxWidth, boxHeight);
        };

        return { width: boxWidth, height: boxHeight, before: await read(variant.before), after: await read(variant.after) };
      })();
      return pending;
    };
  }

  /**
   * Per-pixel comparison at a given sensitivity, returning both the headline
   * ratio and an overlay to paint.
   *
   * The test is max-per-channel rather than a Euclidean distance in RGB: a
   * designer's "did this colour change" question is about the largest shift
   * in any one channel, and a distance metric lets a big change in one
   * channel hide under small ones in the others.
   *
   * Alpha is compared too, because a screenshot that went from opaque to
   * translucent in a region is a real change that an RGB-only test over
   * premultiplied-looking data can miss entirely.
   */
  function computeDiff(pixels, threshold) {
    const { width, height, before, after } = pixels;
    const cutoff = Math.round(threshold * 255);
    const overlay = new ImageData(width, height);
    const a = before.data;
    const b = after.data;
    const out = overlay.data;
    let changed = 0;

    for (let i = 0; i < a.length; i += 4) {
      const delta = Math.max(Math.abs(a[i] - b[i]), Math.abs(a[i + 1] - b[i + 1]), Math.abs(a[i + 2] - b[i + 2]), Math.abs(a[i + 3] - b[i + 3]));
      if (delta > cutoff) {
        changed++;
        // Solid magenta: the one hue that appears in almost no real UI, so
        // an overlay pixel is never mistaken for content underneath it.
        out[i] = 255;
        out[i + 1] = 32;
        out[i + 2] = 168;
        out[i + 3] = 255;
      } else {
        // Unchanged regions are kept as a dim greyscale of the "after" image
        // so the highlighted pixels stay locatable — a diff on a black field
        // tells you how much changed but not where.
        const grey = (b[i] * 0.299 + b[i + 1] * 0.587 + b[i + 2] * 0.114) * 0.32;
        out[i] = out[i + 1] = out[i + 2] = grey;
        out[i + 3] = 255;
      }
    }

    return { ratio: changed / (width * height), changed, total: width * height, overlay };
  }

  /**
   * Builds one comparison and returns the handles the toolbar drives it with.
   *
   * Modes share a single set of layers rather than re-rendering, so dragging
   * the slider, flipping to onion and back, and nudging sensitivity all keep
   * their positions — which is the entire way this gets used in practice
   * (park the slider on the bit you care about, then flip modes).
   */
  /**
   * Tallest a screenshot is allowed to render at, in CSS pixels.
   *
   * This is what makes a phone look like a phone. Left to fill its grid
   * column, a 412x915 mobile baseline renders *larger* than a 1280x720
   * desktop one beside it — the reader's first impression of the pair is
   * then exactly backwards. Capping height instead of width sizes both by
   * the one dimension they share, so the desktop shot stays wide and the
   * mobile one stays narrow, in the proportion the devices actually have.
   */
  const MAX_STAGE_HEIGHT = 620;

  /** Sizes a stage from its image's true aspect ratio, under the height cap. */
  function sizeStage(node, boxWidth, boxHeight) {
    node.style.aspectRatio = `${boxWidth} / ${boxHeight}`;
    node.style.maxWidth = `${Math.round((MAX_STAGE_HEIGHT * boxWidth) / boxHeight)}px`;
  }

  function mountStage(holder, variant, deltaNode) {
    const boxWidth = Math.max(variant.before?.width ?? 0, variant.after?.width ?? 0) || 1;
    const boxHeight = Math.max(variant.before?.height ?? 0, variant.after?.height ?? 0) || 1;
    const comparable = Boolean(variant.before && variant.after && variant.status !== 'unchanged');

    const stage = el('div', { className: 'stage' });
    sizeStage(stage, boxWidth, boxHeight);

    // An unchanged or one-sided screen has nothing to compare; it renders as
    // a plain screenshot and ignores the compare controls entirely rather
    // than offering a slider that does nothing.
    if (!comparable) {
      const single = variant.after ?? variant.before;
      stage.append(single ? imageLayer(single, boxWidth, boxHeight) : missingLayer('no baseline'));
      holder.append(stage);
      return { setMode() {}, setThreshold() {}, setChecker: on => stage.classList.toggle('checker', on) };
    }

    const beforeLayer = imageLayer(variant.before, boxWidth, boxHeight);
    const clip = el('div', { className: 'clip' });
    const afterLayer = imageLayer(variant.after, boxWidth, boxHeight);
    clip.append(afterLayer);
    const handle = el('div', { className: 'handle' });
    const canvas = el('canvas', {});
    canvas.width = boxWidth;
    canvas.height = boxHeight;
    canvas.hidden = true;
    stage.append(beforeLayer, clip, handle, canvas);

    // Side-by-side is a different box shape, so it gets its own subtree that
    // is swapped in wholesale rather than being coaxed out of the stack.
    const sideBySide = el(
      'div',
      { className: 'sbs' },
      el('figure', { className: 'before' }, el('figcaption', {}, 'Before'), sideStage(variant.before, boxWidth, boxHeight, 'no before')),
      el('figure', { className: 'after' }, el('figcaption', {}, 'After'), sideStage(variant.after, boxWidth, boxHeight, 'no after')),
    );
    sideBySide.hidden = true;
    holder.append(stage, sideBySide);

    let position = 0.5;
    let mode = state.mode;
    let threshold = state.threshold;
    let pixels = null;
    const readPixels = pixelReader(variant);

    function applyPosition() {
      handle.style.left = `${position * 100}%`;
      if (mode === 'slider') {
        // Reveal by clipping, not by resizing. The layer keeps the full
        // stage box so it stays pixel-aligned with the "before" underneath;
        // only how much of it is painted changes.
        clip.style.clipPath = `inset(0 ${(1 - position) * 100}% 0 0)`;
        clip.style.opacity = '1';
      } else if (mode === 'onion') {
        clip.style.clipPath = 'none';
        clip.style.opacity = String(position);
      }
    }

    /** Shows a changed-pixel figure, flagging it when it is not the default. */
    function showDelta(ratio, custom) {
      deltaNode.textContent = `${pct(ratio)} of pixels differ${custom ? ' at this sensitivity' : ''}`;
      deltaNode.classList.toggle('hot', ratio > 0.005);
    }

    /** Repaints the difference overlay and the header's changed-pixel figure. */
    async function refreshDiff() {
      if (!pixels) {
        try {
          pixels = await readPixels();
        } catch (error) {
          // Surfaced in the UI *and* logged. The UI text is all a reader
          // needs, but a malformed baseline is a repository problem someone
          // will have to go and look at, and "diff unavailable" on its own
          // does not say which of eleven screens is the broken one.
          console.warn(`[design review] could not compare ${variant.path}:`, error);
          deltaNode.textContent = 'diff unavailable';
          return;
        }
      }
      const result = computeDiff(pixels, threshold);
      canvas.getContext('2d').putImageData(result.overlay, 0, 0);
      showDelta(result.ratio, threshold !== DEFAULT_THRESHOLD);
    }

    function applyMode() {
      const showing = mode === 'sbs';
      sideBySide.hidden = !showing;
      stage.hidden = showing;
      const diffing = mode === 'diff';
      canvas.hidden = !diffing;
      clip.hidden = diffing;
      beforeLayer.hidden = diffing;
      handle.hidden = diffing;
      stage.classList.toggle('slider', mode === 'slider' || mode === 'onion');
      applyPosition();
      if (diffing) void refreshDiff();
    }

    // Dragging anywhere on the stage moves the comparison point — no separate
    // handle to hit. In onion mode the same gesture cross-fades, so the
    // muscle memory is the same in both.
    const track = event => {
      const box = stage.getBoundingClientRect();
      position = Math.min(1, Math.max(0, (event.clientX - box.left) / box.width));
      applyPosition();
    };
    // An explicit flag rather than asking `hasPointerCapture` on every move.
    // Capture answers "does this element own the pointer", which is not the
    // same question as "is the user dragging" — the browser can take the
    // pointer away mid-gesture (see `pointercancel` below), and reading
    // capture state left the handler silently dead with no way to recover.
    let dragging = false;
    const endDrag = () => {
      dragging = false;
    };

    stage.addEventListener('pointerdown', event => {
      if (mode !== 'slider' && mode !== 'onion') return;
      dragging = true;
      // Capture is still requested so a drag continuing outside the stage
      // keeps tracking; it is no longer what the move handler trusts.
      stage.setPointerCapture?.(event.pointerId);
      track(event);
    });
    stage.addEventListener('pointermove', event => {
      if (dragging) track(event);
    });
    for (const ending of ['pointerup', 'pointercancel', 'lostpointercapture']) {
      stage.addEventListener(ending, endDrag);
    }

    // The headline figure comes from the model, computed by the generator
    // with the same rule this page uses. Canvas work is therefore deferred
    // until someone actually asks for the overlay or moves the slider —
    // which is what keeps a report of a dozen screens from decoding and
    // comparing every baseline before its first paint.
    if (variant.diff) showDelta(variant.diff.ratio, false);
    else deltaNode.textContent = 'changed';

    applyMode();

    return {
      setMode(next) {
        mode = next;
        applyMode();
      },
      setThreshold(next) {
        threshold = next;
        // Returning to the default is answered from the model rather than by
        // decoding images to re-derive a number that shipped with the page.
        // Any other value is an explicit request and gets a real comparison,
        // even on a stage that has never been compared before.
        if (next === DEFAULT_THRESHOLD && variant.diff && !pixels) showDelta(variant.diff.ratio, false);
        else void refreshDiff();
      },
      setChecker(on) {
        stage.classList.toggle('checker', on);
        for (const node of sideBySide.querySelectorAll('.stage')) node.classList.toggle('checker', on);
      },
    };
  }

  /** One half of the side-by-side view, or a hatched panel if that side is absent. */
  function sideStage(image, boxWidth, boxHeight, missingText) {
    const node = el('div', { className: 'stage' });
    sizeStage(node, boxWidth, boxHeight);
    node.append(image ? imageLayer(image, boxWidth, boxHeight) : missingLayer(missingText));
    return node;
  }

  // ---------------------------------------------------------------- impact

  /**
   * A horizontal bar, scaled against the largest value in its own group.
   *
   * Relative rather than absolute scaling because these groups measure
   * different things — file counts next to line churn — and the only
   * comparison that means anything is within a group.
   */
  function bar(name, value, max, label) {
    return el(
      'div',
      { className: 'bar' },
      el('span', { className: 'name' }, name),
      el('span', { className: 'val' }, label),
      el('div', { className: 'track' }, Object.assign(el('div', { className: 'fill' }), { style: `width: ${max ? (value / max) * 100 : 0}%` })),
    );
  }

  function renderImpact() {
    const { byCategory, byArea, files } = data.impact;

    const categoryMax = Math.max(1, ...byCategory.map(entry => entry.count));
    const categories = el(
      'div',
      { className: 'panel' },
      el('h3', {}, 'What changed'),
      el(
        'div',
        { className: 'bars' },
        byCategory.map(entry =>
          bar(entry.label, entry.count, categoryMax, `${entry.count} file${entry.count === 1 ? '' : 's'}${entry.added + entry.deleted ? ` · ${entry.added + entry.deleted} lines` : ''}`),
        ),
      ),
    );

    const areaMax = Math.max(1, ...byArea.map(entry => entry.churn));
    const areas = el(
      'div',
      { className: 'panel' },
      el('h3', {}, 'Feature areas touched'),
      byArea.length
        ? el(
            'div',
            { className: 'bars' },
            byArea.map(entry => bar(entry.area, entry.churn, areaMax, `${entry.churn} lines · ${pct(entry.designWeight)} surface`)),
          )
        : el('p', { className: 'hint' }, 'No frontend source files changed.'),
      el('p', { className: 'hint' }, '“Surface” is the share of an area’s changed files that are templates, styles or assets rather than logic.'),
    );

    // The file list is limited to what a designer can act on. A PR that also
    // rewrites a Go handler is not more interesting for listing the handler
    // here; it is less readable.
    const designFiles = files.filter(file => ['template', 'style', 'asset', 'component', 'baseline'].includes(file.category));
    const prefix = commonPrefix(designFiles.map(file => file.path));
    const fileList = el(
      'div',
      { className: 'panel' },
      el('h3', {}, `Design-relevant files (${designFiles.length})`),
      prefix ? el('p', { className: 'prefix' }, prefix) : null,
      designFiles.length
        ? el(
            'ul',
            { className: 'files' },
            designFiles.map(file => {
              const [dir, name] = splitPath(file.path.slice(prefix.length));
              return el(
                'li',
                { title: `${file.path} — ${file.status}` },
                el('span', { className: `st ${file.status}` }),
                el('span', { className: 'name' }, name),
                el('span', { className: 'dir' }, dir),
                el(
                  'span',
                  { className: 'churn' },
                  file.added == null
                    ? el('span', { className: 'dir' }, 'binary')
                    : [el('span', { className: 'add' }, `+${file.added}`), ' ', el('span', { className: 'del' }, `−${file.deleted}`)],
                ),
              );
            }),
          )
        : el('p', { className: 'hint' }, 'Nothing under the app’s templates, styles or assets changed.'),
    );

    return el(
      'section',
      { className: 'impact' },
      el('h2', {}, 'Impact'),
      el('p', { className: 'sub' }, 'Where the change lands in the codebase, for the conversation that follows “does this look right?”.'),
      el('div', { className: 'impact-grid' }, categories, areas, fileList),
      renderUncovered(),
    );
  }

  /**
   * The gap panel: design files that changed with no screen above showing
   * them. Rendered full-width and last, because it is the part of the report
   * that asks for a decision rather than reporting a fact.
   */
  function renderUncovered() {
    const uncovered = data.impact.uncovered ?? [];
    if (!uncovered.length) return null;
    const prefix = commonPrefix(uncovered.map(file => file.path));
    return el(
      'div',
      { className: 'panel gap' },
      el('h3', {}, `Changed, but not shown above (${uncovered.length})`),
      el(
        'p',
        { className: 'hint' },
        'These templates, styles and assets changed, but no screen in this report appears to render them — so nothing here proves what they now look like. Each one is either covered by a screen whose name does not resemble it, or a candidate for a new visual spec.',
      ),
      prefix ? el('p', { className: 'prefix' }, prefix) : null,
      el(
        'ul',
        { className: 'files' },
        uncovered.map(file => {
          const [dir, name] = splitPath(file.path.slice(prefix.length));
          return el(
            'li',
            { title: file.path },
            el('span', { className: 'st modified' }),
            el('span', { className: 'name' }, name),
            el('span', { className: 'dir' }, dir),
            el('span', { className: 'churn' }, file.area ?? ''),
          );
        }),
      ),
    );
  }

  function renderFooter() {
    const covered = data.screens.length;
    return el(
      'footer',
      { className: 'meta' },
      el('span', {}, `Generated ${new Date(data.generatedAt).toLocaleString()}`),
      el('span', {}, `${covered} screen${covered === 1 ? '' : 's'} under visual coverage`),
      el('span', {}, 'Before = baseline committed at the merge base. After = baseline on this branch.'),
      data.base.sha ? null : el('span', {}, 'No merge base found — showing the current state only.'),
    );
  }

  // ----------------------------------------------------------------- boot

  root.append(
    el(
      'div',
      { className: 'wrap' },
      renderMasthead(),
      renderToolbar(),
      el('div', { className: 'screens', id: 'screens' }),
      renderImpact(),
      renderFooter(),
    ),
  );
  renderScreens();
  root.removeAttribute('aria-busy');
})();
