/*
 * The preview site's service worker (ADR 0x050 §3).
 *
 * It serves every request under ./p/<token>/ by asking the host page that owns the token, which asks
 * the app; nothing is kept here. A browser stops an idle worker and drops its memory, so a worker
 * that held the files would lose them; asking on each request keeps it stateless, and a pending
 * request keeps it alive until it is answered.
 *
 * The host page is the window client whose URL carries `t=<token>` (index.html?t=<token>).
 */
'use strict';

const PREFIX = new URL('./p/', self.registration.scope).pathname;
/** How long a request may wait for the app before it is answered 504. */
const ANSWER_TIMEOUT_MS = 20000;

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
  event.respondWith(answer(token, path, event.request.method));
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

async function answer(token, path, method) {
  if (method !== 'GET' && method !== 'HEAD') {
    return new Response('The preview serves files only.', { status: 405 });
  }
  const host = await hostFor(token);
  if (!host) return new Response('This preview has closed.', { status: 410, headers: { 'Content-Type': 'text/plain' } });
  const reply = await ask(host, { type: 'whatiff-preview-get', token, path });
  if (!reply) return new Response('The app did not answer.', { status: 504, headers: { 'Content-Type': 'text/plain' } });
  if (reply.status !== 200) {
    return new Response(reply.message || `${path} is not in this project.`, {
      status: reply.status || 404,
      headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'no-store' },
    });
  }
  return new Response(method === 'HEAD' ? null : reply.body, {
    status: 200,
    headers: { 'Content-Type': reply.contentType || 'application/octet-stream', 'Cache-Control': 'no-store' },
  });
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
