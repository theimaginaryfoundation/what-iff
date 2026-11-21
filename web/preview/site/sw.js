/*
 * The preview site's service worker (ADR 0x050 §3).
 *
 * It serves every request under ./p/<token>/ by asking the host page that owns the token, which asks
 * the app; nothing is kept here. A browser stops an idle worker and drops its memory, so a worker
 * that held the files would lose them; asking on each request keeps it stateless, and a pending
 * request keeps it alive until it is answered.
 *
 * The host page is the window client whose URL carries `t=<token>` (index.html?t=<token>).
 *
 * The site's content security policy (set where it is hosted) reaches the host page only; the pages
 * this worker builds carry the same policy, read from the host page's headers, with 'self' added to
 * frame-ancestors, as the user's page is framed by the host page. Where the site has none (local
 * development), neither do the pages; where it cannot be read, no page is served (503), so a page
 * never runs without the policy the site meant it to have.
 */
'use strict';

const PREFIX = new URL('./p/', self.registration.scope).pathname;
/**
 * How long a request may wait for the app before it is answered 504: long enough for the first
 * TypeScript file of a session, which waits for the compiler to download (ADR 0x050 §4).
 */
const ANSWER_TIMEOUT_MS = 60000;
/** The site's policy for the pages it serves, once read ('' when the site has none). */
let pagePolicy = null;

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));

self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin || !url.pathname.startsWith(PREFIX)) return;
  const rest = url.pathname.slice(PREFIX.length);
  const slash = rest.indexOf('/');
  if (slash <= 0) return;
  const token = rest.slice(0, slash);
  let path = decodePath(rest.slice(slash + 1));
  if (path === null) {
    event.respondWith(new Response('Bad path', { status: 400 }));
    return;
  }
  if (path === '' || path.endsWith('/')) path += 'index.html';
  event.respondWith(answer(token, path, event.request));
});

function decodePath(raw) {
  try {
    const path = raw
      .split('/')
      .map(segment => decodeURIComponent(segment))
      .join('/');
    // No way out of the project: a ".." segment is refused rather than resolved.
    return path.split('/').some(segment => segment === '..') ? null : path;
  } catch {
    return null;
  }
}

async function answer(token, path, request) {
  const method = request.method;
  if (method !== 'GET' && method !== 'HEAD') {
    return new Response('The preview serves files only.', { status: 405 });
  }
  const host = await hostFor(token);
  if (!host) return new Response('This preview has closed.', { status: 410, headers: { 'Content-Type': 'text/plain' } });
  const reply = await ask(host, { type: 'whatiff-preview-get', token, path, destination: request.destination });
  if (!reply) return new Response('The app did not answer.', { status: 504, headers: { 'Content-Type': 'text/plain' } });
  // A module import without its extension: the file it names, so its own imports resolve from there.
  if (reply.status === 302 && typeof reply.location === 'string') {
    const to = PREFIX + token + '/' + reply.location.split('/').map(encodeURIComponent).join('/');
    return Response.redirect(new URL(to, self.location.origin).href, 302);
  }
  if (reply.status !== 200) {
    return new Response(reply.message || `${path} is not in this project.`, {
      status: reply.status || 404,
      headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' },
    });
  }
  const policy = await sitePolicy();
  if (policy === null) {
    return new Response('The preview could not read its security policy; reload to try again.', {
      status: 503,
      headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' },
    });
  }
  const headers = { 'Content-Type': reply.contentType || 'application/octet-stream', 'Cache-Control': 'no-store', 'Accept-Ranges': 'bytes' };
  if (policy) headers['Content-Security-Policy'] = policy;
  const body = reply.body instanceof ArrayBuffer ? reply.body : new ArrayBuffer(0);
  // One range of the file (a video or audio element seeking; Safari asks for nothing else).
  const range = singleRange(request.headers.get('Range'), body.byteLength);
  if (range === 'unsatisfiable') {
    return new Response(null, { status: 416, headers: { 'Content-Range': `bytes */${body.byteLength}`, 'Cache-Control': 'no-store' } });
  }
  if (range) {
    headers['Content-Range'] = `bytes ${range.start}-${range.end}/${body.byteLength}`;
    return new Response(method === 'HEAD' ? null : body.slice(range.start, range.end + 1), { status: 206, headers });
  }
  return new Response(method === 'HEAD' ? null : body, { status: 200, headers });
}

/** The one range a Range header asks for (inclusive), null for none or several, or 'unsatisfiable'. */
function singleRange(header, size) {
  const m = /^bytes=(\d*)-(\d*)$/.exec((header || '').trim());
  if (!m || (m[1] === '' && m[2] === '')) return null;
  let start;
  let end;
  if (m[1] === '') {
    // The last n bytes.
    start = Math.max(0, size - Number(m[2]));
    end = size - 1;
  } else {
    start = Number(m[1]);
    end = m[2] === '' ? size - 1 : Math.min(Number(m[2]), size - 1);
  }
  if (start >= size || start > end) return 'unsatisfiable';
  return { start, end };
}

/**
 * The site's content security policy, as the pages it serves need it: '' where the site sends none,
 * null when it could not be read (the next request tries again).
 */
async function sitePolicy() {
  if (pagePolicy !== null) return pagePolicy;
  try {
    const response = await fetch(new URL('./index.html', self.registration.scope), { method: 'HEAD', cache: 'no-store' });
    if (!response.ok) return null;
    // Several policies arrive joined by commas; each is kept, and each gets 'self' as a frame ancestor.
    pagePolicy = (response.headers.get('Content-Security-Policy') || '')
      .split(',')
      .map(framedBySelf)
      .filter(Boolean)
      .join(', ');
  } catch {
    return null;
  }
  return pagePolicy;
}

/** The policy with 'self' among its frame ancestors, as the host page frames the user's page. */
function framedBySelf(policy) {
  return policy
    .split(';')
    .map(directive => directive.trim())
    .filter(Boolean)
    .map(directive => (/^frame-ancestors\s/i.test(directive) && !/'self'/.test(directive) ? `frame-ancestors 'self' ${directive.slice('frame-ancestors'.length).trim()}` : directive))
    .join('; ');
}

async function hostFor(token) {
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
  return windows.find(client => new URL(client.url).searchParams.get('t') === token) || null;
}

function ask(client, message) {
  return new Promise(resolve => {
    const channel = new MessageChannel();
    const timer = setTimeout(() => resolve(null), ANSWER_TIMEOUT_MS);
    channel.port1.onmessage = event => {
      clearTimeout(timer);
      resolve(event.data);
    };
    client.postMessage(message, [channel.port2]);
  });
}
