import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { SiteProject } from './site-project';

type Tree = Record<string, Record<string, { text: string; version?: number; type?: string }>>;

/** A FolderFiles over an in-memory tree of folders, whose files can be changed between calls. */
function folders(tree: Tree) {
  const file = (folder: string, name: string) => {
    const entry = tree[folder]?.[name];
    return entry
      ? ({ id: `${folder}/${name}`, name, folder, file_type: entry.type ?? '', content_version: entry.version ?? 1 } as FileAttachment)
      : undefined;
  };
  const fake = {
    list: vi.fn(async (folder: string) => {
      const out = new Map<string, FileAttachment>();
      for (const name of Object.keys(tree[folder] ?? {})) out.set(name, file(folder, name)!);
      return out;
    }),
    download: vi.fn(async (f: FileAttachment) => new Blob([tree[f.folder ?? '']![f.name]!.text])),
  };
  return fake as typeof fake & FolderFiles;
}

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html');
const text = (body?: ArrayBuffer) => new TextDecoder().decode(body);

describe('SiteProject', () => {
  it('finds a path in the root folder and the folders beneath it, without case in folder names', async () => {
    const tree: Tree = {
      site: { 'index.html': { text: '<h1>Hi</h1>' } },
      'site/css': { 'app.css': { text: 'h1{}', type: 'application/octet-stream' } },
    };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    const css = await project.get('CSS/app.css');
    expect(css).toMatchObject({ status: 200, contentType: 'text/css; charset=utf-8' });
    expect(text(css.body)).toBe('h1{}');
    expect((await project.get('index.html')).contentType).toBe('text/html; charset=utf-8');
    expect(await project.get('css/App.css')).toMatchObject({ status: 404 });
    expect(await project.get('../secret.txt')).toMatchObject({ status: 400 });
    expect(project.place('a/b/c.js')).toEqual({ folder: 'site/a/b', name: 'c.js' });
    expect(new SiteProject(folders({}), '', new Map(), parse).place('x.js')).toEqual({ folder: '', name: 'x.js' });
  });

  it('serves the editor’s unsaved text for its page, and rewrites bare imports in modules and inline module scripts', async () => {
    const tree: Tree = {
      site: {
        'package.json': { text: JSON.stringify({ dependencies: { preact: '10.19.0' } }) },
        'main.js': { text: `import { h } from 'preact'; import './util.js';` },
      },
    };
    const unsaved = new Map([['index.html', `<script type="module">import { marked } from 'marked';</script><p>draft</p>`]]);
    const project = new SiteProject(folders(tree), 'site', unsaved, parse);
    expect(text((await project.get('main.js')).body)).toBe(`import { h } from 'https://esm.sh/preact@10.19.0'; import './util.js';`);
    const page = text((await project.get('index.html')).body);
    expect(page).toContain(`import { marked } from 'https://esm.sh/marked';`);
    expect(page).toContain('<p>draft</p>');
  });

  it('leaves bare imports to a page that brings its own import map', async () => {
    const tree: Tree = {
      site: {
        'index.html': {
          text: `<script type="importmap">{"imports":{"preact":"/vendor/preact.js"}}</script><script type="module" src="main.js"></script>`,
        },
        'main.js': { text: `import { h } from 'preact';` },
      },
    };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    await project.get('index.html');
    expect(text((await project.get('main.js')).body)).toBe(`import { h } from 'preact';`);
  });

  it('says it changed when a served file has a new version, or a missing one appears', async () => {
    const tree: Tree = { site: { 'index.html': { text: 'a' } } };
    const files = folders(tree);
    const project = new SiteProject(files, 'site', new Map(), parse);
    await project.get('index.html');
    await project.get('data.json');
    expect(await project.changed()).toBe(false);
    expect(files.list).toHaveBeenLastCalledWith('site', true);

    tree['site']['index.html'] = { text: 'b', version: 2 };
    expect(await project.changed()).toBe(true);
    await project.get('index.html');
    expect(await project.changed()).toBe(false);

    tree['site']['data.json'] = { text: '[]' };
    expect(await project.changed()).toBe(true);
  });

  it('does not count the page being edited as changed elsewhere', async () => {
    const tree: Tree = { site: { 'index.html': { text: 'stored' } } };
    const unsaved = new Map([['index.html', 'typed']]);
    const project = new SiteProject(folders(tree), 'site', unsaved, parse);
    expect(text((await project.get('index.html')).body)).toBe('typed');
    tree['site']['index.html'] = { text: 'saved', version: 2 };
    expect(await project.changed()).toBe(false);
  });

  it('finds a conversation’s files at their workspace paths before the gallery’s, and sees them change', async () => {
    const tree: Tree = {
      '': { 'styles.css': { text: 'gallery copy' } },
      css: { 'app.css': { text: 'gallery app.css' } },
    };
    let conversation = new Map<string, FileAttachment>([
      ['styles.css', { id: 'chat-styles', name: 'styles.css', folder: '', content_version: 1 } as FileAttachment],
    ]);
    const files = folders(tree);
    files.download.mockImplementation(
      async (f: FileAttachment) => new Blob([f.id === 'chat-styles' ? 'chat copy' : tree[f.folder ?? '']![f.name]!.text]),
    );
    const listed: boolean[] = [];
    const project = new SiteProject(files, '', new Map(), parse, async fresh => (listed.push(fresh), conversation));
    expect(text((await project.get('styles.css')).body)).toBe('chat copy');
    // Not in the conversation: the gallery folder's.
    expect(text((await project.get('css/app.css')).body)).toBe('gallery app.css');
    expect(listed).toEqual([false, false]);

    expect(await project.changed()).toBe(false);
    conversation = new Map([['styles.css', { id: 'chat-styles', name: 'styles.css', folder: '', content_version: 2 } as FileAttachment]]);
    expect(await project.changed()).toBe(true);
    // One fresh listing of the conversation per check, however many paths it serves.
    expect(listed.slice(2)).toEqual([true, true]);
  });
});
