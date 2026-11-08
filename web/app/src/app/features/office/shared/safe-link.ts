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
