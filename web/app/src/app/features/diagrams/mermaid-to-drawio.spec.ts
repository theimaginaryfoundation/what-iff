import { afterEach, describe, expect, it, vi } from 'vitest';

import { convertMermaidToDrawio, mermaidFileName } from './mermaid-to-drawio';

const SITE = 'https://diagrams.example.test/index.html';
const ORIGIN = 'https://diagrams.example.test';
const DIAGRAM =
  '<mxfile><diagram id="p"><mxGraphModel><root><mxCell id="0"/><mxCell id="2" vertex="1" parent="1"/></root></mxGraphModel></diagram></mxfile>';

describe('convertMermaidToDrawio', () => {
  afterEach(() => vi.useRealTimers());

  function editorFrame() {
    const frame = document.querySelector('iframe') as HTMLIFrameElement;
    const sent: Record<string, unknown>[] = [];
    vi.spyOn(frame.contentWindow!, 'postMessage').mockImplementation(((m: string) =>
      sent.push(JSON.parse(m))) as typeof window.postMessage);
    const answer = (message: Record<string, unknown>) =>
      window.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(message), origin: ORIGIN, source: frame.contentWindow }));
    return { frame, sent, answer };
  }

  it('loads the Mermaid into a hidden editor and takes the diagram it draws', async () => {
    vi.useFakeTimers();
    const done = convertMermaidToDrawio(document, SITE, 'flowchart LR\n A --> B');
    const { frame, sent, answer } = editorFrame();
    expect(frame.getAttribute('src')).toContain('embed=1');
    answer({ event: 'init' });
    expect(sent[0]).toEqual({ action: 'load', descriptor: { format: 'mermaid', data: 'flowchart LR\n A --> B' }, autosave: 1 });
    answer({ event: 'load', xml: '' });
    expect(sent[1]).toEqual({ action: 'export', format: 'xml' });
    // Not drawn yet: asked again.
    answer({ event: 'export', format: 'xml', xml: '<mxfile/>' });
    vi.advanceTimersByTime(300);
    expect(sent[2]).toEqual({ action: 'export', format: 'xml' });
    answer({ event: 'export', format: 'xml', xml: DIAGRAM });
    await expect(done).resolves.toBe(DIAGRAM);
    expect(document.querySelector('iframe')).toBeNull();
  });

  it('gives up when the editor does not answer', async () => {
    vi.useFakeTimers();
    const done = convertMermaidToDrawio(document, SITE, 'pie\n "a": 1', 1000);
    vi.advanceTimersByTime(1000);
    await expect(done).rejects.toThrow('did not answer');
    expect(document.querySelector('iframe')).toBeNull();
  });
});

describe('mermaidFileName', () => {
  it('names the file after the diagram’s title, or its kind', () => {
    expect(mermaidFileName('---\ntitle: Sign-up flow\n---\nflowchart LR\n A --> B')).toBe('Sign-up flow.drawio');
    expect(mermaidFileName('%% a note\nsequenceDiagram\n A->>B: hi')).toBe('sequenceDiagram.drawio');
    expect(mermaidFileName('')).toBe('diagram.drawio');
  });
});
