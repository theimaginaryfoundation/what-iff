import { applyCarriageReturns, ansiToHtml, outputViews, sanitizeOutputHtml } from './notebook-outputs';

describe('notebook outputs', () => {
  it('merges consecutive writes to a stream and applies carriage returns', () => {
    expect(
      outputViews([
        { output_type: 'stream', name: 'stdout', text: ['a\n'] },
        { output_type: 'stream', name: 'stdout', text: 'b' },
        { output_type: 'stream', name: 'stderr', text: 'c' },
      ]),
    ).toEqual([
      { kind: 'text', text: 'a\nb', stderr: false },
      { kind: 'text', text: 'c', stderr: true },
    ]);
    expect(applyCarriageReturns('10%\r50%\r100%\ndone')).toBe('100%\ndone');
    expect(applyCarriageReturns('long line\rshort')).toBe('shortline');
  });

  it('shows tracebacks in colour with their text escaped', () => {
    const [view] = outputViews([{ output_type: 'error', ename: 'E', evalue: 'x', traceback: ['\u001b[0;31mE<b>\u001b[0m: x'] }]);
    expect(view).toEqual({ kind: 'ansi', html: '<span class="nb-ansi-red">E&lt;b&gt;</span>: x', stderr: true });
    expect(ansiToHtml('\u001b[1;32mok\u001b[0m')).toBe('<span class="nb-ansi-green nb-ansi-bold">ok</span>');
    expect(ansiToHtml('\u001b[38;5;196mskipped\u001b[0m')).toBe('skipped');
  });

  it('picks the richest safe representation', () => {
    const view = (data: Record<string, unknown>, metadata = {}) => outputViews([{ output_type: 'display_data', data, metadata }])[0];
    expect(view({ 'text/html': '<table><tr><td>1</td></tr></table>', 'text/plain': 'x' })).toEqual({ kind: 'html', html: '<table><tbody><tr><td>1</td></tr></tbody></table>' });
    expect(view({ 'image/png': 'AA\nAA\n', 'text/plain': 'x' }, { 'image/png': { width: 40 } })).toEqual({ kind: 'image', src: 'data:image/png;base64,AAAA', width: 40 });
    expect(view({ 'text/markdown': ['# a'] })).toEqual({ kind: 'markdown', text: '# a' });
    expect(view({ 'application/json': { a: 1 } })).toEqual({ kind: 'json', text: '{\n  "a": 1\n}' });
    expect(view({ 'application/vnd.jupyter.widget-view+json': {} })).toEqual({ kind: 'unsupported', mime: 'application/vnd.jupyter.widget-view+json' });
  });

  it('prefers the fallback over scripted HTML, and shows SVG only as an image', () => {
    const view = (data: Record<string, unknown>) => outputViews([{ output_type: 'execute_result', data, metadata: {} }])[0];
    expect(view({ 'text/html': '<div id="p"></div><script>plot()</script>', 'image/png': 'AAAA' })).toMatchObject({ kind: 'image' });
    expect(view({ 'text/html': '<script>x()</script>', 'text/plain': '<Figure>' })).toEqual({ kind: 'text', text: '<Figure>', stderr: false });
    const svg = view({ 'image/svg+xml': '<svg onload="x()"/>' });
    expect(svg.kind).toBe('image');
    expect((svg as { src: string }).src.startsWith('data:image/svg+xml')).toBe(true);
  });

  it('strips everything active from HTML output', () => {
    const clean = sanitizeOutputHtml(
      `<div onclick="x()" style="color:red">a<script>x()</script><style>body{display:none}</style><iframe src="https://e.test"></iframe><form action="https://e.test"><input name="p"><button>go</button></form>` +
        `<img src="https://e.test/t.gif"><img src="data:image/png;base64,AAAA"><a href="javascript:x()">j</a><a href="https://e.test">l</a></div>`,
    );
    for (const bad of ['onclick', '<script', '<style', '<iframe', '<form', '<input', '<button', 'e.test/t.gif', 'javascript:']) expect(clean).not.toContain(bad);
    expect(clean).toContain('data:image/png;base64,AAAA');
    expect(clean).toContain('target="_blank"');
    expect(clean).toContain('rel="noopener noreferrer"');
  });
});
