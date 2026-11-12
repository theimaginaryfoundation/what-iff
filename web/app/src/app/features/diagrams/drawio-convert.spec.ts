import { afterEach, describe, expect, it, vi } from 'vitest';

import { convertMermaidToDrawio, convertVisioToDrawio } from './drawio-convert';

const SITE = 'https://diagrams.example.test/index.html';
const ORIGIN = 'https://diagrams.example.test';
const DIAGRAM =
  '<mxfile><diagram id="p"><mxGraphModel><root><mxCell id="0"/><mxCell id="2" vertex="1" parent="1"/></root></mxGraphModel></diagram></mxfile>';

/** The hidden editor's frame, what was sent to it, and a way to answer as it. */
function editorFrame() {
  const frame = document.querySelector('iframe') as HTMLIFrameElement;
  const sent: Record<string, unknown>[] = [];
  vi.spyOn(frame.contentWindow!, 'postMessage').mockImplementation(((m: string) => sent.push(JSON.parse(m))) as typeof window.postMessage);
  const answer = (message: Record<string, unknown>) =>
    window.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(message), origin: ORIGIN, source: frame.contentWindow }));
  return { frame, sent, answer };
}

/** Waits for the converter to put its frame in the page (the Visio one reads the file first). */
async function frameAppears() {
  await vi.waitFor(() => expect(document.querySelector('iframe')).not.toBeNull());
}

describe('convertMermaidToDrawio', () => {
  afterEach(() => vi.useRealTimers());

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

describe('convertVisioToDrawio', () => {
  it('has our plugin import the file, then loads and exports what it made', async () => {
    const done = convertVisioToDrawio(document, SITE, new Blob([new Uint8Array([80, 75, 3, 4])]), 'flow.vsdx');
    await frameAppears();
    const { sent, answer } = editorFrame();
    answer({ event: 'init' });
    expect(sent[0]).toEqual({ action: 'whatiff-import-visio', data: btoa('PK\x03\x04'), name: 'flow.vsdx' });
    answer({ event: 'whatiff-imported', xml: '<mxfile><diagram>compressed</diagram></mxfile>' });
    expect(sent[1]).toEqual({ action: 'load', xml: '<mxfile><diagram>compressed</diagram></mxfile>', autosave: 1 });
    answer({ event: 'load', xml: '' });
    answer({ event: 'export', format: 'xml', xml: DIAGRAM });
    await expect(done).resolves.toBe(DIAGRAM);
    expect(document.querySelector('iframe')).toBeNull();
  });

  it('fails with the importer’s reason', async () => {
    const done = convertVisioToDrawio(document, SITE, new Blob(['nope']), 'bad.vsdx');
    await frameAppears();
    const { answer } = editorFrame();
    answer({ event: 'init' });
    answer({ event: 'whatiff-import-failed', message: 'Invalid file' });
    await expect(done).rejects.toThrow('could not be converted: Invalid file');
    expect(document.querySelector('iframe')).toBeNull();
  });
});
