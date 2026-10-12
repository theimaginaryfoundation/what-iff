import { Marked } from 'marked';

import { latexMathExtension, renderLatexMath, replaceRenderedMathWithTex } from './latex-math.helpers';

function parse(markdown: string): string {
  return new Marked(latexMathExtension()).parse(markdown, { async: false });
}

/** The TeX of every placeholder in `html`, prefixed with `display:` or `inline:`. */
function formulas(html: string): string[] {
  const root = document.createElement('div');
  root.innerHTML = html;
  return Array.from(root.querySelectorAll('.latex-math')).map(
    el => `${el.classList.contains('latex-math--display') ? 'display' : 'inline'}:${el.textContent}`,
  );
}

const RMSNORM = String.raw`\text{RMSNorm}(x)=\frac{x}{\sqrt{\frac{1}{d}\sum_i x_i^2+\epsilon}}\odot g`;

describe('latexMathExtension', () => {
  it('renders bracket display math on its own line (the reported RMSNorm example)', () => {
    const html = parse(String.raw`\[ ${RMSNORM} \]`);
    expect(formulas(html)).toEqual([`display:${RMSNORM}`]);
    // marked would otherwise read `_i x_` as emphasis.
    expect(html).not.toContain('<em>');
    expect(html).toMatch(/^<div class="latex-math latex-math--display">/);
  });

  it('renders multi-line display blocks for both delimiter styles', () => {
    const md = ['Where:', '$$', 'a = b_1', '+ c_2', '$$', '', '\\[', 'x^2', '\\]', 'done'].join('\n');
    expect(formulas(parse(md))).toEqual(['display:a = b_1\n+ c_2', 'display:x^2']);
  });

  it('keeps matrix row breaks inside a display block', () => {
    const md = '$$\n\\begin{pmatrix} a & b \\\\\n c & d \\end{pmatrix}\n$$';
    expect(formulas(parse(md))).toEqual(['display:\\begin{pmatrix} a & b \\\\\n c & d \\end{pmatrix}']);
  });

  it('renders inline math with dollars and parentheses', () => {
    const html = parse(String.raw`Scale by $\sqrt{d_k}$ and \(W_q x\), or inline display $$\sum_i x_i$$.`);
    expect(formulas(html)).toEqual([String.raw`inline:\sqrt{d_k}`, 'inline:W_q x', String.raw`display:\sum_i x_i`]);
    expect(html).toContain('<p>Scale by <span');
  });

  it.each([
    ['prices', 'It costs $5 and $10 per month.'],
    ['a space after the opening dollar', 'Between $ x $ and y.'],
    ['a space before the closing dollar', 'Between $x $ and y.'],
    ['whitespace-only math', String.raw`Empty \(   \) parens.`],
    ['a digit after the closing dollar', 'Pay $x$5 now.'],
    ['an escaped dollar', String.raw`A \$5 fee and \$10 tip.`],
    ['unclosed delimiters', String.raw`Open \( and $$ never closed`],
  ])('leaves %s as text', (_label, md) => {
    expect(formulas(parse(md))).toEqual([]);
  });

  it('leaves code spans and fenced code alone', () => {
    const md = ['Use `$x$` literally.', '', '```', '$$', 'y', '$$', String.raw`\[ z \]`, '```'].join('\n');
    const html = parse(md);
    expect(formulas(html)).toEqual([]);
    expect(html).toContain('<code>$x$</code>');
    expect(html).toContain(String.raw`\[ z \]`);
  });

  it('keeps a line that only starts with $$ inside its paragraph', () => {
    const html = parse('Energy:\n$$E=mc^2$$ in words.');
    expect(formulas(html)).toEqual(['display:E=mc^2']);
    expect(html).not.toContain('<div');
    expect(html.match(/<p>/g)).toHaveLength(1);
  });

  it('does not let a stray $$ swallow later paragraphs', () => {
    const md = ['$$ broken', '', 'Next paragraph with $$ later.'].join('\n');
    expect(formulas(parse(md))).toEqual([]);
  });

  it('escapes the TeX source in the placeholder', () => {
    const html = parse('$a<b>c$');
    expect(html).toContain('a&lt;b&gt;c');
    expect(html).not.toContain('<b>');
  });

  it('works inside list items', () => {
    expect(formulas(parse('- loss $\\mathcal{L}$\n- norm:\n\n  $$\n  \\|x\\|_2\n  $$'))).toEqual([
      'inline:\\mathcal{L}',
      'display:\\|x\\|_2',
    ]);
  });
});

describe('renderLatexMath', () => {
  it('renders placeholders with KaTeX, display and inline', async () => {
    const root = document.createElement('div');
    root.innerHTML = parse(String.raw`Inline $x_i^2$.` + '\n\n' + String.raw`\[ ${RMSNORM} \]`);
    document.body.append(root);

    await renderLatexMath(root);

    expect(root.querySelectorAll('.katex')).toHaveLength(2);
    expect(root.querySelectorAll('.katex-display')).toHaveLength(1);
    expect(root.querySelector('annotation')?.textContent).toBe('x_i^2');
    root.remove();
  });

  it('does nothing when there is no math', async () => {
    const root = document.createElement('div');
    root.innerHTML = parse('Just *words*.');

    await renderLatexMath(root);

    expect(root.innerHTML).toBe('<p>Just <em>words</em>.</p>\n');
  });

  it('skips placeholders replaced before KaTeX is ready (a streaming re-render)', async () => {
    const root = document.createElement('div');
    root.innerHTML = parse('$x$');
    document.body.append(root);
    const stale = root.querySelector('.latex-math')!;

    const rendering = renderLatexMath(root);
    root.innerHTML = '<p>newer chunk</p>';
    await rendering;

    expect(stale.querySelector('.katex')).toBeNull();
    expect(stale.textContent).toBe('x');
    root.remove();
  });

  it('renders invalid TeX as an error instead of throwing', async () => {
    const root = document.createElement('div');
    root.innerHTML = parse(String.raw`Bad $\frac{1}{$ here and $\notacommand$.`);
    document.body.append(root);

    await expect(renderLatexMath(root)).resolves.toBeUndefined();
    root.remove();
  });

  it('does not enable trusted commands', async () => {
    const root = document.createElement('div');
    root.innerHTML = parse(String.raw`$\href{javascript:alert(1)}{x}$`);
    document.body.append(root);

    await renderLatexMath(root);

    // KaTeX shows the refused command as error text; the source survives only as inert annotation text.
    expect(root.querySelector('a, [href]')).toBeNull();
    root.remove();
  });
});

describe('replaceRenderedMathWithTex', () => {
  it('turns rendered formulas back into delimited TeX', async () => {
    const root = document.createElement('div');
    root.innerHTML = parse('Inline $x^2$ and:\n\n$$\ny_1\n$$');
    document.body.append(root);
    await renderLatexMath(root);

    replaceRenderedMathWithTex(root);

    expect(root.querySelector('.katex')).toBeNull();
    expect(root.textContent).toContain('Inline $x^2$ and:');
    expect(root.textContent).toContain('$$y_1$$');
    root.remove();
  });

  it('leaves a formula fragment without its TeX annotation as it is', () => {
    const root = document.createElement('div');
    root.innerHTML = '<span class="katex"><span class="katex-html">x</span></span>';

    replaceRenderedMathWithTex(root);

    expect(root.querySelector('.katex')?.textContent).toBe('x');
  });
});
