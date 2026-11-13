import { Marked } from 'marked';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  parse: vi.fn(),
  render: vi.fn(async (id: string, source: string) => {
    if (source.includes('broken')) throw new Error('Parse error on line 1');
    if (source.includes('offline')) throw new TypeError('Failed to fetch dynamically imported module: flowDiagram.js');
    return {
      svg:
        `<svg id="${id}" viewBox="0 0 100 50"><style>#${id} .node{fill:red;background:url(https://evil.example/x)}@import url(https://evil.example/a.css);</style>` +
        `<script>alert(1)</script><foreignObject><div>html label</div></foreignObject>` +
        `<defs><marker id="${id}_arrow"><path d="M0 0"/></marker></defs>` +
        `<g class="node" onclick="alert(1)"><text>${source.split('\n')[1]?.trim() ?? ''}</text></g>` +
        `<path marker-end="url(#${id}_arrow)" style="fill:url(https://evil.example/f)"/></svg>`,
    };
  }),
}));
vi.mock('mermaid', () => ({ default: mermaid }));

import { buildClipboardPayload } from '../../features/chat/helpers/html-clipboard.helpers';
import {
  MERMAID_CLASS,
  drawMermaid,
  isClosedFence,
  mermaidExtension,
  renderMermaid,
  replaceRenderedDiagramsWithSource,
  withoutFetchingCss,
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
      expect.objectContaining({
        securityLevel: 'strict',
        startOnLoad: false,
        htmlLabels: false,
        theme: 'base',
        suppressErrorRendering: true,
        secure: expect.arrayContaining(['securityLevel', 'suppressErrorRendering', 'themeCSS']),
      }),
    );
    expect(mermaid.parse).not.toHaveBeenCalled();
    const svg = diagram.querySelector('.mermaid-diagram__figure svg')!;
    expect(svg.querySelector('text')?.textContent).toBe('Hello');
    expect(svg.querySelector('script, foreignObject')).toBeNull();
    expect(svg.querySelector('[onclick]')).toBeNull();
    const css = svg.querySelector('style')!.textContent!;
    expect(css).toContain('fill:red');
    expect(css).not.toContain('evil.example');
    const path = svg.querySelector('path[marker-end]')!;
    expect(path.getAttribute('marker-end')).toBe(`url(#${svg.querySelector('marker')!.id})`);
    expect(path.getAttribute('style')).not.toContain('evil.example');
    expect(diagram.querySelector<HTMLElement>('.mermaid-diagram__source')!.hidden).toBe(true);
    expect([...diagram.querySelectorAll('.mermaid-diagram__button')].map(b => b.textContent)).toEqual([
      'Source',
      'Expand',
      'Copy',
      'SVG',
      'PNG',
    ]);
  });

  it('sizes the diagram from its drawing, not the 300px a shrinking box gives width="100%"', async () => {
    const root = mount(render('```mermaid\nflowchart LR\n  Hello\n```'));
    await renderMermaid(root);
    const figure = root.querySelector<HTMLElement>('.mermaid-diagram__figure')!;
    expect(figure.style.getPropertyValue('--mermaid-width')).toBe('100px');
    expect(figure.querySelector('svg')!.hasAttribute('width')).toBe(false);
  });

  it('expands the diagram into a dialog, and closes it', async () => {
    const root = mount(render('```mermaid\nflowchart LR\n  Hello\n```'));
    await renderMermaid(root);
    [...root.querySelectorAll<HTMLButtonElement>('.mermaid-diagram__button')].find(b => b.textContent === 'Expand')!.click();
    const dialog = document.querySelector('dialog.mermaid-diagram__dialog')!;
    expect(dialog.querySelector('svg text')?.textContent).toBe('Hello');
    dialog.querySelector<HTMLButtonElement>('.mermaid-diagram__close')!.click();
    expect(document.querySelector('dialog.mermaid-diagram__dialog')).toBeNull();
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
    expect(copy.querySelector('pre')!.textContent).toBe('```mermaid\nflowchart LR\n  Hello\n```');
  });

  it('copies a selection holding a diagram with the source on its own lines, in a fence longer than its backticks', async () => {
    const source = 'flowchart LR\n  A["```"] --> B';
    const root = mount('<p>See:</p>' + render('````mermaid\n' + source + '\n````'));
    await renderMermaid(root);
    expect(root.querySelector<HTMLElement>(`.${MERMAID_CLASS}`)!.dataset['mermaidState']).toBe('drawn');
    const selection = document.getSelection()!;
    selection.removeAllRanges();
    const range = document.createRange();
    range.selectNodeContents(root);
    selection.addRange(range);
    const { plainText, html } = buildClipboardPayload(document, selection);
    expect(plainText).toBe('See:\n````mermaid\n' + source + '\n````');
    expect(html).toContain('<pre>````mermaid\nflowchart LR\n  A["```"] --&gt; B\n````</pre>');
    selection.removeAllRanges();
  });

  it('gives each drawn copy of a diagram, and its full-screen view, ids of its own', async () => {
    const markdown = render('```mermaid\nflowchart LR\n  Twice\n```');
    const first = mount(markdown);
    const second = mount(markdown);
    await renderMermaid(first);
    await renderMermaid(second);
    const ids = (root: ParentNode) => {
      const svg = root.querySelector('svg')!;
      const marker = svg.querySelector('marker')!.id;
      expect(svg.querySelector('path[marker-end]')!.getAttribute('marker-end')).toBe(`url(#${marker})`);
      expect(svg.querySelector('style')!.textContent).toContain(`#${svg.id} .node`);
      return [svg.id, marker];
    };
    const [a, b] = [ids(first), ids(second)];
    expect(a[0]).not.toBe(b[0]);
    expect(a[1]).not.toBe(b[1]);
    [...first.querySelectorAll<HTMLButtonElement>('.mermaid-diagram__button')].find(x => x.textContent === 'Expand')!.click();
    const dialog = document.querySelector<HTMLElement>('dialog.mermaid-diagram__dialog')!;
    const c = ids(dialog);
    expect(c[0]).not.toBe(a[0]);
    expect(c[1]).not.toBe(a[1]);
    dialog.querySelector<HTMLButtonElement>('.mermaid-diagram__close')!.click();
  });

  it('draws a diagram again after Mermaid failed to load, but not one whose source is wrong', async () => {
    await drawMermaid('flowchart LR\n  offline', 'light');
    await drawMermaid('flowchart LR\n  offline', 'light');
    expect(mermaid.render).toHaveBeenCalledTimes(2);
    mermaid.render.mockClear();
    await drawMermaid('broken once', 'light');
    expect(await drawMermaid('broken once', 'light')).toEqual({ error: 'Parse error on line 1' });
    expect(mermaid.render).toHaveBeenCalledTimes(1);
  });

  it("takes its colours from the page's theme tokens", async () => {
    await drawMermaid('flowchart LR\n  Tokens light', 'light');
    document.documentElement.style.setProperty('--accent', '#ABCDEF');
    document.documentElement.style.setProperty('--bg-card', '#010203');
    await drawMermaid('flowchart LR\n  Tokens dark', 'dark');
    document.documentElement.style.removeProperty('--accent');
    document.documentElement.style.removeProperty('--bg-card');
    const { themeVariables } = mermaid.initialize.mock.lastCall![0];
    expect(themeVariables).toMatchObject({ darkMode: true, primaryBorderColor: '#ABCDEF', background: '#010203', textColor: '#F9FAFB' });
  });

  it('skips a queued drawing whose placeholder was replaced before its turn', async () => {
    const replaced = mount(render('```mermaid\nflowchart LR\n  Typing\n```'));
    const kept = mount(render('```mermaid\nflowchart LR\n  Typing more\n```'));
    const rendering = [renderMermaid(replaced), renderMermaid(kept)];
    replaced.remove();
    await Promise.all(rendering);
    expect(mermaid.render).toHaveBeenCalledTimes(1);
    expect(mermaid.render).toHaveBeenCalledWith(expect.any(String), 'flowchart LR\n  Typing more');
    // Nothing was cached for the skipped one: shown again, it is drawn.
    const again = mount(render('```mermaid\nflowchart LR\n  Typing\n```'));
    await renderMermaid(again);
    expect(again.querySelector<HTMLElement>(`.${MERMAID_CLASS}`)!.dataset['mermaidState']).toBe('drawn');
  });
});

describe('withoutFetchingCss', () => {
  it('keeps references to the SVG itself, quoted or not', () => {
    expect(withoutFetchingCss('marker-end:url(#arrow);fill:url( "#grad-1" );stroke:url(\'#b\')')).toBe(
      'marker-end:url(#arrow);fill:url(#grad-1);stroke:url(#b)',
    );
  });

  it('drops url() to anything else, and @import', () => {
    expect(withoutFetchingCss('@import url(https://e.example/a.css);a{background:url("https://e.example/x")}')).toBe('a{background:none}');
    expect(withoutFetchingCss('@import "https://e.example/a.css";a{}')).toBe('a{}');
  });

  it('drops image-set(), image() and src(), which take a URL as a string', () => {
    const css = withoutFetchingCss(
      'a{background:image-set("https://e.example/x.png" 1x)}b{background:-webkit-image-set("https://e.example/y" 1x)}c{background:image("https://e.example/z")}',
    );
    expect(css).not.toMatch(/image-set\(|image\(/);
    expect(css).toContain('none(');
  });

  it('reads CSS escapes before it checks', () => {
    expect(withoutFetchingCss('a{background:\\75 rl(https://e.example/x)}')).toBe('a{background:none}');
    expect(withoutFetchingCss('a{background:u\\72 l("https://e.example/x")}')).toBe('a{background:none}');
    expect(withoutFetchingCss('@\\69mport url(https://e.example/a.css);a{}')).toBe('a{}');
    expect(withoutFetchingCss('@\\69 mport "https://e.example/a.css";a{}')).toBe('a{}');
  });

  it('leaves no backslash that could form a new escape', () => {
    expect(withoutFetchingCss('a{background:\\\\75 rl(https://e.example/x)}')).not.toContain('\\');
  });
});
