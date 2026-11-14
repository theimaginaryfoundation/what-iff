import { describe, expect, it, vi } from 'vitest';

import { buildTree, foldersOf, treeRows } from './file-tree';
import { ShareApiService, SharedFile } from './share-api.service';
import { SHARE_ROOT, ShareFolderFiles, sharedAttachment, sharedPath } from './share-folder-files';
import { previewUrlFor } from './share.providers';

const f = (path: string, file_type = 'text/plain'): SharedFile => ({ path, file_type, content_version: 1 });

describe('previewUrlFor', () => {
  it('takes the web apps site on another origin, on https or this machine, even on the same site', () => {
    const share = 'https://share.example.com';
    expect(previewUrlFor('https://webapps.example.com/index.html', share)).toBe('https://webapps.example.com/index.html');
    expect(previewUrlFor('http://127.0.0.1:4200/preview/index.html', 'http://localhost:4204')).toBe(
      'http://127.0.0.1:4200/preview/index.html',
    );
    expect(previewUrlFor('https://share.example.com/preview/', share)).toBeNull();
    expect(previewUrlFor('http://webapps.example.com/', share)).toBeNull();
    expect(previewUrlFor('__PREVIEW_URL__', share)).toBeNull();
    expect(previewUrlFor('', share)).toBeNull();
  });
});

describe('the file tree', () => {
  const files = [f('index.html'), f('js/app.js'), f('css/b.css'), f('css/A.css'), f('js/lib/x.js'), f('README.md')];

  it('lists folders first, then files, each by name; only open folders show what they hold', () => {
    const tree = buildTree(files);
    const names = (open: string[]) =>
      treeRows(tree, new Set(open)).map(r => `${'  '.repeat(r.depth)}${r.name}${r.kind === 'folder' ? '/' : ''}`);
    expect(names([])).toEqual(['css/', 'js/', 'index.html', 'README.md']);
    expect(names(['css'])).toEqual(['css/', '  A.css', '  b.css', 'js/', 'index.html', 'README.md']);
    expect(names(['js', 'js/lib'])).toEqual(['css/', 'js/', '  lib/', '    x.js', '  app.js', 'index.html', 'README.md']);
  });

  it('knows the folders a path is in', () => {
    expect(foldersOf('a/b/c.js')).toEqual(['a', 'a/b']);
    expect(foldersOf('c.js')).toEqual([]);
  });
});

describe('ShareFolderFiles', () => {
  it('stands a web app’s files in a folder of their own, by their paths, and reads them from the share', async () => {
    const api = {
      files: vi.fn(async () => [f('index.html', 'text/html'), f('img/a.png', 'image/png')]),
      file: vi.fn(async () => new Blob(['x'])),
      content: vi.fn(),
    };
    const files = Object.assign(Object.create(ShareFolderFiles.prototype), {
      api,
      key: '',
      single: null,
      listing: null,
      now: () => 0,
    }) as ShareFolderFiles;
    files.use('k', null);
    expect([...(await files.list(SHARE_ROOT)).keys()]).toEqual(['index.html']);
    const img = (await files.list(`${SHARE_ROOT}/img`)).get('a.png')!;
    expect(img.id).toBe('shared:img/a.png');
    expect(sharedPath(img)).toBe('img/a.png');
    await files.download(img);
    expect(api.file).toHaveBeenCalledWith('k', 'img/a.png');
    expect(api.files).toHaveBeenCalledTimes(1);
    await files.list(SHARE_ROOT, true);
    expect(api.files).toHaveBeenCalledTimes(1); // a fresh listing within its time is the last one
    expect(await files.conversation()).toEqual(new Map());
  });

  it('a single file is its folder’s only file, read as the share’s content', async () => {
    const api = { files: vi.fn(), file: vi.fn(), content: vi.fn(async () => new Blob(['y'])) } as unknown as ShareApiService;
    const files = Object.assign(Object.create(ShareFolderFiles.prototype), {
      api,
      key: '',
      single: null,
      listing: null,
      now: () => 0,
    }) as ShareFolderFiles;
    files.use('k', f('page.html', 'text/html'));
    const page = (await files.list(SHARE_ROOT)).get('page.html')!;
    expect(page).toEqual(sharedAttachment(f('page.html', 'text/html')));
    await files.download(page);
    expect(api.content).toHaveBeenCalledWith('k');
    expect((await files.list('elsewhere')).size).toBe(0);
  });
});
