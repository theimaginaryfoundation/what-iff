const ALLOWED_SCHEMES = new Set(['http:', 'https:', 'mailto:']);

/**
 * The link from an uploaded document if it is safe to make clickable, otherwise null. A document
 * can carry any target, including `javascript:`, so only web and mail links and anchors within the
 * document get through. Browsers ignore control characters and leading spaces in a scheme, so the
 * check does too.
 */
export function safeLinkUrl(url: string | null | undefined): string | null {
  if (!url) return null;
  // eslint-disable-next-line no-control-regex
  const normalized = url.replace(/[\u0000- \u007f]/g, '');
  if (normalized.startsWith('#')) return url;
  const scheme = /^[a-z][a-z0-9+.-]*:/i.exec(normalized)?.[0].toLowerCase();
  return scheme && ALLOWED_SCHEMES.has(scheme) ? url : null;
}

/**
 * A link's properties in a Univer model. A link this app will not follow keeps its target for the
 * saved file under `keptUrl`, with an empty `url`, so it is shown but cannot be opened here.
 */
export function linkProperties(url: string): { url: string; keptUrl?: string } {
  const safe = safeLinkUrl(url);
  return safe ? { url: safe } : { url: '', keptUrl: url };
}

/** The target to write back to the file for a link's properties. */
export function savedLinkUrl(properties: Record<string, unknown> | undefined): string {
  const kept = properties?.['keptUrl'];
  return typeof kept === 'string' && kept ? kept : String(properties?.['url'] ?? '');
}
