import { describe, expect, it } from 'vitest';

import {
  CompileError,
  assetModule,
  compileStyle,
  compiledAs,
  jsxDevRuntime,
  moduleCandidates,
  openingTagEnd,
  withSourceRanges,
} from './compile';

describe('compiledAs', () => {
  it('compiles TypeScript, JSX and SCSS, and serves everything else as it is', () => {
    expect(compiledAs('main.ts')).toEqual({ kind: 'script', loader: 'ts' });
    expect(compiledAs('App.TSX')).toEqual({ kind: 'script', loader: 'tsx' });
    expect(compiledAs('a.jsx')).toEqual({ kind: 'script', loader: 'jsx' });
    expect(compiledAs('styles.scss')).toEqual({ kind: 'style', syntax: 'scss' });
    expect(compiledAs('old.sass')).toEqual({ kind: 'style', syntax: 'indented' });
    for (const name of ['app.js', 'a.mjs', 'styles.css', 'index.html', 'types.d.ts.map']) expect(compiledAs(name)).toBeNull();
  });
});

describe('moduleCandidates', () => {
  it('tries the extensions, then a folder’s index, as a bundler does', () => {
    expect(moduleCandidates('src/App').slice(0, 3)).toEqual(['src/App.ts', 'src/App.tsx', 'src/App.js']);
    expect(moduleCandidates('src/App')).toContain('src/App/index.tsx');
  });
});

describe('assetModule', () => {
  it('adds an imported stylesheet to the page, gives JSON its value and anything else its URL', () => {
    const css = assetModule('app.css', 'h1 { color: red }', 'text/css');
    expect(css).toContain('"h1 { color: red }"');
    expect(css).toContain('document.head.appendChild(style)');
    expect(assetModule('data.json', '{"a": 1}', 'application/json')).toBe('export default {"a": 1};\n');
    expect(assetModule('logo.svg', null, 'image/svg+xml')).toBe('export default new URL(import.meta.url).href;\n');
  });
});

describe('compileStyle', () => {
  it('compiles SCSS, resolving the project’s partials and indexes', async () => {
    const files: Record<string, string> = {
      'css/_vars.scss': '$brand: teal;',
      'css/mixins/_index.scss': '@mixin big { font-size: 2rem; }',
    };
    const read = async (path: string) => files[path] ?? null;
    const css = await compileStyle(
      `@use 'vars'; @use 'mixins'; @use 'sass:math';\nh1 { color: vars.$brand; width: math.div(10px, 2); @include mixins.big; }`,
      'css/app.scss',
      'scss',
      read,
    );
    expect(css.replace(/\s+/g, ' ')).toContain('h1 { color: teal; width: 5px; font-size: 2rem; }');
  });

  it('says where a stylesheet is wrong', async () => {
    await expect(compileStyle('h1 { color: $missing; }', 'css/app.scss', 'scss', async () => null)).rejects.toBeInstanceOf(CompileError);
    await expect(compileStyle('h1 { color: $missing; }', 'css/app.scss', 'scss', async () => null)).rejects.toThrow(/Undefined variable/);
  });
});

describe('JSX source ranges (§5)', () => {
  // esbuild's dev-mode output for the two elements of this source.
  const SOURCE = 'export const App = () => (\n  <main title={"a>b"}>\n    /*é😀*/ <h1 id="t">Hi</h1>\n  </main>\n);\n';
  const COMPILED = [
    'import { jsxDEV } from "react/jsx-dev-runtime";',
    'export const App = () => /* @__PURE__ */ jsxDEV("main", { title: "a>b", children: /* @__PURE__ */ jsxDEV("h1", { id: "t", children: "Hi" }, void 0, false, {',
    '  fileName: "App.tsx",',
    '  lineNumber: 3,',
    '  columnNumber: 13',
    '}, this) }, void 0, false, {',
    '  fileName: _jsxFileName,',
    '  lineNumber: 2,',
    '  columnNumber: 3',
    '}, this);',
  ].join('\n');

  it('gives each element the range of its opening tag, keeping the lines, and imports the shim', () => {
    const out = withSourceRanges(COMPILED, SOURCE, 'f-1', 'react/jsx-dev-runtime', '../__whatiff/jsx-dev-runtime.js');
    expect(out.split('\n')).toHaveLength(COMPILED.split('\n').length);
    expect(out).toContain('import { jsxDEV } from "../__whatiff/jsx-dev-runtime.js";');
    const h1 = SOURCE.indexOf('<h1');
    const main = SOURCE.indexOf('<main');
    expect(out).toContain(`"f-1:${h1}-${h1 + '<h1 id="t">'.length}"`);
    expect(out).toContain(`"f-1:${main}-${main + '<main title={"a>b"}>'.length}"`);
  });

  it('ends an opening tag at its own >, not one in an attribute', () => {
    const text = '<a b={x > 1 ? "}" : `>`} c="d>e">';
    expect(openingTagEnd(text, 0)).toBe(text.length);
    expect(openingTagEnd('<br/>rest', 0)).toBe(5);
    expect(openingTagEnd('<unclosed', 0)).toBe(9);
  });

  it('runs the library’s runtime, giving elements their range', () => {
    const shim = jsxDevRuntime('preact');
    expect(shim).toContain('from "preact/jsx-runtime"');
    expect(shim).toContain("typeof type === 'string'");
  });
});
