import { cssColor, cssFontFamily, paragraphsToHtml } from './text-dom';

describe('run styles from a file', () => {
  it('quotes a plain font name and refuses one that could add declarations', () => {
    expect(cssFontFamily('Segoe UI')).toBe("'Segoe UI'");
    expect(cssFontFamily('Arial;background:url(https://e.test/x)')).toBeNull();
    expect(cssFontFamily('a}b')).toBeNull();
  });

  it('accepts hex and keyword colours only', () => {
    expect(cssColor('#1F2937')).toBe('#1F2937');
    expect(cssColor('red')).toBe('red');
    expect(cssColor('red;background:url(x)')).toBeNull();
  });

  it('never writes an unsafe value into the style attribute', () => {
    const html = paragraphsToHtml([
      { align: 'left', runs: [{ text: 'x', fontFace: 'X;background:url(https://e.test/a)', color: 'red;x:y', highlight: 'url(a)' }] },
    ]);
    expect(html).not.toContain('url(');
    expect(html).not.toContain('x:y');
  });
});
