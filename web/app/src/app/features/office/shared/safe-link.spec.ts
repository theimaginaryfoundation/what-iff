import { linkProperties, safeLinkUrl, savedLinkUrl } from './safe-link';

describe('safeLinkUrl', () => {
  it('keeps web links, mail links and anchors', () => {
    for (const url of ['https://a.test/x', 'HTTP://a.test', 'mailto:a@b.test', '#Sheet1!A1']) {
      expect(safeLinkUrl(url)).toBe(url);
    }
  });

  it('refuses script, data and local targets, however they are disguised', () => {
    for (const url of ['javascript:alert(1)', ' JaVaScRiPt:alert(1)', 'java\tscript:alert(1)', '\u0001javascript:x',
      'data:text/html,x', 'vbscript:x', 'file:///etc/passwd', 'relative/path', '', null, undefined]) {
      expect(safeLinkUrl(url)).toBeNull();
    }
  });
});

describe('linkProperties', () => {
  it('keeps a link this app will not follow for the saved file, without making it clickable', () => {
    expect(linkProperties('https://a.test')).toEqual({ url: 'https://a.test' });
    const kept = linkProperties('file:///share/plan.docx');
    expect(kept).toEqual({ url: '', keptUrl: 'file:///share/plan.docx' });
    expect(savedLinkUrl(kept)).toBe('file:///share/plan.docx');
    expect(savedLinkUrl({ url: 'https://a.test' })).toBe('https://a.test');
  });
});
