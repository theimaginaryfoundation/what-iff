/**
 * Renders the collected model as one self-contained HTML file.
 *
 * "Self-contained" is the requirement that drives everything else here.
 * The audience for this report is a designer who is not going to clone the
 * repo, and the delivery mechanism is going to be a Slack message or a
 * drag onto a browser window — so images are inlined as data URIs and there
 * is no external stylesheet, no CDN, and no fetch. One file, opens offline,
 * survives being forwarded.
 *
 * The cost of that is size, which is why identical images are stored once
 * (see `assetTable`): most screens in a typical PR are unchanged, and an
 * unchanged screen would otherwise embed the same PNG twice.
 */

import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ASSET_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'assets');

/**
 * The report's stylesheet and behaviour live in `assets/` as real `.css` and
 * `.js` files, inlined at render time, rather than as template literals in
 * this module.
 *
 * Same output either way; the difference is that an editor gives them syntax
 * highlighting and a formatter, and a template literal makes every backtick
 * and `${` in the source something you have to remember to escape. The
 * report is a UI a designer is going to look at closely, so the files that
 * define how it looks should be as easy to work on as any other UI.
 */
async function inlineAsset(name) {
  return readFile(path.join(ASSET_DIR, name), 'utf8');
}

/**
 * Collapses every image in the model to a deduplicated table of data URIs,
 * replacing each `{ buffer }` with an index into it.
 *
 * Deduplication is by content hash rather than by path, which matters for
 * the common case: an unchanged screen's before and after are the same
 * bytes at the same path, and a changed screen may still share a viewport
 * with another shot.
 */
function assetTable(screens) {
  const assets = [];
  const indexByHash = new Map();

  const intern = image => {
    if (!image) return null;
    const hash = createHash('sha1').update(image.buffer).digest('hex');
    if (!indexByHash.has(hash)) {
      indexByHash.set(hash, assets.length);
      assets.push(`data:image/png;base64,${image.buffer.toString('base64')}`);
    }
    return { asset: indexByHash.get(hash), path: image.path, bytes: image.bytes, width: image.width, height: image.height };
  };

  const lean = screens.map(screen => ({
    ...screen,
    variants: screen.variants.map(variant => ({ ...variant, before: intern(variant.before), after: intern(variant.after) })),
  }));

  return { assets, screens: lean };
}

/**
 * Embeds a value as JSON inside a `<script type="application/json">`.
 *
 * `<` is escaped rather than only the `</script>` sequence. The HTML parser
 * ends a script block on `</script` in any casing and with arbitrary
 * whitespace before the `>`, and there are enough near-misses (`<!--`
 * starting a comment inside a script) that escaping the one character all
 * of them begin with is the only version of this that is obviously correct.
 */
function embedJson(value) {
  return JSON.stringify(value).replace(/</g, '\\u003c');
}

export async function render(model) {
  const { assets, screens } = assetTable(model.screens);
  const payload = { ...model, screens, assets, repoRoot: undefined };

  return `<!doctype html>
<html lang="en" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(reportTitle(model))}</title>
<style>${await inlineAsset('report.css')}</style>
</head>
<body>
<div id="app" aria-busy="true"></div>
<script type="application/json" id="design-data">${embedJson(payload)}</script>
<script>${await inlineAsset('report.js')}</script>
</body>
</html>
`;
}

export function reportTitle(model) {
  if (model.pr) return `Design review — PR #${model.pr.number}: ${model.pr.title}`;
  if (model.head.branch) return `Design review — ${model.head.branch}`;
  return 'Design review';
}

function escapeHtml(text) {
  return String(text).replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[character]);
}
