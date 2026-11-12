/**
 * What If's settings for the draw.io editor it hosts (ADR 0x049), written over the release's own
 * js/PreConfig.js by scripts/build-diagrams-site.sh. It runs before the editor loads.
 *
 * The editor is embedded in the file workspace and must work with no outside services: no export
 * server, no plugin store, no cloud storage, no update checks.
 */
window.DRAWIO_PUBLIC_BUILD = true;
// With no export server, PDF export uses the browser's print dialog and images are made in the page.
window.EXPORT_URL = null;
window.DRAWIO_BASE_URL = null;
window.DRAWIO_VIEWER_URL = null;
window.DRAWIO_LIGHTBOX_URL = null;
window.DRAW_MATH_URL = 'math4/es5';
window.DRAWIO_CONFIG = {
  // Files are saved as plain XML, so agents and diffs can read them (ADR 0x049).
  compressXml: false,
};
urlParams['sync'] = 'manual';
// No features that call outside services, whatever the embedding page asks for.
urlParams['stealth'] = '1';
urlParams['offline'] = '1';
urlParams['pwa'] = '0';
