import { applyCarriageReturns, ansiToHtml, notebookLoaders, outputViews, sanitizeOutputHtml, withoutRemoteImages } from './notebook-outputs';

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
    expect(view({ 'application/x-unknown': {} })).toEqual({ kind: 'unsupported', mime: 'application/x-unknown' });
  });

  it('offers scripted output with its best static fallback, and shows SVG only as an image', () => {
    const view = (data: Record<string, unknown>) => outputViews([{ output_type: 'execute_result', data, metadata: {} }])[0];
    expect(view({ 'text/html': '<div id="p"></div><script>plot()</script>', 'image/png': 'AAAA' })).toMatchObject({ kind: 'interactive', fallback: { kind: 'image' } });
    expect(view({ 'text/html': '<div></div><script>x()</script>', 'text/plain': '<Figure>' })).toMatchObject({ kind: 'interactive', fallback: { kind: 'text', text: '<Figure>' } });
    expect(view({ 'text/html': '<div></div><script>x()</script>' })).toMatchObject({ kind: 'interactive', fallback: null });
    const svg = view({ 'image/svg+xml': '<svg onload="x()"/>' });
    expect(svg.kind).toBe('image');
    expect((svg as { src: string }).src.startsWith('data:image/svg+xml')).toBe(true);
  });

  it('builds a frame for Plotly and Vega from their own data, safe inside the script', () => {
    const doc = (data: Record<string, unknown>) => (outputViews([{ output_type: 'display_data', data, metadata: {} }])[0] as { document: string }).document;
    const plotly = doc({ 'application/vnd.plotly.v1+json': { data: [{ y: ['</script><script>evil()</script>'] }], layout: {} }, 'text/plain': 'Figure' });
    expect(plotly).toContain('plotly.min.js');
    expect(plotly).toContain('Plotly.newPlot');
    expect(plotly).not.toContain('</script><script>evil()');
    expect(doc({ 'application/vnd.vegalite.v5+json': { mark: 'bar' } })).toContain('vega-lite@5');
    expect(doc({ 'application/vnd.vegalite.v6+json': { mark: 'bar' } })).toContain('vega-lite@6');
    expect(doc({ 'application/vnd.vega.v5.3+json': {} })).toContain('vegaEmbed');
  });

  it('holds every frame to a policy with no forms, no base, and https only', () => {
    const [view] = outputViews([{ output_type: 'display_data', data: { 'text/html': '<script>x()</script><p>a</p>' }, metadata: {} }]);
    const policy = /Content-Security-Policy" content="([^"]*)"/.exec((view as { document: string }).document)?.[1];
    expect(policy).toContain("default-src 'none'");
    expect(policy).toContain("form-action 'none'");
    expect(policy).toContain("base-uri 'none'");
    expect(policy).not.toMatch(/http:|\*/);
  });

  it('measures the body, so a frame can shrink to its content', () => {
    const [view] = outputViews([{ output_type: 'display_data', data: { 'text/html': '<script>x()</script><p>a</p>' }, metadata: {} }]);
    expect((view as { document: string }).document).toContain('document.body.getBoundingClientRect().height');
  });

  it('adds require only for output that uses it', () => {
    const doc = (html: string) => (outputViews([{ output_type: 'display_data', data: { 'text/html': html }, metadata: {} }])[0] as { document: string }).document;
    expect(doc('<p>a</p><script>require(["x"], f)</script>')).toContain('require.js');
    expect(doc('<p>a</p><script>x()</script>')).not.toContain('require.js');
  });

  it('points a widget at Run, whatever its fallback', () => {
    expect(outputViews([{ output_type: 'display_data', data: { 'application/vnd.jupyter.widget-view+json': {}, 'text/plain': 'IntSlider()' }, metadata: {} }])).toEqual([{ kind: 'widget' }]);
  });

  it('treats library loaders as nothing to show, and hands them to every frame', () => {
    const load = { output_type: 'display_data', metadata: {}, data: { 'text/html': '<script>window.Lib=1</script>' } };
    const bokeh = { output_type: 'display_data', metadata: {}, data: { 'application/javascript': 'window.Bokeh=1', 'application/vnd.bokehjs_load.v0+json': '', 'text/html': '<div>Loading</div>' } };
    const js = { output_type: 'display_data', metadata: {}, data: { 'application/javascript': 'window.J=1', 'text/plain': '<Js>' } };
    const plain = { output_type: 'stream', name: 'stdout', text: 'x' };
    expect(outputViews([load, bokeh])).toEqual([]);
    const loaders = notebookLoaders([{ outputs: [load, plain] }, { outputs: [bokeh, js] }]);
    expect(loaders).toBe('<script>window.Lib=1</script><script>window.Bokeh=1</script>');
    expect(outputViews([js])[0].kind).toBe('interactive');
    const [plot] = outputViews([{ output_type: 'display_data', metadata: {}, data: { 'text/html': '<div></div>', 'application/javascript': 'draw()' } }], loaders);
    expect((plot as { document: string }).document).toContain('window.Bokeh=1');
    expect((plot as { document: string }).document).toContain('draw()');
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

describe('nothing loads from elsewhere when a notebook opens', () => {
  it('strips CSS url(), SVG image and filter hrefs, and remote paint servers from output HTML', () => {
    const html = sanitizeOutputHtml(
      '<div style="background-image:url(https://e.test/a.png)">a</div><p style="color:red">b</p>' +
        '<svg><image href="https://e.test/b.png"></image><image xlink:href="https://e.test/c.png"></image>' +
        '<filter><feImage href="https://e.test/d.png"></feImage></filter><rect fill="url(https://e.test/e#p)"></rect>' +
        '<rect fill="url(#local)"></rect></svg><img src="data:image/png;base64,AAAA">',
    );
    expect(html).not.toContain('e.test');
    expect(html).toContain('color:red');
    expect(html).toContain('url(#local)');
    expect(html).toContain('data:image/png;base64,AAAA');
  });

  it('drops style that uses any function but colour and size ones, including escaped ones', () => {
    const html = sanitizeOutputHtml(
      '<p style="background:\\75 rl(https://e.test/a)">a</p><p style="cursor:-webkit-image-set(\'https://e.test/b\' 1x)">b</p>' +
        '<p style="color:rgb(1,2,3);width:calc(100% - 2px)">c</p>',
    );
    expect(html).not.toContain('e.test');
    expect(html).toContain('rgb(1,2,3)');
  });

  it('turns remote Markdown images into links, and keeps embedded ones', () => {
    const md = withoutRemoteImages('![chart](https://e.test/c.png "t") ![ref][r] ![ok](data:image/png;base64,AA) <img src="https://e.test/x.png" alt="x">');
    expect(md).toContain('[chart](https://e.test/c.png)');
    expect(md).toContain('[ref][r]');
    expect(md).toContain('![ok](data:image/png;base64,AA)');
    expect(md).not.toContain('src="https://e.test/x.png"');
  });
});
