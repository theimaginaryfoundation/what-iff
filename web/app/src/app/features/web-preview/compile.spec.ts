import { describe, expect, it } from 'vitest';

import { CompileError, assetModule, compileStyle, compiledAs, moduleCandidates } from './compile';

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
