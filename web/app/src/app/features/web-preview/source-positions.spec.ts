import { describe, expect, it } from 'vitest';

import { SOURCE_ATTRIBUTE, anchorText, parseSourceRange, withSourcePositions } from './source-positions';

/** Each element's source range, read back from the page and applied to the original text. */
function tagsAt(original: string, out: string): string[] {
  const doc = new DOMParser().parseFromString(out, 'text/html');
  const text = anchorText(original);
  const els = [
    ...doc.querySelectorAll(`[${SOURCE_ATTRIBUTE}]`),
    ...[...doc.querySelectorAll('template')].flatMap(t => [...t.content.querySelectorAll(`[${SOURCE_ATTRIBUTE}]`)]),
  ];
  return els.map(el => {
    const range = parseSourceRange(el.getAttribute(SOURCE_ATTRIBUTE))!;
    expect(range.file).toBe('f1');
    return text.slice(range.start, range.end);
  });
}

describe('withSourcePositions', () => {
  it('marks each element with its opening tag’s range in the file', () => {
    const html = `<!doctype html>\n<html lang="en"><head><title>T</title></head>\n<body>\n  <a href="x?a>b" class="c">link</a><br/>\n  <img src="i.png" />\n</body></html>`;
    expect(tagsAt(html, withSourcePositions(html, 'f1'))).toEqual([
      '<html lang="en">',
      '<head>',
      '<title>',
      '<body>',
      '<a href="x?a>b" class="c">',
      '<br/>',
      '<img src="i.png" />',
    ]);
  });

  it('leaves elements the parser made up unmarked, and finds a template’s', () => {
    const html = `<p>One<template><span>Two</span></template></p>`;
    const out = withSourcePositions(html, 'f1');
    expect(tagsAt(html, out)).toEqual(['<p>', '<template>', '<span>']);
    const doc = new DOMParser().parseFromString(out, 'text/html');
    expect(doc.body.hasAttribute(SOURCE_ATTRIBUTE)).toBe(false);
  });

  it('counts as comment anchors do, after a byte order mark', () => {
    const html = '﻿<p>é😀</p><b>x</b>';
    expect(tagsAt(html, withSourcePositions(html, 'f1'))).toEqual(['<p>', '<b>']);
  });

  it('puts a script first in the head, or where the head would be', () => {
    const script = 'window.x = 1; "</script>";';
    const withHead = withSourcePositions('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>', 'f1', script);
    expect(withHead).toMatch(/<head data-wi-src="f1:\d+-\d+"><script>window\.x = 1; "<\\\/script>";<\/script><meta/);
    // No head: after the doctype, so the page keeps its standards mode.
    const bare = withSourcePositions('<!DOCTYPE html><p>Hi</p>', 'f1', 'x()');
    expect(bare.startsWith('<!DOCTYPE html><script>x()</script><p')).toBe(true);
    expect(new DOMParser().parseFromString(bare, 'text/html').compatMode).toBe('CSS1Compat');
  });

  it('reads a range back, and nothing else', () => {
    expect(parseSourceRange('abc-123:10-25')).toEqual({ file: 'abc-123', start: 10, end: 25 });
    expect(parseSourceRange('f:5-5')).toBeNull();
    expect(parseSourceRange('nonsense')).toBeNull();
  });
});
