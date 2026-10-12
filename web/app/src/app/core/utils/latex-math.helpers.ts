import type { MarkedExtension, TokenizerAndRendererExtension, Tokens } from 'marked';

/**
 * LaTeX math in rendered markdown.
 *
 * Rendering happens in two steps so the markdown sanitizer can stay strict:
 *
 * 1. `latexMathExtension()` is a marked extension that only *tokenizes* math. It recognises the
 *    delimiters models actually emit — `$…$`, `$$…$$`, `\(…\)` and `\[…\]` — and renders each
 *    formula as a `.latex-math` placeholder whose text is the escaped TeX source. Spans, divs and
 *    `class` survive Angular's HTML sanitizer, so nothing about the sanitizer changes.
 * 2. `renderLatexMath(root)` runs after the sanitized HTML is in the DOM, lazy-loads KaTeX and
 *    renders each placeholder in place. KaTeX's inline styles and MathML never pass through the
 *    sanitizer (which would strip them), and KaTeX itself runs with `trust: false`, so commands
 *    such as `\href` or `\htmlStyle` stay disabled.
 *
 * Code spans and fenced code are untouched: marked tokenizes them before our extension can see
 * their contents.
 */

export const LATEX_MATH_CLASS = 'latex-math';
export const LATEX_MATH_DISPLAY_CLASS = 'latex-math--display';

interface LatexMathToken extends Tokens.Generic {
  type: 'latexMathBlock' | 'latexMathInline';
  raw: string;
  text: string;
  display: boolean;
}

// Block math: the opening delimiter starts a line (up to 3 spaces of indent, like other markdown
// blocks) and the closing one ends a line. No blank lines inside, so a stray `$$` can't swallow
// the rest of the message while it streams in.
const BLOCK_DOLLARS = /^ {0,3}\$\$((?:[^$\n]|\$(?!\$)|\n(?![ \t]*\n))+?)\$\$[ \t]*(?:\n+|$)/;
const BLOCK_BRACKETS = /^ {0,3}\\\[((?:[^\\\n]|\\(?!\])|\n(?![ \t]*\n))+?)\\\][ \t]*(?:\n+|$)/;
const BLOCK_START = /^ {0,3}(?:\$\$|\\\[)/gm;

// Inline math. `$…$` follows Pandoc's rule so prices don't turn into math: the opening `$` must be
// followed by a non-space, the closing `$` preceded by a non-space and not followed by a digit.
// "costs $5 and $10" therefore stays text.
const INLINE_DISPLAY_DOLLARS = /^\$\$((?:[^$]|\$(?!\$))+?)\$\$/;
const INLINE_DISPLAY_BRACKETS = /^\\\[([\s\S]+?)\\\]/;
const INLINE_PARENS = /^\\\(([\s\S]+?)\\\)/;
const INLINE_DOLLAR = /^\$(?=[^\s$])((?:\\.|[^\\\n$])+?)\$(?!\d)/;
const INLINE_START = /\$|\\[([]/;

function blockTokenizer(src: string): LatexMathToken | undefined {
  const match = BLOCK_DOLLARS.exec(src) ?? BLOCK_BRACKETS.exec(src);
  if (!match || !match[1].trim()) {
    return undefined;
  }
  return { type: 'latexMathBlock', raw: match[0], text: match[1].trim(), display: true };
}

function inlineTokenizer(src: string): LatexMathToken | undefined {
  for (const [pattern, display] of [
    [INLINE_DISPLAY_DOLLARS, true],
    [INLINE_DISPLAY_BRACKETS, true],
    [INLINE_PARENS, false],
    [INLINE_DOLLAR, false],
  ] as const) {
    const match = pattern.exec(src);
    if (!match) {
      continue;
    }
    const text = match[1];
    // Pandoc's closing-`$` rule: no whitespace just inside the delimiter.
    if (pattern === INLINE_DOLLAR && /\s$/.test(text)) {
      return undefined;
    }
    if (!text.trim()) {
      return undefined;
    }
    return { type: 'latexMathInline', raw: match[0], text: text.trim(), display };
  }
  return undefined;
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function renderPlaceholder(token: LatexMathToken, tag: 'div' | 'span'): string {
  const classes = token.display ? `${LATEX_MATH_CLASS} ${LATEX_MATH_DISPLAY_CLASS}` : LATEX_MATH_CLASS;
  return `<${tag} class="${classes}">${escapeHtml(token.text)}</${tag}>`;
}

const blockExtension: TokenizerAndRendererExtension = {
  name: 'latexMathBlock',
  level: 'block',
  // Only report a start where a block really matches, so marked doesn't cut a paragraph at a
  // line that merely begins with `$$`.
  start(src: string) {
    for (const match of src.matchAll(BLOCK_START)) {
      if (blockTokenizer(src.slice(match.index))) {
        return match.index;
      }
    }
    return undefined;
  },
  tokenizer: blockTokenizer,
  renderer: token => `${renderPlaceholder(token as LatexMathToken, 'div')}\n`,
};

const inlineExtension: TokenizerAndRendererExtension = {
  name: 'latexMathInline',
  level: 'inline',
  start(src: string) {
    const index = src.search(INLINE_START);
    return index < 0 ? undefined : index;
  },
  tokenizer: inlineTokenizer,
  renderer: token => renderPlaceholder(token as LatexMathToken, 'span'),
};

/** The marked extension; register it through ngx-markdown's `MARKED_EXTENSIONS`. */
export function latexMathExtension(): MarkedExtension {
  return { extensions: [blockExtension, inlineExtension] };
}

type Katex = typeof import('katex').default;
let katexLoader: Promise<Katex> | null = null;

/** KaTeX is ~270 kB, so it is only fetched once a message actually contains math. */
function loadKatex(): Promise<Katex> {
  katexLoader ??= import('katex').then(module => module.default);
  return katexLoader;
}

/**
 * Renders every `.latex-math` placeholder under `root` with KaTeX. Invalid TeX renders as KaTeX's
 * red error text rather than throwing, which matters while a reply is still streaming in.
 */
export async function renderLatexMath(root: HTMLElement): Promise<void> {
  const placeholders = Array.from(root.querySelectorAll<HTMLElement>(`.${LATEX_MATH_CLASS}`));
  if (!placeholders.length) {
    return;
  }
  const katex = await loadKatex();
  for (const element of placeholders) {
    // Streaming re-renders replace the markdown; skip placeholders that were already swapped out.
    if (!element.isConnected) {
      continue;
    }
    katex.render(element.textContent ?? '', element, {
      displayMode: element.classList.contains(LATEX_MATH_DISPLAY_CLASS),
      throwOnError: false,
      trust: false,
      output: 'htmlAndMathml',
    });
  }
}

/**
 * Replaces each rendered formula in a copied selection with its TeX source (`$…$` / `$$…$$`), read
 * from the annotation KaTeX embeds in its MathML. Without this the clipboard gets KaTeX's
 * glyph-by-glyph layout spans, which read as noise once pasted.
 */
export function replaceRenderedMathWithTex(container: HTMLElement): void {
  for (const formula of Array.from(container.querySelectorAll<HTMLElement>('.katex'))) {
    const tex = formula.querySelector('annotation[encoding="application/x-tex"]')?.textContent;
    if (tex == null) {
      continue;
    }
    const displayRoot = formula.closest('.katex-display');
    (displayRoot ?? formula).replaceWith(container.ownerDocument.createTextNode(displayRoot ? `$$${tex}$$` : `$${tex}$`));
  }
}
