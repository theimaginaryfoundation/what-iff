import { init, parse } from 'es-module-lexer';

/**
 * Bare imports in a project's modules (ADR 0x050 §4): `import { h } from 'preact'` names a package,
 * which a browser cannot resolve on its own. Each one is rewritten, as the module is served, to the
 * package on esm.sh, at the version the project's `package.json` asks for when it has one.
 * Relative, absolute and URL imports are left as written. A module the lexer cannot read (a classic
 * script that is not valid as a module) is served as it is.
 */

/** Where packages are loaded from. */
export const PACKAGE_CDN = 'https://esm.sh';

/** Whether a specifier names a package rather than a file or a URL. */
export function isBareSpecifier(specifier: string): boolean {
  if (!specifier || specifier.startsWith('.') || specifier.startsWith('/')) return false;
  // A scheme (https:, data:, node:) is a URL or a built-in, never a package on the CDN.
  return !/^[a-z][a-z0-9+.-]*:/i.test(specifier);
}

/** The package a bare specifier is in, and the path inside it (`@scope/pkg/sub` is `@scope/pkg`, `/sub`). */
export function splitSpecifier(specifier: string): { name: string; subpath: string } {
  const parts = specifier.split('/');
  const take = specifier.startsWith('@') ? 2 : 1;
  return { name: parts.slice(0, take).join('/'), subpath: parts.length > take ? '/' + parts.slice(take).join('/') : '' };
}

/** The CDN URL of a bare specifier, at `version` (a version or range) when there is one. */
export function packageUrl(specifier: string, version?: string): string {
  const { name, subpath } = splitSpecifier(specifier);
  return `${PACKAGE_CDN}/${name}${version ? '@' + encodeURIComponent(version) : ''}${subpath}`;
}

/** The versions a `package.json` asks for, by package (dependencies win over devDependencies). */
export function packageVersions(packageJson: string | null): Map<string, string> {
  const versions = new Map<string, string>();
  if (!packageJson) return versions;
  try {
    const parsed = JSON.parse(packageJson) as { dependencies?: Record<string, unknown>; devDependencies?: Record<string, unknown> };
    for (const deps of [parsed.devDependencies, parsed.dependencies]) {
      for (const [name, version] of Object.entries(deps ?? {})) {
        // Only registry versions: a file:, git or workspace dependency has no CDN equivalent.
        if (typeof version === 'string' && /^[\^~<>=*\dxX.\s|-]+$/.test(version.trim())) versions.set(name, version.trim());
      }
    }
  } catch {
    // An unreadable package.json asks for nothing; the latest versions are used.
  }
  return versions;
}

let ready: Promise<void> | null = null;

/** The module with its bare imports pointing at the CDN; the source as it is when it has none or cannot be read. */
export async function rewriteBareImports(source: string, versions: ReadonlyMap<string, string>): Promise<string> {
  ready ??= init;
  await ready;
  let imports: readonly { n: string | undefined; s: number; e: number; d: number }[];
  try {
    [imports] = parse(source);
  } catch {
    return source;
  }
  let out = '';
  let at = 0;
  for (const imp of imports) {
    const specifier = imp.n;
    // import.meta (d === -2) and dynamic imports of an expression (no name) are left alone.
    if (!specifier || imp.d === -2 || !isBareSpecifier(specifier)) continue;
    const url = packageUrl(specifier, versions.get(splitSpecifier(specifier).name));
    // A static import's range is the specifier's text; a dynamic import's includes its quotes.
    const replacement = imp.d > -1 ? JSON.stringify(url) : url;
    out += source.slice(at, imp.s) + replacement;
    at = imp.e;
  }
  return at === 0 ? source : out + source.slice(at);
}
