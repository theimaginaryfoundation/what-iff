import { escapeXml, parseXml, xmlSafe } from './ooxml';

describe('escapeXml', () => {
  it('escapes markup and drops characters XML cannot hold', () => {
    expect(escapeXml(`a<b & "c"`)).toBe('a&lt;b &amp; &quot;c&quot;');
    expect(escapeXml('see \u000b here\u000c\u0000')).toBe('see  here');
    expect(escapeXml('tab\tline\ncr\r')).toBe('tab\tline\ncr\r');
  });

  it('keeps paired surrogates and drops lone ones', () => {
    expect(xmlSafe('😀')).toBe('😀');
    expect(xmlSafe('a\ud83d b\ude00 c￿')).toBe('a b c');
  });

  it('writes text a parser reads back', () => {
    const text = 'pasted\u000bfrom a PDF\u001f';
    expect(() => parseXml(`<t>${escapeXml(text)}</t>`)).not.toThrow();
  });
});
