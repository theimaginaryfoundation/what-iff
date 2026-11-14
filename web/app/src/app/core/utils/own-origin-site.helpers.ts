/**
 * A site the app embeds in an iframe and that must be served from an origin of its own: the
 * JupyterLite that runs notebooks (ADR 0x043 §11) and the draw.io editor for diagrams (ADR 0x049).
 * A same-origin iframe shares `sessionStorage` with this page, so code the frame runs could read the
 * session token; a URL on this origin is refused outright, which is what makes `allow-same-origin`
 * on such an iframe safe. It must also be https outside local development, since the frame runs
 * what it is served. Returns the URL, or null when the site is not usable (the feature stays off).
 */
export function usableOwnOriginSite(url: string | null | undefined, appOrigin: string, what: string): string | null {
  if (!url) return null;
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null; // Includes a deployment's unsubstituted placeholder.
  }
  const local = parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1';
  if (parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && local)) return null;
  if (parsed.origin === appOrigin) {
    console.error(`${what} must be served from its own origin; it stays off.`);
    return null;
  }
  return parsed.href;
}

/**
 * A site that runs code users and agents wrote with that code's own origin (the preview site, ADR
 * 0x050 §3) must be on another *site* than the app, a registrable domain of its own, not just
 * another origin: a page on any host under the app's domain can read the app's cookies on that
 * domain (a shared sign-in keeps the session in one) and set cookies the app reads. So, beyond
 * usableOwnOriginSite, a URL on the app's site is refused. The site is approximated by the host's
 * last two labels, which refuses a little more than the public suffix list would (two hosts under
 * one country's second-level domain), never less; an address or `localhost` is its own site.
 */
export function usableOtherSite(url: string | null | undefined, appOrigin: string, what: string): string | null {
  const usable = usableOwnOriginSite(url, appOrigin, what);
  if (!usable) return null;
  if (siteOf(new URL(usable).hostname) === siteOf(new URL(appOrigin).hostname)) {
    console.error(`${what} must be served from a domain of its own, not one under the app's; it stays off.`);
    return null;
  }
  return usable;
}

/** A host's site, as usableOtherSite approximates it. */
export function siteOf(hostname: string): string {
  const host = hostname.toLowerCase().replace(/\.$/, '');
  if (/^[\d.]+$/.test(host) || host.includes(':') || !host.includes('.')) return host;
  return host.split('.').slice(-2).join('.');
}
