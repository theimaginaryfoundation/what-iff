import { describe, expect, it } from 'vitest';

import { factoryAt, factoryLibraryOf, factoryModule, factoryPath, rewriteFactoryImports } from './factory-sites';

describe('factoryLibraryOf', () => {
  it('knows a library’s main entry, bare or on a CDN, and nothing inside it', () => {
    expect(factoryLibraryOf('preact')).toBe('preact');
    expect(factoryLibraryOf('react')).toBe('react');
    expect(factoryLibraryOf('https://esm.sh/preact@10.26.4')).toBe('preact');
    expect(factoryLibraryOf('https://esm.sh/react@19.1.0?dev')).toBe('react');
    expect(factoryLibraryOf('https://cdn.jsdelivr.net/npm/preact@10/+esm')).toBe('preact');
    expect(factoryLibraryOf('https://unpkg.com/react')).toBe('react');
    expect(factoryLibraryOf('preact/hooks')).toBeNull();
    expect(factoryLibraryOf('https://esm.sh/preact@10.26.4/hooks')).toBeNull();
    expect(factoryLibraryOf('react-dom')).toBeNull();
    expect(factoryLibraryOf('https://esm.sh/react-dom@19/client')).toBeNull();
    expect(factoryLibraryOf('./preact.js')).toBeNull();
    expect(factoryLibraryOf('data:text/javascript,preact')).toBeNull();
  });
});

describe('factory paths', () => {
  it('stand for the library and specifier, and nothing else', () => {
    const path = factoryPath('react', 'https://esm.sh/react@19.1.0?dev');
    expect(path).toMatch(/^__whatiff\/factory\/react\/[A-Za-z0-9_-]+\.js$/);
    expect(factoryAt(path)).toEqual({ library: 'react', specifier: 'https://esm.sh/react@19.1.0?dev' });
    // A path naming one library with another's specifier, or none, is not a wrapper.
    expect(factoryAt(path.replace('/react/', '/preact/'))).toBeNull();
    expect(factoryAt(factoryPath('preact', 'preact/hooks'))).toBeNull();
    expect(factoryAt('__whatiff/factory/preact/!.js')).toBeNull();
  });
});

describe('rewriteFactoryImports', () => {
  it('points static imports of a library’s main entry at its wrapper, from where the module is', async () => {
    const source = `import { h, render } from 'https://esm.sh/preact@10';\nimport { useState } from 'https://esm.sh/preact@10/hooks';\nconst later = import('preact');\n`;
    const out = await rewriteFactoryImports(source, 'src/app.js');
    expect(out).toBe(
      `import { h, render } from '../${factoryPath('preact', 'https://esm.sh/preact@10')}';\n` +
        `import { useState } from 'https://esm.sh/preact@10/hooks';\nconst later = import('preact');\n`,
    );
    // No line moves.
    expect(out.split('\n')).toHaveLength(source.split('\n').length);
    expect(await rewriteFactoryImports(`import React from 'react';`, 'app.js')).toBe(
      `import React from './${factoryPath('react', 'react')}';`,
    );
  });

  it('serves a module with none, or one it cannot read, as it is', async () => {
    expect(await rewriteFactoryImports(`import { x } from './x.js';`, 'a.js')).toBe(`import { x } from './x.js';`);
    expect(await rewriteFactoryImports('let = = ;', 'a.js')).toBe('let = = ;');
  });
});

describe('factoryModule', () => {
  it('is the library with its factory wrapped', () => {
    const preact = factoryModule('preact', 'https://esm.sh/preact@10');
    expect(preact).toContain(`export * from "https://esm.sh/preact@10";`);
    expect(preact).toContain('export const h = wiWrap(wiH);');
    expect(preact).toContain('wiOptions.diffed');
    const react = factoryModule('react', 'react');
    expect(react).toContain(`export * from "react";`);
    expect(react).toContain('export const createElement = wiWrap(wiCreate);');
    expect(react).toContain('export default');
  });
});
