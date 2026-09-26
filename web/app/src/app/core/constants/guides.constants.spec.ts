import { guideUrl } from './guides.constants';

describe('guideUrl', () => {
  const docs = 'https://whatiff.chat/docs.html';

  it('resolves guide pages next to the docs index', () => {
    expect(guideUrl('docs', docs)).toBe('https://whatiff.chat/docs.html');
    expect(guideUrl('gettingStarted', docs)).toBe('https://whatiff.chat/guides/getting-started.html');
    expect(guideUrl('continuity', docs)).toBe('https://whatiff.chat/guides/building-continuity.html');
  });

  it('follows a docs site hosted under a path', () => {
    expect(guideUrl('context', 'https://example.org/help/docs.html')).toBe('https://example.org/help/guides/how-context-works.html');
  });

  it('returns null when docs are not configured', () => {
    expect(guideUrl('gettingStarted', '')).toBeNull();
    expect(guideUrl('gettingStarted', '__DOCS_URL__')).toBeNull();
    expect(guideUrl('gettingStarted', 'javascript:alert(1)')).toBeNull();
  });
});
