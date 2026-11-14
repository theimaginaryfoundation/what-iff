import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { SiteProject } from './site-project';

// esbuild's WebAssembly does not load under jsdom: its transform is stood in for; sass runs.
const esbuild = vi.hoisted(() => ({ initialize: vi.fn(async () => undefined), transform: vi.fn() }));
vi.mock('esbuild-wasm', () => esbuild);

type Tree = Record<string, Record<string, string>>;

function folders(tree: Tree) {
  const file = (folder: string, name: string) =>
    ({ id: `${folder}/${name}`, name, folder, file_type: '', content_version: 1 }) as FileAttachment;
  const fake = {
    list: vi.fn(async (folder: string) => new Map(Object.keys(tree[folder] ?? {}).map(name => [name, file(folder, name)]))),
    download: vi.fn(async (f: FileAttachment) => new Blob([tree[f.folder ?? '']![f.name]!])),
  };
  return fake as typeof fake & FolderFiles;
}

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html');
const text = (body?: ArrayBuffer) => new TextDecoder().decode(body);

describe('SiteProject compiling (§4)', () => {
  it('compiles TSX once per version, with the project’s JSX runtime, and rewrites its bare imports', async () => {
    esbuild.transform
      .mockReset()
      .mockResolvedValue({ code: `import { jsx } from "preact/jsx-runtime";\nexport default () => jsx("h1", {});\n` });
    const tree: Tree = { site: { 'App.tsx': 'export default () => <h1/>;', 'package.json': '{"dependencies": {"preact": "^10.0.0"}}' } };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse, null, 'wasm-url');
    const first = await project.get('App.tsx', 'script');
    expect(first).toMatchObject({ status: 200, contentType: 'text/javascript; charset=utf-8' });
    expect(text(first.body)).toContain('https://esm.sh/preact@%5E10.0.0/jsx-runtime');
    expect(esbuild.initialize).toHaveBeenCalledWith({ wasmURL: 'wasm-url', worker: false });
    expect(esbuild.transform).toHaveBeenCalledWith(
      'export default () => <h1/>;',
      expect.objectContaining({ loader: 'tsx', jsx: 'automatic', jsxImportSource: 'preact', sourcefile: 'App.tsx' }),
    );
    await project.get('App.tsx', 'script');
    expect(esbuild.transform).toHaveBeenCalledTimes(1);
  });

  it('serves a script’s compile error as a script that throws it', async () => {
    esbuild.transform.mockReset().mockRejectedValue({ errors: [{ text: 'Expected ";"', location: { line: 1, column: 4 } }] });
    const project = new SiteProject(folders({ site: { 'main.ts': 'let x y' } }), 'site', new Map(), parse);
    const out = text((await project.get('main.ts', 'script')).body);
    expect(out).toContain('throw new Error(message)');
    expect(out).toContain('main.ts:1:5: Expected \\";\\"');
  });

  it('compiles SCSS as a stylesheet, and as a module when a script imports it', async () => {
    const tree: Tree = { site: { 'app.scss': "@use 'vars';\nh1 { color: vars.$c; }", '_vars.scss': '$c: red;' } };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    const sheet = await project.get('app.scss', 'style');
    expect(sheet.contentType).toBe('text/css; charset=utf-8');
    expect(text(sheet.body).replace(/\s+/g, ' ')).toContain('h1 { color: red; }');
    const mod = await project.get('app.scss', 'script');
    expect(mod.contentType).toBe('text/javascript; charset=utf-8');
    expect(text(mod.body)).toContain('document.head.appendChild(style)');
  });

  it('shows a stylesheet’s compile error at the top of the page', async () => {
    const project = new SiteProject(folders({ site: { 'app.scss': 'h1 { color: $nope; }' } }), 'site', new Map(), parse);
    expect(text((await project.get('app.scss', 'style')).body)).toMatch(/^body::before \{ content: ".*Undefined variable/);
  });

  it('turns an import without an extension into a redirect to the file a bundler would take', async () => {
    const project = new SiteProject(folders({ site: {}, 'site/components': { 'index.tsx': '' } }), 'site', new Map(), parse);
    expect(await project.get('components', 'script')).toEqual({ status: 302, location: 'components/index.tsx' });
    expect(await project.get('nothing', 'script')).toMatchObject({ status: 404 });
  });

  it('gives JSON and other assets a script imports a module', async () => {
    const project = new SiteProject(folders({ site: { 'data.json': '[1, 2]', 'logo.svg': '<svg/>' } }), 'site', new Map(), parse);
    expect(text((await project.get('data.json', 'script')).body)).toBe('export default [1, 2];\n');
    expect(text((await project.get('logo.svg', 'script')).body)).toContain('import.meta.url');
    expect(text((await project.get('logo.svg', 'image')).body)).toBe('<svg/>');
  });
});
