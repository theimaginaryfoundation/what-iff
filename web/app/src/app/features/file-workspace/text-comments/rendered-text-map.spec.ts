import { describe, expect, it } from 'vitest';

import { RenderedTextMap } from './rendered-text-map';

const SOURCE =
  '# Plan\n\nWe will ship **three major features** this quarter.\n\nSee [the roadmap](https://example.com) for more.\n\n- three\n';

function rendered(): HTMLElement {
  const root = document.createElement('div');
  root.innerHTML =
    '<h1>Plan</h1><p>We will ship <strong>three major features</strong> this quarter.</p><p>See <a href="https://example.com">the roadmap</a> for more.</p><ul><li>three</li></ul>';
  document.body.appendChild(root);
  return root;
}

describe('RenderedTextMap', () => {
  it('finds a source range in the formatted text by its words, across markup', () => {
    const map = new RenderedTextMap(rendered(), SOURCE);
    const quote = '**three major features** this';
    const start = SOURCE.indexOf(quote);
    expect(map.range(start, start + quote.length)!.toString()).toBe('three major features</strong> this'.replace('</strong>', ''));
    const link = SOURCE.indexOf('[the roadmap]');
    expect(map.range(link, link + '[the roadmap](https://example.com) for'.length)!.toString()).toBe('the roadmap for');
  });

  it('takes the occurrence nearest where the range is in the source', () => {
    const map = new RenderedTextMap(rendered(), SOURCE);
    const last = SOURCE.lastIndexOf('three');
    const range = map.range(last, last + 5)!;
    expect(range.startContainer.parentElement?.tagName).toBe('LI');
  });

  it('turns a selection in the formatted text into its source range, and a click into a source offset', () => {
    const root = rendered();
    const map = new RenderedTextMap(root, SOURCE);
    const strong = root.querySelector('strong')!.firstChild!;
    const selection = document.createRange();
    selection.setStart(strong, 6);
    selection.setEnd(strong, 11);
    const got = map.sourceRange(selection)!;
    expect(SOURCE.slice(got.start, got.end)).toBe('major');
    const at = map.offsetOf(strong, 8)!;
    expect(SOURCE.slice(at - 2, at + 3)).toBe('major');
  });
});
