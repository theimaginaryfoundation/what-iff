import { describe, expect, it } from 'vitest';

import { cssReferences, pageReferences, rewriteCss, rewritePage, serializePage, servedType, siblingName } from './page-assets';

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html');

describe('siblingName', () => {
  it('names a file in the page’s own folder, without ./, a query or a fragment', () => {
    expect(siblingName('styles.css')).toBe('styles.css');
    expect(siblingName('./app.js?v=2')).toBe('app.js');
    expect(siblingName(' logo.png#top ')).toBe('logo.png');
    expect(siblingName('my%20photo.jpg')).toBe('my photo.jpg');
  });

  it('leaves everything else alone: other folders, absolute and scheme URLs, fragments', () => {
    for (const ref of [
      'css/app.css',
      '../x.css',
      '/abs.css',
      'https://cdn.example/x.js',
      '//cdn.example/x.js',
      'data:image/png;base64,AA',
      'blob:x',
      '#top',
      '',
      '.',
      null,
    ]) {
      expect(siblingName(ref)).toBeNull();
    }
  });
});

describe('page references', () => {
  it('finds files in elements, srcset, <style> and style attributes, once each', () => {
    const doc = parse(`<!doctype html><html><head>
      <link rel="stylesheet" href="styles.css"><script src="./app.js"></script>
      <style>body { background: url("bg.png") } @import 'extra.css';</style>
    </head><body>
      <img src="logo.png" srcset="logo.png 1x, logo@2x.png 2x">
      <div style="background-image: url(tile.svg)"></div>
      <video src="clip.mp4" poster="poster.jpg"></video>
      <a href="other.html">not a resource</a>
      <script src="https://cdn.example/lib.js"></script>
    </body></html>`);
    expect([...pageReferences(doc)].sort()).toEqual(
      ['app.js', 'bg.png', 'clip.mp4', 'extra.css', 'logo.png', 'logo@2x.png', 'poster.jpg', 'styles.css', 'tile.svg'].sort(),
    );
  });

  it('swaps each reference that has a URL, and leaves the rest as written', () => {
    const doc = parse(
      `<!DOCTYPE html><html><head><link rel="stylesheet" href="styles.css"><style>h1{background:url('bg.png')}</style></head>` +
        `<body><img src="logo.png" srcset="logo.png 1x, big.png 2x"><img src="gone.png"></body></html>`,
    );
    const urls = new Map([
      ['styles.css', 'blob:css'],
      ['bg.png', 'blob:bg'],
      ['logo.png', 'blob:logo'],
    ]);
    rewritePage(doc, urls);
    const html = serializePage(doc);
    expect(html.startsWith('<!DOCTYPE html>\n<html>')).toBe(true);
    expect(html).toContain('href="blob:css"');
    expect(html).toContain("url('blob:bg')");
    expect(html).toContain('src="blob:logo"');
    expect(html).toContain('srcset="blob:logo 1x, big.png 2x"');
    expect(html).toContain('src="gone.png"');
  });
});

describe('stylesheets', () => {
  it('finds and swaps url() and @import references beside the stylesheet', () => {
    const css = `@import "base.css"; @font-face { src: url(font.woff2) format("woff2"); } .a { background: url('img/x.png') }`;
    expect(cssReferences(css)).toEqual(['font.woff2', 'base.css']);
    const out = rewriteCss(
      css,
      new Map([
        ['base.css', 'blob:base'],
        ['font.woff2', 'blob:font'],
      ]),
    );
    expect(out).toContain('@import "blob:base"');
    expect(out).toContain('url(blob:font)');
    expect(out).toContain("url('img/x.png')");
  });
});

describe('servedType', () => {
  it('serves stylesheets and scripts by extension, anything else as stored', () => {
    expect(servedType('a.css', 'application/octet-stream')).toBe('text/css');
    expect(servedType('a.MJS', '')).toBe('text/javascript');
    expect(servedType('a.png', 'image/png')).toBe('image/png');
    expect(servedType('a.bin', null)).toBe('application/octet-stream');
  });
});
