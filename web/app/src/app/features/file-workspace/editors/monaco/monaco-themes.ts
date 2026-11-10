import type { Monaco } from './monaco-loader';

/**
 * Monaco themes on the app's own palette (src/styles/themes.scss), so code reads well on the
 * card background in both modes. Monaco takes hex colours only, so the token values are copied.
 */
export const MONACO_THEME_DARK = 'whatiff-dark';
export const MONACO_THEME_LIGHT = 'whatiff-light';

export function defineMonacoThemes(monaco: Pick<Monaco, 'editor'>): void {
  monaco.editor.defineTheme(MONACO_THEME_DARK, {
    base: 'vs-dark',
    inherit: true,
    rules: [
      { token: '', foreground: 'E5E7EB' },
      { token: 'comment', foreground: '8B95A7', fontStyle: 'italic' },
      { token: 'keyword', foreground: 'C4B5FD' },
      { token: 'keyword.control', foreground: 'C4B5FD' },
      { token: 'storage', foreground: 'C4B5FD' },
      { token: 'string', foreground: 'FCD38D' },
      { token: 'string.escape', foreground: 'FDBA74' },
      { token: 'number', foreground: 'F9A8D4' },
      { token: 'regexp', foreground: 'FDBA74' },
      { token: 'type', foreground: '7DD3FC' },
      { token: 'type.identifier', foreground: '7DD3FC' },
      { token: 'identifier', foreground: 'E5E7EB' },
      { token: 'variable', foreground: 'E5E7EB' },
      { token: 'function', foreground: '93C5FD' },
      { token: 'delimiter', foreground: 'A5B4CC' },
      { token: 'delimiter.bracket', foreground: 'A5B4CC' },
      { token: 'tag', foreground: '7DD3FC' },
      { token: 'attribute.name', foreground: 'C4B5FD' },
      { token: 'attribute.value', foreground: 'FCD38D' },
      { token: 'operator', foreground: 'A5B4CC' },
      { token: 'constant', foreground: 'F9A8D4' },
      { token: 'annotation', foreground: '8B95A7' },
      { token: 'key', foreground: '7DD3FC' },
      { token: 'string.key.json', foreground: '7DD3FC' },
      { token: 'string.value.json', foreground: 'FCD38D' },
      { token: 'emphasis', fontStyle: 'italic' },
      { token: 'strong', fontStyle: 'bold' },
      { token: 'keyword.md', foreground: 'C4B5FD', fontStyle: 'bold' },
      { token: 'string.link.md', foreground: '93C5FD' },
    ],
    colors: {
      'editor.background': '#1F2937',
      'editor.foreground': '#E5E7EB',
      'editorLineNumber.foreground': '#6B7280',
      'editorLineNumber.activeForeground': '#D1D5DB',
      'editor.lineHighlightBackground': '#273244',
      'editor.selectionBackground': '#4C4A9C',
      'editor.inactiveSelectionBackground': '#374151',
      'editorCursor.foreground': '#C4B5FD',
      'editorIndentGuide.background': '#374151',
      'editorIndentGuide.activeBackground': '#4B5563',
      'editorWidget.background': '#111827',
      'editorWidget.border': '#374151',
      'editorSuggestWidget.background': '#111827',
      'editorSuggestWidget.selectedBackground': '#374151',
      'editorHoverWidget.background': '#111827',
      'scrollbarSlider.background': '#4B556380',
      'scrollbarSlider.hoverBackground': '#6B7280A0',
      'minimap.background': '#1F2937',
    },
  });
  monaco.editor.defineTheme(MONACO_THEME_LIGHT, {
    base: 'vs',
    inherit: true,
    rules: [
      { token: '', foreground: '111827' },
      { token: 'comment', foreground: '6B7280', fontStyle: 'italic' },
      { token: 'keyword', foreground: '6D28D9' },
      { token: 'storage', foreground: '6D28D9' },
      { token: 'string', foreground: 'B45309' },
      { token: 'number', foreground: 'BE185D' },
      { token: 'type', foreground: '0369A1' },
      { token: 'type.identifier', foreground: '0369A1' },
      { token: 'function', foreground: '1D4ED8' },
      { token: 'tag', foreground: '0369A1' },
      { token: 'attribute.name', foreground: '6D28D9' },
      { token: 'attribute.value', foreground: 'B45309' },
      { token: 'key', foreground: '0369A1' },
      { token: 'string.key.json', foreground: '0369A1' },
      { token: 'string.value.json', foreground: 'B45309' },
    ],
    colors: {
      'editor.background': '#FFFFFF',
      'editor.foreground': '#111827',
      'editorLineNumber.foreground': '#9CA3AF',
      'editor.lineHighlightBackground': '#F3F4F6',
      'editor.selectionBackground': '#DDD9FF',
      'editorCursor.foreground': '#6D28D9',
    },
  });
}

export function monacoThemeFor(theme: 'light' | 'dark'): string {
  return theme === 'dark' ? MONACO_THEME_DARK : MONACO_THEME_LIGHT;
}

/** The app's resolved theme, as theme.helpers.ts writes it on <html data-theme>. */
export function appTheme(document: Document): 'light' | 'dark' {
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
}

/** Calls back with the Monaco theme name now and whenever the app's theme changes; returns a stop function. */
export function followAppTheme(document: Document, apply: (theme: string) => void): () => void {
  apply(monacoThemeFor(appTheme(document)));
  if (typeof MutationObserver === 'undefined') return () => undefined;
  const observer = new MutationObserver(() => apply(monacoThemeFor(appTheme(document))));
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
  return () => observer.disconnect();
}
