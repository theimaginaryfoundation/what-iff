/*
 * The preview site's host page (ADR 0x050 §3): the app frames index.html?t=<token>, sends one
 * `whatiff-preview-init` message with a MessagePort and the entry path, and then talks over that
 * port only. The user's page runs in the inner frame at ./p/<token>/<entry>; the service worker
 * asks this page for each file it serves, and this page asks the app.
 *
 * Messages from the app:   { type: 'file', id, status, contentType?, body?, message? }  (an answer)
 *                          { type: 'reload' } | { type: 'navigate', entry }
 *                          { type: 'bridge', data }  (for the page's bridge script, ADR 0x050 §5)
 * Messages to the app:     { type: 'ready' } | { type: 'get', id, path, destination } | { type: 'loaded', path }
 *                          | { type: 'failed', message } | { type: 'bridge', data }  (from the bridge)
 */
'use strict';

(function () {
  const token = new URLSearchParams(location.search).get('t') || '';
  const allowed = window.WHATIFF_PREVIEW_HOST_ORIGINS || [];
  const frame = document.getElementById('page');
  const status = document.getElementById('status');
  /** The port to the app, once it has introduced itself. */
  let app = null;
  let entry = null;
  let workerReady = false;
  let nextId = 1;
  /** Requests from the worker waiting for the app, by id. */
  const pending = new Map();

  function say(text) {
    status.textContent = text;
    status.hidden = !text;
  }

  if (!/^[A-Za-z0-9_-]{16,64}$/.test(token)) {
    say('This preview was opened without its key.');
    return;
  }
  if (!('serviceWorker' in navigator)) {
    say('This browser cannot show previews here (no service workers, as in some private windows).');
    return;
  }

  // Nothing carries over between previews: each starts with the origin's storage empty, as the page
  // it shows may be another project's.
  try {
    localStorage.clear();
    sessionStorage.clear();
  } catch {
    // Storage may be blocked; the page then simply has none.
  }

  navigator.serviceWorker.onmessage = event => {
    const data = event.data || {};
    const reply = event.ports && event.ports[0];
    if (data.type !== 'whatiff-preview-get' || data.token !== token || !reply) return;
    if (!app) {
      reply.postMessage({ status: 503, message: 'The app is not connected.' });
      return;
    }
    const id = nextId++;
    pending.set(id, reply);
    app.postMessage({ type: 'get', id, path: data.path, destination: data.destination || '' });
  };

  window.addEventListener('message', event => {
    // The page's bridge script, relayed to the app as it is.
    if (event.source === frame.contentWindow && event.origin === location.origin) {
      if (app && event.data && event.data.wiBridge) app.postMessage({ type: 'bridge', data: event.data.wiBridge });
      return;
    }
    if (event.source !== window.parent || !originAllowed(event.origin)) return;
    const data = event.data || {};
    if (data.type !== 'whatiff-preview-init' || app || !event.ports || !event.ports[0]) return;
    app = event.ports[0];
    app.onmessage = fromApp;
    entry = typeof data.entry === 'string' ? data.entry : 'index.html';
    if (workerReady) start();
  });

  function originAllowed(origin) {
    return allowed.includes('*') || allowed.includes(origin);
  }

  function fromApp(event) {
    const data = event.data || {};
    if (data.type === 'file') {
      const reply = pending.get(data.id);
      if (!reply) return;
      pending.delete(data.id);
      const body = data.body instanceof ArrayBuffer ? data.body : null;
      reply.postMessage({ status: data.status, contentType: data.contentType, body, message: data.message, location: data.location }, body ? [body] : []);
    } else if (data.type === 'reload') {
      reload();
    } else if (data.type === 'bridge' && data.data) {
      try {
        frame.contentWindow.postMessage({ wiApp: data.data }, location.origin);
      } catch {
        // A page that navigated off the site has no bridge of ours.
      }
    } else if (data.type === 'navigate' && typeof data.entry === 'string') {
      entry = data.entry;
      start();
    }
  }

  function pageUrl(path) {
    return `p/${token}/${path.split('/').map(encodeURIComponent).join('/')}`;
  }

  function start() {
    say('');
    frame.src = pageUrl(entry);
  }

  function reload() {
    try {
      frame.contentWindow.location.reload();
    } catch {
      start();
    }
  }

  frame.addEventListener('load', () => {
    if (!app) return;
    let path = entry;
    try {
      const prefix = new URL(`p/${token}/`, location.href).pathname;
      const shown = frame.contentWindow.location.pathname;
      if (shown.startsWith(prefix)) path = decodeURIComponent(shown.slice(prefix.length));
    } catch {
      // A page that navigated off the site is not ours to read.
    }
    app.postMessage({ type: 'loaded', path });
  });

  navigator.serviceWorker
    .register('sw.js', { scope: './' })
    .then(() => navigator.serviceWorker.ready)
    .then(() => {
      workerReady = true;
      if (app) start();
      else say('Waiting for the app…');
      window.parent.postMessage({ type: 'whatiff-preview-ready', token }, '*');
    })
    .catch(error => {
      say('The preview could not start.');
      if (app) app.postMessage({ type: 'failed', message: String(error && error.message ? error.message : error) });
    });
})();
