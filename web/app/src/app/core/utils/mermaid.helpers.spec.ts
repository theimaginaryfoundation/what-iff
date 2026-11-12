import { Marked } from 'marked';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  parse: vi.fn(async (source: string) => {
    if (source.includes('broken')) throw new Error('Parse error on line 1');
    return {};
  }),
  render: vi.fn(async (_id: string, source: string) => ({
    svg:
      `<svg viewBox="0 0 100 50"><style>.node{fill:red;background:url(https://evil.example/x)}@import url(https://evil.example/a.css);</style>` +
      `<script>alert(1)</script><foreignObject><div>html label</div></foreignObject>` +
      `<g class="node" onclick="alert(1)"><text>${source.split('\n')[1]?.trim() ?? ''}</text></g>` +
      `<path marker-end="url(#arrow)" style="fill:url(https://evil.example/f)"/></svg>`,
  })),
}));
vi.mock('mermaid', () => ({ default: mermaid }));

import {
  MERMAID_CLASS,
  drawMermaid,
  isClosedFence,
  mermaidExtension,
  renderMermaid,
  replaceRenderedDiagramsWithSource,
} from './mermaid.helpers';

function render(markdown: string): string {
  return new Marked(mermaidExtension()).parse(markdown) as string;
}

function mount(html: string): HTMLElement {
  const root = document.createElement('div');
  root.innerHTML = html;
  document.body.appendChild(root);
  return root;
}

describe('mermaid in markdown', () => {
  beforeEach(() => {
    mermaid.render.mockClear();
  });

  it('turns a closed ```mermaid fence into a placeholder holding the escaped source', () => {
    const html = render('Before\n\n```mermaid\nflowchart LR\n  A --> B<script>\n```\n\nAfter');
    expect(html).toContain(
      `<div class="${MERMAID_CLASS}"><pre class="mermaid-diagram__source">flowchart LR\n  A --&gt; B&lt;script&gt;</pre></div>`,
    );
    expect(html).toContain('<p>After</p>');
  });

  it('leaves other code, and a fence still streaming in, as code', () => {
    expect(render('```js\nconst a = 1;\n```')).toContain('<code class="language-js">');
    const streaming = render('```mermaid\nflowchart LR\n  A --> B');
    expect(streaming).not.toContain(MERMAID_CLASS);
    expect(streaming).toContain('<code class="language-mermaid">');
    expect(render('```mermaid\n\n```')).not.toContain(MERMAID_CLASS);
  });

  it('tells a closed fence from an open one', () => {
    expect(isClosedFence('```mermaid\na\n```\n')).toBe(true);
    expect(isClosedFence('~~~~mermaid\na\n~~~~')).toBe(true);
    expect(isClosedFence('````mermaid\na\n```')).toBe(false);
    expect(isClosedFence('```mermaid\na\nb')).toBe(false);
  });

  it('draws the diagram with Mermaid in strict mode, through the SVG sanitizer, with its toolbar', async () => {
    const root = mount(render('```mermaid\nflowchart LR\n  Hello\n```'));
    await renderMermaid(root);
    const diagram = root.querySelector<HTMLElement>(`.${MERMAID_CLASS}`)!;
    expect(diagram.dataset['mermaidState']).toBe('drawn');
    expect(mermaid.initialize).toHaveBeenCalledWith(
      expect.objectContaining({ securityLevel: 'strict', startOnLoad: false, htmlLabels: false, theme: 'base' }),
    );
    const svg = diagram.querySelector('.mermaid-diagram__figure svg')!;
    expect(svg.querySelector('text')?.textContent).toBe('Hello');
    expect(svg.querySelector('script, foreignObject')).toBeNull();
    expect(svg.querySelector('[onclick]')).toBeNull();
    const css = svg.querySelector('style')!.textContent!;
    expect(css).toContain('fill:red');
    expect(css).not.toContain('evil.example');
    expect(svg.querySelector('path')!.getAttribute('marker-end')).toBe('url(#arrow)');
    expect(svg.querySelector('path')!.getAttribute('style')).not.toContain('evil.example');
    expect(diagram.querySelector<HTMLElement>('.mermaid-diagram__source')!.hidden).toBe(true);
    expect([...diagram.querySelectorAll('.mermaid-diagram__button')].map(b => b.textContent)).toEqual(['Source', 'Copy', 'SVG', 'PNG']);
  });

  it('switches between the diagram and its source', async () => {
    const root = mount(render('```mermaid\nflowchart LR\n  Hello\n```'));
    await renderMermaid(root);
    const toggle = root.querySelector<HTMLButtonElement>('.mermaid-diagram__button')!;
    toggle.click();
    expect(root.querySelector<HTMLElement>('.mermaid-diagram__figure')!.hidden).toBe(true);
    expect(root.querySelector<HTMLElement>('.mermaid-diagram__source')!.hidden).toBe(false);
    expect(toggle.textContent).toBe('Diagram');
    toggle.click();
    expect(root.querySelector<HTMLElement>('.mermaid-diagram__figure')!.hidden).toBe(false);
  });

  it('keeps source Mermaid cannot draw as code, with a note', async () => {
    const root = mount(render('```mermaid\nbroken diagram\n```'));
    await renderMermaid(root);
    const diagram = root.querySelector<HTMLElement>(`.${MERMAID_CLASS}`)!;
    expect(diagram.dataset['mermaidState']).toBe('error');
    expect(diagram.querySelector('.mermaid-diagram__error')?.textContent).toBe("Couldn't draw this diagram.");
    expect(diagram.querySelector<HTMLElement>('.mermaid-diagram__source')!.hidden).toBe(false);
  });

  it('draws a diagram once however often its message is rendered again', async () => {
    const source = 'flowchart LR\n  Cached once';
    await drawMermaid(source, 'light');
    await drawMermaid(source, 'light');
    const root = mount(render('```mermaid\n' + source + '\n```'));
    await renderMermaid(root);
    expect(mermaid.render).toHaveBeenCalledTimes(1);
    await drawMermaid(source, 'dark');
    expect(mermaid.render).toHaveBeenCalledTimes(2);
  });

  it('copies a drawn diagram as its source', async () => {
    const root = mount('<p>See:</p>' + render('```mermaid\nflowchart LR\n  Hello\n```'));
    await renderMermaid(root);
    const copy = root.cloneNode(true) as HTMLElement;
    replaceRenderedDiagramsWithSource(copy);
    expect(copy.textContent!.trim()).toBe('See:```mermaid\nflowchart LR\n  Hello\n```');
  });
});
