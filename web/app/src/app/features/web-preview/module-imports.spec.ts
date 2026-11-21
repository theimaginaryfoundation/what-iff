import { describe, expect, it } from 'vitest';

import { isBareSpecifier, packageUrl, packageVersions, rewriteBareImports, splitSpecifier } from './module-imports';

describe('bare specifiers', () => {
  it('are package names, not files, URLs or built-ins', () => {
    expect(['preact', '@scope/pkg', 'lodash/fp'].every(isBareSpecifier)).toBe(true);
    expect(['./a.js', '../a.js', '/a.js', 'https://esm.sh/x', 'data:text/javascript,1', 'node:fs', ''].some(isBareSpecifier)).toBe(false);
  });

  it('split into the package and the path inside it, and map to the CDN', () => {
    expect(splitSpecifier('@scope/pkg/sub/x.js')).toEqual({ name: '@scope/pkg', subpath: '/sub/x.js' });
    expect(splitSpecifier('preact/hooks')).toEqual({ name: 'preact', subpath: '/hooks' });
    expect(packageUrl('preact/hooks', '^10.19.0')).toBe('https://esm.sh/preact@%5E10.19.0/hooks');
    expect(packageUrl('marked')).toBe('https://esm.sh/marked');
  });
});

describe('packageVersions', () => {
  it('takes registry versions from dependencies over devDependencies, and nothing from a bad file', () => {
    const json = JSON.stringify({
      dependencies: { preact: '^10.19.0', local: 'file:../x', git: 'github:a/b' },
      devDependencies: { preact: '9.0.0', vitest: '~4.1.0' },
    });
    expect([...packageVersions(json)]).toEqual([
      ['preact', '^10.19.0'],
      ['vitest', '~4.1.0'],
    ]);
    expect(packageVersions('{not json').size).toBe(0);
    expect(packageVersions(null).size).toBe(0);
  });
});

describe('rewriteBareImports', () => {
  it('points static, re-exported and dynamic bare imports at the CDN, and leaves the rest', async () => {
    const source = [
      `import { h, render } from 'preact';`,
      `import "./side-effect.js";`,
      `export * from '@scope/pkg/sub';`,
      `const lazy = await import("marked");`,
      `const dynamic = await import(name);`,
      `console.log(import.meta.url);`,
    ].join('\n');
    const out = await rewriteBareImports(source, new Map([['preact', '10.19.0']]));
    expect(out).toBe(
      [
        `import { h, render } from 'https://esm.sh/preact@10.19.0';`,
        `import "./side-effect.js";`,
        `export * from 'https://esm.sh/@scope/pkg/sub';`,
        `const lazy = await import("https://esm.sh/marked");`,
        `const dynamic = await import(name);`,
        `console.log(import.meta.url);`,
      ].join('\n'),
    );
  });

  it('serves a script it cannot read as a module as it is', async () => {
    const classic = 'with (obj) { x = 1 } import(';
    await expect(rewriteBareImports(classic, new Map())).resolves.toBe(classic);
    await expect(rewriteBareImports('const a = 1;', new Map())).resolves.toBe('const a = 1;');
  });
});
