import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { MAX_SERVED_BYTES, SiteProject } from './site-project';

type Tree = Record<string, Record<string, { text: string; version?: number; type?: string; size?: number }>>;

/** A FolderFiles over an in-memory tree of folders, whose files can be changed between calls. */
function folders(tree: Tree) {
  const file = (folder: string, name: string) => {
    const entry = tree[folder]?.[name];
    return entry
      ? ({
          id: `${folder}/${name}`,
          name,
          folder,
          file_type: entry.type ?? '',
          content_version: entry.version ?? 1,
          size_bytes: entry.size,
        } as FileAttachment)
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
    const unsaved = new Map([
      ['index.html', { text: `<script type="module">import { marked } from 'marked';</script><p>draft</p>`, file: 'page-id' }],
    ]);
    const project = new SiteProject(folders(tree), 'site', unsaved, parse);
    expect(text((await project.get('main.js')).body)).toBe(`import { h } from 'https://esm.sh/preact@10.19.0'; import './util.js';`);
    const page = text((await project.get('index.html')).body);
    expect(page).toContain(`import { marked } from 'https://esm.sh/marked';`);
    // Its elements carry their source ranges in the unsaved text, and our bridge comes first.
    expect(page).toMatch(/<p data-wi-src="page-id:\d+-\d+">draft<\/p>/);
    expect(page.indexOf('__whatiffBridge')).toBeLessThan(page.indexOf('marked'));
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
    await project.get('index.html', 'iframe');
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
    const unsaved = new Map([['index.html', { text: 'typed', file: 'page-id' }]]);
    const project = new SiteProject(folders(tree), 'site', unsaved, parse);
    expect(text((await project.get('index.html')).body)).toContain('typed');
    tree['site']['index.html'] = { text: 'saved', version: 2 };
    expect(await project.changed()).toBe(false);
  });

  it('finds a conversation’s files at their workspace paths before the gallery’s, and sees them change', async () => {
    const tree: Tree = {
      site: { 'styles.css': { text: 'gallery copy' } },
      'site/css': { 'app.css': { text: 'gallery app.css' } },
    };
    let conversation = new Map<string, FileAttachment>([
      ['site/styles.css', { id: 'chat-styles', name: 'styles.css', folder: 'site', content_version: 1 } as FileAttachment],
    ]);
    const files = folders(tree);
    files.download.mockImplementation(
      async (f: FileAttachment) => new Blob([f.id === 'chat-styles' ? 'chat copy' : tree[f.folder ?? '']![f.name]!.text]),
    );
    const listed: boolean[] = [];
    const project = new SiteProject(files, 'site', new Map(), parse, async fresh => (listed.push(fresh), conversation));
    expect(text((await project.get('styles.css')).body)).toBe('chat copy');
    // Not in the conversation: the gallery folder's.
    expect(text((await project.get('css/app.css')).body)).toBe('gallery app.css');
    expect(listed).toEqual([false, false]);

    expect(await project.changed()).toBe(false);
    conversation = new Map([
      ['site/styles.css', { id: 'chat-styles', name: 'styles.css', folder: 'site', content_version: 2 } as FileAttachment],
    ]);
    expect(await project.changed()).toBe(true);
    // One fresh listing of the conversation per check, however many paths it serves.
    expect(listed.slice(2)).toEqual([true, true]);
  });

  it('serves a page at the top level with the top level’s own files only, and says why', async () => {
    const tree: Tree = { '': { 'index.html': { text: '<p>' }, 'a.css': { text: 'a{}' } }, private: { 'notes.md': { text: 'secret' } } };
    const files = folders(tree);
    const project = new SiteProject(files, '', new Map(), parse);
    expect(text((await project.get('a.css')).body)).toBe('a{}');
    const refused = await project.get('private/notes.md');
    expect(refused).toMatchObject({ status: 404 });
    expect(refused.message).toContain('Put the project in a folder');
    expect(files.list).not.toHaveBeenCalledWith('private', expect.anything());
  });

  it('answers from the gallery when the conversation’s files cannot be listed', async () => {
    const project = new SiteProject(folders({ site: { 'a.css': { text: 'a{}' } } }), 'site', new Map(), parse, async () => {
      throw new Error('500');
    });
    expect(text((await project.get('a.css')).body)).toBe('a{}');
  });

  it('refuses a file larger than the preview serves, before downloading it', async () => {
    const files = folders({ site: { 'movie.mp4': { text: '', size: MAX_SERVED_BYTES + 1 } } });
    expect(await new SiteProject(files, 'site', new Map(), parse).get('movie.mp4')).toMatchObject({ status: 413 });
    expect(files.download).not.toHaveBeenCalled();
  });

  it('remembers what an import could have meant, so adding it reloads the page; other requests probe nothing', async () => {
    const tree: Tree = { site: { 'index.html': { text: '<p>' } } };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    expect(await project.get('App', 'script')).toMatchObject({ status: 404 });
    tree['site']!['App.tsx'] = { text: 'export {}' };
    expect(await project.changed()).toBe(true);
    // A link or an image without an extension is not a module import.
    expect(await project.get('about', 'document')).toMatchObject({ status: 404 });
  });

  it('starts its record again for each page loaded, so a page’s picture names only what it loaded', async () => {
    const tree: Tree = { site: { 'a.html': { text: '<p>' }, 'b.html': { text: '<p>' }, 'a.css': { text: '' } } };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    await project.get('a.html', 'iframe');
    await project.get('a.css', 'style');
    expect([...project.servedFiles().keys()]).toEqual(['a.html', 'a.css']);
    await project.get('b.html', 'iframe');
    expect([...project.servedFiles().keys()]).toEqual(['b.html']);
  });

  it('reads package.json from the conversation first, and reloads when it changes', async () => {
    const tree: Tree = {
      site: { 'package.json': { text: '{"dependencies": {"marked": "1.0.0"}}' }, 'a.js': { text: 'import "marked";' } },
    };
    const files = folders(tree);
    let version = 1;
    const pkg = () => ({ id: 'chat-pkg', name: 'package.json', folder: 'site', content_version: version }) as FileAttachment;
    files.download.mockImplementation(
      async (f: FileAttachment) =>
        new Blob([f.id === 'chat-pkg' ? '{"dependencies": {"marked": "9.9.9"}}' : tree[f.folder ?? '']![f.name]!.text]),
    );
    const project = new SiteProject(files, 'site', new Map(), parse, async () => new Map([['site/package.json', pkg()]]));
    expect(text((await project.get('a.js', 'script')).body)).toContain('https://esm.sh/marked@9.9.9');
    version = 2;
    expect(await project.changed()).toBe(true);
  });

  it('decides on an import map per page, by its parsed scripts', async () => {
    const tree: Tree = {
      site: {
        'map.html': { text: '<!-- <script type="importmap"> --><script type="importmap">{"imports":{}}</script>' },
        'plain.html': { text: '<!-- <script type="importmap"> --><p>' },
        'a.js': { text: 'import "marked";' },
      },
    };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    await project.get('map.html', 'iframe');
    expect(text((await project.get('a.js', 'script')).body)).toBe('import "marked";');
    // A comment that looks like an import map is not one.
    await project.get('plain.html', 'iframe');
    expect(text((await project.get('a.js', 'script')).body)).toContain('esm.sh/marked');
  });
});
