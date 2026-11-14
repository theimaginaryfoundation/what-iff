import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { SiteProject } from './site-project';

// esbuild's WebAssembly does not load under jsdom: its transform is stood in for; sass runs.
const esbuild = vi.hoisted(() => ({ initialize: vi.fn(async () => undefined), transform: vi.fn() }));
vi.mock('esbuild-wasm', () => esbuild);

type Tree = Record<string, Record<string, string>>;

function folders(tree: Tree, versions: Record<string, number> = {}) {
  const file = (folder: string, name: string) =>
    ({ id: `${folder}/${name}`, name, folder, file_type: '', content_version: versions[`${folder}/${name}`] ?? 1 }) as FileAttachment;
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

  it('compiles a stylesheet again when a partial it uses changed, and counts the partial as served', async () => {
    const tree: Tree = { site: { 'app.scss': "@use 'vars';\nh1 { color: vars.$c; }", '_vars.scss': '$c: red;' } };
    const versions: Record<string, number> = {};
    const project = new SiteProject(folders(tree, versions), 'site', new Map(), parse);
    expect(text((await project.get('app.scss', 'style')).body)).toContain('color: red');
    expect([...project.servedFiles().keys()]).toContain('_vars.scss');
    expect(await project.changed()).toBe(false);
    tree['site']!['_vars.scss'] = '$c: blue;';
    versions['site/_vars.scss'] = 2;
    expect(await project.changed()).toBe(true);
    expect(text((await project.get('app.scss', 'style')).body)).toContain('color: blue');
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

  it('serves the JSX runtime that gives elements their source range, and compiles JSX against it', async () => {
    esbuild.transform.mockReset().mockResolvedValue({
      code: 'import { jsxDEV } from "react/jsx-dev-runtime";\nexport default () => jsxDEV("b", {}, void 0, false, {\n  fileName: "c/B.tsx",\n  lineNumber: 1,\n  columnNumber: 22\n}, this);\n',
    });
    const tree: Tree = {
      site: { 'package.json': '{"dependencies": {"react": "18.3.1"}}' },
      'site/c': { 'B.tsx': 'export default () => <b>x</b>;' },
    };
    const project = new SiteProject(folders(tree), 'site', new Map(), parse);
    const out = text((await project.get('c/B.tsx', 'script')).body);
    expect(esbuild.transform).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ jsxDev: true }));
    expect(out).toContain('from "../__whatiff/jsx-dev-runtime.js"');
    expect(out).toContain('"site/c/B.tsx:21-24"');
    // From the root, relative too: a bare name would be taken for a package.
    tree['site']!['A.tsx'] = 'export default () => <b>x</b>;';
    expect(text((await project.get('A.tsx', 'script')).body)).toContain('from "./__whatiff/jsx-dev-runtime.js"');
    const runtime = text((await project.get('__whatiff/jsx-dev-runtime.js', 'script')).body);
    expect(runtime).toContain('https://esm.sh/react@18.3.1/jsx-runtime');
    expect(runtime).toContain("'data-wi-src'");
  });

  it('maps a call site in a compiled module to the line the user wrote', async () => {
    esbuild.transform.mockReset().mockResolvedValue({ code: COMPILED_LIST });
    const project = new SiteProject(folders({ site: { 'list.ts': 'unused' } }), 'site', new Map(), parse);
    expect(await project.sourceLine('list.ts', 2, 14)).toBeNull(); // not served yet
    await project.get('list.ts', 'script');
    expect(await project.sourceLine('list.ts', 2, 14)).toBe(4);
    expect(await project.sourceLine('app.js', 7, 1)).toBe(7);
  });
});

// esbuild's output for a list.ts whose createElement call is on its line 4 (line 2 compiled).
const COMPILED_LIST =
  'function add(list, item) {\n  const li = document.createElement("li");\n  li.textContent = item.name;\n  list.append(li);\n}\nexport {\n  add\n};\n//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsibGlzdC50cyJdLAogICJzb3VyY2VzQ29udGVudCI6IFsidHlwZSBJdGVtID0geyBuYW1lOiBzdHJpbmcgfTtcblxuZXhwb3J0IGZ1bmN0aW9uIGFkZChsaXN0OiBIVE1MRWxlbWVudCwgaXRlbTogSXRlbSk6IHZvaWQge1xuICBjb25zdCBsaTogSFRNTExJRWxlbWVudCA9IGRvY3VtZW50LmNyZWF0ZUVsZW1lbnQoJ2xpJyk7XG4gIGxpLnRleHRDb250ZW50ID0gaXRlbS5uYW1lO1xuICBsaXN0LmFwcGVuZChsaSk7XG59XG4iXSwKICAibWFwcGluZ3MiOiAiQUFFTyxTQUFTLElBQUksTUFBbUIsTUFBa0I7QUFDdkQsUUFBTSxLQUFvQixTQUFTLGNBQWMsSUFBSTtBQUNyRCxLQUFHLGNBQWMsS0FBSztBQUN0QixPQUFLLE9BQU8sRUFBRTtBQUNoQjsiLAogICJuYW1lcyI6IFtdCn0K\n';
