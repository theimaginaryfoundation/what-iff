import { entitySegments, normalizeEntityName } from './entity.helpers';

describe('entitySegments', () => {
  it('splits text around [[links]] and tidies link names', () => {
    expect(entitySegments('Works with [[Dana  Lee]] on [[Project Orchard]].')).toEqual([
      { text: 'Works with ', link: false },
      { text: 'Dana Lee', link: true },
      { text: ' on ', link: false },
      { text: 'Project Orchard', link: true },
      { text: '.', link: false },
    ]);
  });

  it('leaves text without links, and unclosed brackets, as plain text', () => {
    expect(entitySegments('Just [[text')).toEqual([{ text: 'Just [[text', link: false }]);
    expect(entitySegments('')).toEqual([]);
  });
});

describe('normalizeEntityName', () => {
  it('matches the server key normalization', () => {
    expect(normalizeEntityName('  Alex   RIVERA ')).toBe('alex rivera');
  });
});
