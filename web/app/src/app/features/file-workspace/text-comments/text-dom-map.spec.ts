import { describe, expect, it } from 'vitest';

import { TextDomMap } from './text-dom-map';

const NBSP = ' ';

/** Monaco's colorize shape: lines of token spans, nbsp for spaces and tabs, <br/> between lines. */
function colorized(text: string, tabSize = 4): HTMLElement {
  const root = document.createElement('pre');
  const lines = text.split(/\r?\n/);
  root.innerHTML = lines
    .map(line => {
      let col = 0;
      let out = '';
      for (const ch of line) {
        if (ch === '\t') {
          const w = tabSize - (col % tabSize);
          out += NBSP.repeat(w);
          col += w;
        } else {
          out += ch === ' ' ? NBSP : ch.replace('&', '&amp;').replace('<', '&lt;');
          col++;
        }
      }
      // Split each line into two spans, as tokens would.
      const mid = Math.floor(out.length / 2);
      return `<span class="mtk1">${out.slice(0, mid)}</span><span class="mtk2">${out.slice(mid)}</span>`;
    })
    .map(line => `${line}<br/>`)
    .join('');
  return root;
}

function plain(text: string): HTMLElement {
  const root = document.createElement('pre');
  root.textContent = text;
  return root;
}

describe('TextDomMap', () => {
  const text = 'package main\r\n\r\nfunc main() {\r\n\tfmt.Println("a < b")\r\n}\r\n';

  for (const [name, render] of [
    ['plain text', plain],
    ['Monaco-coloured text', colorized],
  ] as const) {
    it(`maps ranges both ways over ${name}`, () => {
      const root = render(text);
      document.body.appendChild(root);
      const map = TextDomMap.build(root, text)!;
      expect(map).not.toBeNull();
      for (const words of ['package main', 'func main', 'fmt.Println("a < b")', 'Println']) {
        const start = text.indexOf(words);
        const range = map.range(start, start + words.length)!;
        expect(range.toString().replace(new RegExp(NBSP, 'g'), ' ')).toBe(words);
        expect(map.offsetOf(range.startContainer, range.startOffset)).toBe(start);
        expect(map.offsetOf(range.endContainer, range.endOffset)).toBe(start + words.length);
      }
      // A range from a tab spans the tab's whole width.
      const tab = text.indexOf('\t');
      const shownTab = render === plain ? '\tfmt' : '    fmt';
      expect(
        map
          .range(tab, tab + 4)!
          .toString()
          .replace(new RegExp(NBSP, 'g'), ' '),
      ).toBe(shownTab);
      root.remove();
    });
  }

  it('declines a rendering that does not line up with the text', () => {
    expect(TextDomMap.build(plain('something else'), 'the text')).toBeNull();
    expect(TextDomMap.build(plain('the text and more'), 'the text')).toBeNull();
  });
});
