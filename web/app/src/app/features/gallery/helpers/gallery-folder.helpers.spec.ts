import {
  breadcrumbsFor,
  childFolderTiles,
  folderLabel,
  folderName,
  folderPathError,
  isWithinFolder,
  moveDestinations,
  normalizeFolderPath,
  parentFolder,
} from './gallery-folder.helpers';

describe('gallery folder helpers', () => {
  describe('normalizeFolderPath', () => {
    it('trims, lower-cases and drops empty segments, like the server', () => {
      expect(normalizeFolderPath('')).toBe('');
      expect(normalizeFolderPath('/')).toBe('');
      expect(normalizeFolderPath('  Charts / Oura  ')).toBe('charts/oura');
      expect(normalizeFolderPath('/charts//oura/')).toBe('charts/oura');
      expect(normalizeFolderPath('charts\\oura')).toBe('charts/oura');
      expect(normalizeFolderPath('Résumé/Ünï')).toBe('résumé/ünï');
    });

    it('refuses what the server refuses', () => {
      expect(normalizeFolderPath('a/./b')).toBeNull();
      expect(normalizeFolderPath('../up')).toBeNull();
      expect(normalizeFolderPath('bad\u0000name')).toBeNull();
      expect(normalizeFolderPath('a'.repeat(65))).toBeNull();
      expect(normalizeFolderPath('a/b/c/d/e/f/g/h/i')).toBeNull();
      expect(normalizeFolderPath(`${'a'.repeat(60)}/`.repeat(5))).toBeNull();
    });

    it('accepts the limits exactly', () => {
      expect(normalizeFolderPath('a'.repeat(64))).toBe('a'.repeat(64));
      expect(normalizeFolderPath('a/b/c/d/e/f/g/h')).toBe('a/b/c/d/e/f/g/h');
    });

    it('explains why a path is refused', () => {
      expect(folderPathError('a/../b')).toContain('not allowed');
      expect(folderPathError('a/b/c/d/e/f/g/h/i')).toContain('nest at most');
      expect(folderPathError('fine/path')).toBeNull();
    });
  });

  it('knows what is inside a folder, without confusing siblings that share a prefix', () => {
    expect(isWithinFolder('charts', 'charts')).toBe(true);
    expect(isWithinFolder('charts/oura', 'charts')).toBe(true);
    expect(isWithinFolder('anything', '')).toBe(true);
    expect(isWithinFolder('charts-old', 'charts')).toBe(false);
    expect(isWithinFolder('', 'charts')).toBe(false);
  });

  it('splits a path into a name and a parent', () => {
    expect(folderName('charts/oura')).toBe('oura');
    expect(folderName('charts')).toBe('charts');
    expect(parentFolder('charts/oura')).toBe('charts');
    expect(parentFolder('charts')).toBe('');
  });

  it('builds breadcrumbs from the top level down', () => {
    expect(breadcrumbsFor('')).toEqual([{ label: 'Gallery', path: '' }]);
    expect(breadcrumbsFor('charts/oura')).toEqual([
      { label: 'Gallery', path: '' },
      { label: 'charts', path: 'charts' },
      { label: 'oura', path: 'charts/oura' },
    ]);
  });

  describe('childFolderTiles', () => {
    const folders = [
      { path: 'art', count: 1 },
      { path: 'charts', count: 1 },
      { path: 'charts/oura', count: 2 },
      { path: 'charts/oura/hrv', count: 3 },
      { path: 'charts-old', count: 4 },
    ];

    it('shows the top level as its direct children, each counting everything beneath it', () => {
      expect(childFolderTiles(folders, '')).toEqual([
        { path: 'art', name: 'art', count: 1 },
        { path: 'charts', name: 'charts', count: 6 },
        { path: 'charts-old', name: 'charts-old', count: 4 },
      ]);
    });

    it('shows one level at a time inside a folder', () => {
      expect(childFolderTiles(folders, 'charts')).toEqual([{ path: 'charts/oura', name: 'oura', count: 5 }]);
      expect(childFolderTiles(folders, 'charts/oura')).toEqual([{ path: 'charts/oura/hrv', name: 'hrv', count: 3 }]);
      expect(childFolderTiles(folders, 'charts/oura/hrv')).toEqual([]);
    });

    it('includes a folder that only exists in this session, and parents with no image of their own', () => {
      expect(childFolderTiles([{ path: 'a/b/c', count: 2 }], '')).toEqual([{ path: 'a', name: 'a', count: 2 }]);
      expect(childFolderTiles([], '', ['new'])).toEqual([{ path: 'new', name: 'new', count: 0 }]);
      expect(childFolderTiles([{ path: 'new', count: 3 }], '', ['new'])).toEqual([{ path: 'new', name: 'new', count: 3 }]);
      expect(childFolderTiles([], 'a', ['a/b/c'])).toEqual([{ path: 'a/b', name: 'b', count: 0 }]);
    });
  });

  it('lists every folder a move could target, parents included, sorted', () => {
    expect(
      moveDestinations(
        [
          { path: 'charts/oura', count: 1 },
          { path: 'art', count: 1 },
        ],
        ['zeta/new'],
      ),
    ).toEqual(['art', 'charts', 'charts/oura', 'zeta', 'zeta/new']);
    expect(moveDestinations([])).toEqual([]);
  });

  it('labels the top level as the gallery', () => {
    expect(folderLabel('')).toBe('Gallery');
    expect(folderLabel('charts/oura')).toBe('charts / oura');
  });
});
