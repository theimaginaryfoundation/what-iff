import { describe, expect, it } from 'vitest';

import { dataUrlToBlob, drawioUrl, exportName, readDrawioMessage } from './drawio-frame';

describe('drawio-frame', () => {
  it('opens the editor in embed mode with our JSON protocol and theme', () => {
    const url = new URL(drawioUrl('https://diagrams.example.test/index.html', { readOnly: false, dark: true }));
    expect(url.origin).toBe('https://diagrams.example.test');
    for (const [k, v] of Object.entries({ embed: '1', proto: 'json', noSaveBtn: '1', noExitBtn: '1', dark: '1' })) {
      expect(url.searchParams.get(k)).toBe(v);
    }
    expect(url.searchParams.has('chrome')).toBe(false);
  });

  it('views without menus or editing', () => {
    const url = new URL(drawioUrl('https://diagrams.example.test/index.html', { readOnly: true, dark: false }));
    expect(url.searchParams.get('chrome')).toBe('0');
    expect(url.searchParams.get('dark')).toBe('0');
  });

  it('reads only the editor frame’s own JSON messages', () => {
    const frame = {} as Window;
    const msg = (data: unknown, origin = 'https://diagrams.example.test', source: unknown = frame) =>
      readDrawioMessage(new MessageEvent('message', { data, origin, source: source as Window }), 'https://diagrams.example.test', frame);
    expect(msg(JSON.stringify({ event: 'autosave', xml: '<mxfile/>' }))).toEqual({ event: 'autosave', xml: '<mxfile/>' });
    expect(msg(JSON.stringify({ event: 'init' }), 'https://evil.example.test')).toBeNull();
    expect(msg(JSON.stringify({ event: 'init' }), undefined, {})).toBeNull();
    expect(msg('not json')).toBeNull();
    expect(msg({ event: 'init' })).toBeNull();
  });

  it('turns an export into bytes', async () => {
    const png = dataUrlToBlob('data:image/png;base64,iVBORw==');
    expect(png?.type).toBe('image/png');
    expect([...new Uint8Array(await png!.arrayBuffer())]).toEqual([0x89, 0x50, 0x4e, 0x47]);
    const svg = dataUrlToBlob('data:image/svg+xml,%3Csvg%2F%3E');
    expect(svg?.type).toBe('image/svg+xml');
    expect(await svg!.text()).toBe('<svg/>');
    expect(dataUrlToBlob('<svg/>')).toBeNull();
  });

  it('names a download after the diagram', () => {
    expect(exportName('flow.drawio', 'png')).toBe('flow.png');
    expect(exportName('Flow.drawio.xml', 'svg')).toBe('Flow.svg');
    expect(exportName('.drawio', 'png')).toBe('diagram.png');
  });
});
