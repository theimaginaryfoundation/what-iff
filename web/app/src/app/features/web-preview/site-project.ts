import { FileAttachment } from '../../core/models/file-attachment.model';
import { CompileError, assetModule, compileScript, compileStyle, compiledAs, jsxDevRuntime, moduleCandidates } from './compile';
import { FolderFiles } from './folder-files.service';
import { packageVersions, rewriteBareImports } from './module-imports';
import { servedType } from './page-assets';
import { PAGE_BRIDGE_SCRIPT } from './page-bridge';
import { withSourcePositions } from './source-positions';
import { inlineSourceMap, originalLine } from './source-map';

/** An answer to the preview site's worker: a file, or why there is none. */
export interface SiteAnswer {
  status: number;
  contentType?: string;
  body?: ArrayBuffer;
  message?: string;
  /** For a redirect (302): the project path to ask for instead. */
  location?: string;
}

const MODULE_EXTENSIONS = ['.js', '.mjs'];
/** Files a script may import as themselves; anything else imported from a script is served as a module standing for it. */
const SCRIPT_EXTENSIONS = /\.(m?js|m?ts|tsx|jsx)$/i;
/** How many compiled files are kept, by file version. */
const MAX_COMPILED = 300;
const HTML_EXTENSIONS = ['.html', '.htm'];

/** A conversation's files by workspace path (ADR 0x050 §7); `fresh` lists them again now. */
export type ConversationFiles = (fresh: boolean) => Promise<Map<string, FileAttachment>>;

/**
 * One open project on the preview site (ADR 0x050 §2, §3): a gallery folder as the site's root,
 * whose files are found by path when the page asks for them. A path's folders are the gallery's
 * (matched without case: the gallery keeps folder names lower case), its name is the file's name.
 * In a conversation, its files at that path come first: the conversation's project is what its
 * agent's container holds, and a file of the same name elsewhere in the gallery does not stand in
 * for it. Text the editor holds but has not saved is served in place of the stored file.
 *
 * It remembers what it served and what it could not find, so `changed` can tell when a reload would
 * show something new: a served file with a new version, or a missing one that now exists.
 */
export class SiteProject {
  /** What each served path was, by its folder and name and the version served. */
  private readonly served = new Map<string, { folder: string; name: string; version: string; file: FileAttachment }>();
  /** Paths asked for that were not there. */
  private readonly missing = new Map<string, { folder: string; name: string }>();
  private versions: Promise<Map<string, string>> | null = null;
  /** A page of the project brought its own import map: bare imports are then the page's to resolve. */
  private ownImportMap = false;
  /** Compiled files by file version and how they were asked for, least recently used first. */
  private readonly compiled = new Map<string, Promise<Compiled>>();

  constructor(
    private readonly folders: FolderFiles,
    /** The gallery folder that is the site's root ("" for the top level). */
    readonly root: string,
    /** Unsaved text by path (the page being edited) and the file it is, served instead of the stored file. */
    private readonly unsaved: ReadonlyMap<string, { text: string; file: string }>,
    /** Parses HTML (the document's DOMParser). */
    private readonly parseHtml: (html: string) => Document,
    /** The conversation's files, when the page is open in one. */
    private readonly conversation: ConversationFiles | null = null,
    /** Where esbuild's WebAssembly is, for compiling TypeScript and JSX (§4). */
    private readonly wasmUrl = 'assets/esbuild/esbuild.wasm',
  ) {}

  /**
   * The file at `path` in the project, ready to serve. `destination` is what the page asked for it
   * as (`script` for a module import, `style`, `document`, ...), from the request.
   */
  async get(path: string, destination = ''): Promise<SiteAnswer> {
    if (path === JSX_RUNTIME_PATH) return this.jsxRuntime();
    const place = this.place(path);
    if (!place) return { status: 400, message: `${path} is not a path in this project.` };
    const unsaved = this.unsaved.get(path);
    if (unsaved !== undefined) return this.serve(path, place.name, unsaved.file, null, new TextEncoder().encode(unsaved.text), destination);
    const file = await this.find(place, this.lookups(false));
    if (!file) {
      // A module import without an extension (`./App`): the file a bundler would take, by redirect,
      // so its own relative imports resolve from where it is.
      if (destination === 'script' || !place.name.includes('.')) {
        for (const candidate of moduleCandidates(path)) {
          const at = this.place(candidate);
          if (at && (await this.find(at, this.lookups(false)))) return { status: 302, location: candidate };
        }
      }
      this.missing.set(path, place);
      this.served.delete(path);
      return { status: 404, message: `${path} is not in this project.` };
    }
    this.missing.delete(path);
    this.served.set(path, { ...place, version: versionOf(file), file });
    const bytes = new Uint8Array(await (await this.folders.download(file)).arrayBuffer());
    return this.serve(path, place.name, file.id, file, bytes, destination);
  }

  /** The project's files the page has loaded, by path (stored ones; the page being edited is not among them). */
  servedFiles(): ReadonlyMap<string, FileAttachment> {
    return new Map([...this.served].map(([path, s]) => [path, s.file]));
  }

  /** The file at a project path, as a request for it would find it. */
  async fileAt(path: string): Promise<FileAttachment | null> {
    const place = this.place(path);
    return place ? ((await this.find(place, this.lookups(false))) ?? null) : null;
  }

  /** Whether anything served, or asked for and missing, is different now. */
  async changed(): Promise<boolean> {
    // Each folder and the conversation are listed once, now, however many paths share them.
    const lookups = this.lookups(true);
    for (const [path, was] of this.served) {
      if (this.unsaved.has(path)) continue;
      const now = await this.find(was, lookups);
      if (!now || versionOf(now) !== was.version) return true;
    }
    for (const was of this.missing.values()) {
      if (await this.find(was, lookups)) return true;
    }
    return false;
  }

  /** The file at a place: the conversation's at that path first, then the gallery folder's. */
  private async find(place: { folder: string; name: string }, lookups: Lookups): Promise<FileAttachment | undefined> {
    const inConversation = (await lookups.conversation())?.get(place.folder ? `${place.folder}/${place.name}` : place.name);
    return inConversation ?? (await lookups.folder(place.folder)).get(place.name);
  }

  /** Fresh listings (to see changes) or recent ones (to serve), each read once per lookup. */
  private lookups(fresh: boolean): Lookups {
    return new Lookups(folder => this.folders.list(folder, fresh), this.conversation ? () => this.conversation!(fresh) : null);
  }

  /** The gallery folder and name of a project path, or null when it is not one. */
  place(path: string): { folder: string; name: string } | null {
    const segments = path.split('/');
    const name = segments.pop() ?? '';
    if (!name || segments.some(s => !s || s === '.' || s === '..')) return null;
    const folder = [this.root, ...segments.map(s => s.toLowerCase())].filter(Boolean).join('/');
    return { folder, name };
  }

  private async serve(
    path: string,
    name: string,
    fileId: string,
    file: FileAttachment | null,
    bytes: Uint8Array,
    destination: string,
  ): Promise<SiteAnswer> {
    const lower = name.toLowerCase();
    let contentType = servedType(name, file?.file_type);
    const compile = compiledAs(name);
    if (destination === 'script' && !SCRIPT_EXTENSIONS.test(lower)) {
      // A stylesheet, data or an asset a script imports: a module standing for it.
      const styles = compile?.kind === 'style' ? await this.compiledOnce(path, file, bytes, destination) : null;
      const text = styles ? styles.text : /\.(css|json)$/.test(lower) ? new TextDecoder().decode(bytes) : null;
      bytes = new TextEncoder().encode(assetModule(name, text, contentType));
      contentType = 'text/javascript';
    } else if (compile) {
      const out = await this.compiledOnce(path, file, bytes, destination);
      bytes = new TextEncoder().encode(out.text);
      contentType = out.contentType;
    } else if (MODULE_EXTENSIONS.some(ext => lower.endsWith(ext)) && !this.ownImportMap) {
      const source = new TextDecoder().decode(bytes);
      bytes = new TextEncoder().encode(await rewriteBareImports(source, await this.packageVersions()));
    } else if (HTML_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      // Each element's source range and our bridge script first (ADR 0x050 §5), then the modules.
      const page = withSourcePositions(new TextDecoder().decode(bytes), fileId, PAGE_BRIDGE_SCRIPT);
      bytes = new TextEncoder().encode(await this.withInlineModules(page));
    }
    if (/^text\/|javascript|json|svg/.test(contentType) && !/charset=/i.test(contentType)) contentType += '; charset=utf-8';
    return { status: 200, contentType, body: bytes.slice().buffer };
  }

  /**
   * A TypeScript, JSX or SCSS file compiled (§4), once per version. A compile error is served so it
   * shows: a script that throws it, a stylesheet that puts it at the top of the page.
   */
  private async compiledOnce(path: string, file: FileAttachment | null, bytes: Uint8Array, destination: string): Promise<Compiled> {
    const key = `${file ? versionOf(file) : path}:${destination === 'script' ? 'module' : 'file'}`;
    let out = this.compiled.get(key);
    this.compiled.delete(key);
    // A stylesheet whose imports changed is compiled again, though its own file did not.
    if (out && (await this.importsChanged(await out.catch(() => null)))) out = undefined;
    if (!out) {
      out = this.compile(path, new TextDecoder().decode(bytes), file?.id ?? null);
      out.catch(() => this.compiled.get(key) === out && this.compiled.delete(key));
    }
    this.compiled.set(key, out);
    for (const oldest of this.compiled.keys()) {
      if (this.compiled.size <= MAX_COMPILED) break;
      this.compiled.delete(oldest);
    }
    return out;
  }

  /** Whether a file a compiled stylesheet imported has another version now, or is gone. */
  private async importsChanged(compiled: Compiled | null): Promise<boolean> {
    for (const [path, version] of compiled?.imports ?? []) {
      const place = this.place(path);
      const now = place ? await this.find(place, this.lookups(false)) : undefined;
      if (!now || versionOf(now) !== version) return true;
    }
    return false;
  }

  private async compile(path: string, source: string, fileId: string | null): Promise<Compiled> {
    const how = compiledAs(path)!;
    // The project files a stylesheet imported, at the versions read.
    const imports = new Map<string, string>();
    try {
      if (how.kind === 'script') {
        const versions = await this.packageVersions();
        const jsxImportSource = jsxImportSourceOf(versions);
        // JSX elements carry their source range (§5), through a runtime the project serves itself,
        // found from where this module is.
        const runtime = ('../'.repeat(path.split('/').length - 1) || './') + JSX_RUNTIME_PATH;
        const code = await compileScript(source, path, how.loader, {
          wasmUrl: this.wasmUrl,
          jsxImportSource,
          ...(fileId ? { sourceRanges: { fileId, runtime } } : {}),
        });
        return { text: this.ownImportMap ? code : await rewriteBareImports(code, versions), contentType: 'text/javascript; charset=utf-8' };
      }
      const css = await compileStyle(source, path, how.syntax, async p => {
        const text = await this.rawText(p);
        const read = this.served.get(p);
        if (text !== null && read) imports.set(p, read.version);
        return text;
      });
      return { text: css, contentType: 'text/css; charset=utf-8', imports };
    } catch (error) {
      const message = error instanceof CompileError ? error.message : `${path}: ${String(error)}`;
      if (how.kind === 'script') {
        return {
          text: `const message = ${JSON.stringify(message)};\nconsole.error(message);\nthrow new Error(message);\n`,
          contentType: 'text/javascript; charset=utf-8',
        };
      }
      const shown = message.replace(/[\\"]/g, '\\$&').replace(/\n/g, '\\A ');
      return {
        text: `body::before { content: "${shown}"; display: block; white-space: pre-wrap; font: 12px/1.4 monospace; color: #b00020; background: #fff0f0; padding: 8px; border-bottom: 1px solid #b00020; }\n`,
        contentType: 'text/css; charset=utf-8',
        // Fixing the partial the error is in compiles it again.
        imports,
      };
    }
  }

  /** The JSX dev runtime the project's compiled JSX imports (§5): the library's runtime, giving elements their source range. */
  private async jsxRuntime(): Promise<SiteAnswer> {
    const versions = await this.packageVersions();
    const source = jsxDevRuntime(jsxImportSourceOf(versions));
    const text = this.ownImportMap ? source : await rewriteBareImports(source, versions);
    return { status: 200, contentType: 'text/javascript; charset=utf-8', body: new TextEncoder().encode(text).buffer };
  }

  /**
   * The line of the file the user wrote that a place in a served module is (1-based): through the
   * compiled module's source map for TypeScript and JSX (§4), as it is for anything else; null for a
   * compiled module whose map does not say.
   */
  async sourceLine(path: string, line: number, column: number): Promise<number | null> {
    if (compiledAs(path)?.kind !== 'script') return line;
    const file = this.served.get(path)?.file;
    const out = file ? this.compiled.get(`${versionOf(file)}:module`) : undefined;
    if (!out) return null;
    try {
      const map = inlineSourceMap((await out).text);
      return map ? originalLine(map.mappings, line, column) : null;
    } catch {
      return null;
    }
  }

  /**
   * A project file's text as it is stored, for a stylesheet's imports; null when there is none. It
   * counts as served, so a change to it reloads the page and dates its picture (§6, §8).
   */
  private async rawText(path: string): Promise<string | null> {
    const place = this.place(path);
    const file = place ? await this.find(place, this.lookups(false)) : undefined;
    if (!place || !file) return null;
    this.served.set(path, { ...place, version: versionOf(file), file });
    return (await this.folders.download(file)).text();
  }

  /** The page with its inline module scripts' bare imports rewritten, unless it brings its own import map. */
  private async withInlineModules(html: string): Promise<string> {
    if (/<script[^>]*type\s*=\s*["']?importmap/i.test(html)) {
      this.ownImportMap = true;
      return html;
    }
    if (!/<script[^>]*type\s*=\s*["']?module/i.test(html)) return html;
    const doc = this.parseHtml(html);
    let changed = false;
    const versions = await this.packageVersions();
    for (const script of Array.from(doc.querySelectorAll('script[type="module"]:not([src])'))) {
      const source = script.textContent ?? '';
      const rewritten = await rewriteBareImports(source, versions);
      if (rewritten !== source) {
        script.textContent = rewritten;
        changed = true;
      }
    }
    if (!changed) return html;
    const doctype = doc.doctype ? `<!DOCTYPE ${doc.doctype.name}>\n` : '';
    return doctype + doc.documentElement.outerHTML;
  }

  /** The versions the root's `package.json` asks for, read once. */
  private packageVersions(): Promise<Map<string, string>> {
    this.versions ??= (async () => {
      const file = (await this.folders.list(this.root)).get('package.json');
      return packageVersions(file ? await (await this.folders.download(file)).text() : null);
    })().catch(() => new Map<string, string>());
    return this.versions;
  }
}

/** A compiled file, and for a stylesheet the project files it imported, by path, at the versions read. */
interface Compiled {
  text: string;
  contentType: string;
  imports?: ReadonlyMap<string, string>;
}

/** Where the project's compiled JSX finds its runtime (§5); a path no project file is expected at. */
const JSX_RUNTIME_PATH = '__whatiff/jsx-dev-runtime.js';

/** The JSX runtime a project's JSX compiles against: Preact's when it depends on Preact and not React. */
function jsxImportSourceOf(versions: Map<string, string>): string {
  return versions.has('preact') && !versions.has('react') ? 'preact' : 'react';
}

function versionOf(file: FileAttachment): string {
  return `${file.id}@${file.content_version ?? '?'}`;
}

/** The listings one lookup (a request, or one check for changes) reads, each at most once. */
class Lookups {
  private readonly folders = new Map<string, Promise<Map<string, FileAttachment>>>();
  private conversationFiles: Promise<Map<string, FileAttachment>> | null | undefined;

  constructor(
    private readonly listFolder: (folder: string) => Promise<Map<string, FileAttachment>>,
    private readonly listConversation: (() => Promise<Map<string, FileAttachment>>) | null,
  ) {}

  folder(folder: string): Promise<Map<string, FileAttachment>> {
    let files = this.folders.get(folder);
    if (!files) {
      files = this.listFolder(folder);
      this.folders.set(folder, files);
    }
    return files;
  }

  conversation(): Promise<Map<string, FileAttachment>> | null {
    if (this.conversationFiles === undefined) this.conversationFiles = this.listConversation?.() ?? null;
    return this.conversationFiles;
  }
}
