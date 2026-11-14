/**
 * Compiling a project's TypeScript, JSX and SCSS in the browser, as the page asks for them (ADR
 * 0x050 §4): esbuild (its WebAssembly build) for `.ts`, `.tsx`, `.mts` and `.jsx`, and sass for
 * `.scss` and `.sass`. Both are loaded the first time a page needs them, so a project of plain
 * HTML, CSS and JavaScript loads neither. Each file is compiled on its own, as a dev server does:
 * imports stay imports, which the page then asks for in turn.
 */

/** What a file compiles to, by its extension; null for a file served as it is. */
export function compiledAs(
  name: string,
): { kind: 'script'; loader: 'ts' | 'tsx' | 'jsx' } | { kind: 'style'; syntax: 'scss' | 'indented' } | null {
  const ext = name.toLowerCase().match(/\.[^.]+$/)?.[0];
  switch (ext) {
    case '.ts':
    case '.mts':
      return { kind: 'script', loader: 'ts' };
    case '.tsx':
      return { kind: 'script', loader: 'tsx' };
    case '.jsx':
      return { kind: 'script', loader: 'jsx' };
    case '.scss':
      return { kind: 'style', syntax: 'scss' };
    case '.sass':
      return { kind: 'style', syntax: 'indented' };
    default:
      return null;
  }
}

/** A compile error, saying where. */
export class CompileError extends Error {}

type Esbuild = typeof import('esbuild-wasm');
let esbuild: Promise<Esbuild> | null = null;

function loadEsbuild(wasmUrl: string): Promise<Esbuild> {
  esbuild ??= (async () => {
    const mod = await import('esbuild-wasm');
    // On the page's thread: a worker made from a blob is something the app's own policy may refuse,
    // and transforming one file is quick.
    await mod.initialize({ wasmURL: wasmUrl, worker: false });
    return mod;
  })();
  // A failed load is not kept, so the next file tries again.
  esbuild.catch(() => (esbuild = null));
  return esbuild;
}

/**
 * The script as an ES module the browser runs: types removed, JSX turned into calls to
 * `jsxImportSource`'s automatic runtime, with an inline source map naming `path`.
 */
export async function compileScript(
  source: string,
  path: string,
  loader: 'ts' | 'tsx' | 'jsx',
  options: { wasmUrl: string; jsxImportSource: string },
): Promise<string> {
  const mod = await loadEsbuild(options.wasmUrl);
  try {
    const result = await mod.transform(source, {
      loader,
      format: 'esm',
      target: 'es2022',
      jsx: 'automatic',
      jsxImportSource: options.jsxImportSource,
      sourcemap: 'inline',
      sourcefile: path,
    });
    return result.code;
  } catch (error) {
    const first = (error as { errors?: { text: string; location?: { line: number; column: number } }[] }).errors?.[0];
    const at = first?.location ? `${path}:${first.location.line}:${first.location.column + 1}` : path;
    throw new CompileError(`${at}: ${first?.text ?? String(error)}`);
  }
}

type Sass = typeof import('sass');
let sass: Promise<Sass> | null = null;

/** Project paths as sass URLs, so its imports resolve inside the project. */
const SCHEME = 'project:';
const toUrl = (path: string) => new URL(`${SCHEME}/${path}`);
const toPath = (url: URL) => decodeURIComponent(url.pathname.replace(/^\//, ''));

/**
 * The stylesheet as CSS. `@use`, `@forward` and `@import` of other project files are resolved by
 * `read` (a path's text, or null), as sass would on disk: partials (`_name.scss`), the extension
 * left out, and a folder's `index` or `_index`.
 */
export async function compileStyle(
  source: string,
  path: string,
  syntax: 'scss' | 'indented',
  read: (path: string) => Promise<string | null>,
): Promise<string> {
  sass ??= import('sass');
  sass.catch(() => (sass = null));
  const mod = await sass;
  const found = new Map<string, string>();
  const find = async (path: string): Promise<string | null> => {
    if (found.has(path)) return path;
    const text = await read(path);
    if (text === null) return null;
    found.set(path, text);
    return path;
  };
  try {
    const result = await mod.compileStringAsync(source, {
      syntax,
      url: toUrl(path),
      // Its warnings (deprecations, mostly) would land in the app's console, not the page's.
      logger: mod.Logger.silent,
      importers: [
        {
          async canonicalize(url, context) {
            const base = context.containingUrl?.protocol === SCHEME ? context.containingUrl : toUrl(path);
            const resolved = new URL(url, base);
            // sass's own modules (sass:math) and URLs elsewhere are not the project's.
            if (resolved.protocol !== SCHEME) return null;
            const wanted = toPath(resolved);
            for (const candidate of candidates(wanted)) {
              if (await find(candidate)) return toUrl(candidate);
            }
            return null;
          },
          load(canonicalUrl) {
            const p = toPath(canonicalUrl);
            const contents = found.get(p);
            if (contents === undefined) return null;
            return { contents, syntax: p.endsWith('.sass') ? 'indented' : p.endsWith('.css') ? 'css' : 'scss' };
          },
        },
      ],
    });
    return result.css;
  } catch (error) {
    throw new CompileError(String((error as { message?: string }).message ?? error));
  }
}

/** Where sass looks for an import of `wanted`: as it is, as a partial, with each extension, as a folder's index. */
function candidates(wanted: string): string[] {
  const slash = wanted.lastIndexOf('/');
  const dir = slash >= 0 ? wanted.slice(0, slash + 1) : '';
  const base = wanted.slice(slash + 1);
  if (/\.(s[ac]ss|css)$/i.test(base)) return [wanted, `${dir}_${base}`];
  const out: string[] = [];
  for (const ext of ['.scss', '.sass', '.css']) out.push(`${dir}${base}${ext}`, `${dir}_${base}${ext}`);
  for (const ext of ['.scss', '.sass', '.css']) out.push(`${wanted}/index${ext}`, `${wanted}/_index${ext}`);
  return out;
}

/** The files a module import without an extension may name, in the order a bundler tries them. */
export function moduleCandidates(path: string): string[] {
  const exts = ['.ts', '.tsx', '.js', '.jsx', '.mjs', '.mts'];
  return [...exts.map(ext => path + ext), ...exts.map(ext => `${path}/index${ext}`)];
}

/**
 * A module that stands for a non-script file a script imports (`import './app.css'`,
 * `import data from './data.json'`, `import logo from './logo.svg'`), as bundlers allow: a
 * stylesheet is added to the page, JSON is its value, anything else is its URL.
 */
export function assetModule(name: string, text: string | null, contentType: string): string {
  const lower = name.toLowerCase();
  if (/\.(css|s[ac]ss)$/.test(lower) && text !== null) {
    return `const css = ${JSON.stringify(text)};\nconst style = document.createElement('style');\nstyle.setAttribute('data-wi-module', ${JSON.stringify(name)});\nstyle.textContent = css;\ndocument.head.appendChild(style);\nexport default css;\n`;
  }
  if ((lower.endsWith('.json') || contentType.includes('json')) && text !== null) {
    return `export default ${text.trim() || 'null'};\n`;
  }
  return `export default new URL(import.meta.url).href;\n`;
}
