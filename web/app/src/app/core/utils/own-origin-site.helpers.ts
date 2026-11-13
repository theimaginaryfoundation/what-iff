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
