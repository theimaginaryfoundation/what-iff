import { FileAttachment } from '../../core/models/file-attachment.model';
import { CompileError, assetModule, compiledAs, importCandidates, jsxDevRuntime } from './compile';
import { factoryAt, factoryModule, rewriteFactoryImports } from './factory-sites';
import { Compiler, compilerFor } from './compiler';
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
const HTML_EXTENSIONS = ['.html', '.htm'];

/**
 * Limits on what a page can make the app do (a page is code the user or an agent wrote, and may be
 * careless or hostile): how big a file is served, how many compiled files are kept, how many paths
 * that were not there are remembered (each is looked for again on every check for changes), how many
 * gallery folders one project looks in (each is a listing of the API), and how many files are read
 * at once.
 */
export const MAX_SERVED_BYTES = 50 * 1024 * 1024;
/** The largest package.json read for versions (ADR 0x050 §4). */
const MAX_PACKAGE_JSON_BYTES = 1024 * 1024;
const MAX_COMPILED = 300;
const MAX_MISSING = 500;
const MAX_FOLDERS = 200;
const MAX_IN_FLIGHT = 8;

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
 * A page at the top level of the gallery is not given the whole gallery as its project: only the
 * top level's own files (and, in a conversation, the conversation's). A page can send what it reads
 * anywhere, so its project is no more than the folder it is in.
 *
 * It remembers what the page now shown loaded and what it could not find, so `changed` can tell
 * when a reload would show something new: a served file with a new version, or a missing one that
 * now exists. Loading a page (a document) starts that record again.
 */
export class SiteProject {
  /** What each served path was, by its folder and name and the version served. */
  private readonly served = new Map<string, { folder: string; name: string; version: string; file: FileAttachment }>();
  /** Paths asked for that were not there. */
  private readonly missing = new Map<string, { folder: string; name: string }>();
  /** The gallery folders this project has looked in (MAX_FOLDERS). */
  private readonly folderNames = new Set<string>();
  private versions: Promise<Map<string, string>> | null = null;
  /** The page shown brought its own import map: bare imports are then the page's to resolve. */
  private ownImportMap = false;
  /** Compiled files by file version and how they were asked for, least recently used first. */
  private readonly compiled = new Map<string, Promise<Compiled>>();
  private inFlight = 0;
  private readonly waiting: (() => void)[] = [];

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
    /** What TypeScript, JSX and SCSS are compiled with (§4). */
    private readonly compiler: Compiler = compilerFor('assets/esbuild/'),
  ) {}

  /**
   * The file at `path` in the project, ready to serve. `destination` is what the page asked for it
   * as (`script` for a module import, `style`, `document` or `iframe` for a page, ...), from the request.
   */
  async get(path: string, destination = ''): Promise<SiteAnswer> {
    await this.slot();
    try {
      return await this.answer(path, destination);
    } finally {
      this.release();
    }
  }

  private async answer(path: string, destination: string): Promise<SiteAnswer> {
    if (path === JSX_RUNTIME_PATH) return this.jsxRuntime();
    const factory = factoryAt(path);
    if (factory) return textAnswer(factoryModule(factory.library, factory.specifier));
    const place = this.place(path);
    if (!place) return { status: 400, message: `${path} is not a path in this project.` };
    const isPage =
      (destination === 'document' || destination === 'iframe') && HTML_EXTENSIONS.some(ext => place.name.toLowerCase().endsWith(ext));
    if (isPage) this.startPage();
    const unsaved = this.unsaved.get(path);
    if (unsaved !== undefined) return this.serve(path, place.name, unsaved.file, null, new TextEncoder().encode(unsaved.text), destination);
    const lookups = this.lookups(false);
    const file = await this.find(place, lookups);
    if (!file) {
      // A module import that found nothing: the file a bundler would take (`./App` → `./App.tsx`,
      // `./util.js` → `./util.ts`), by redirect, so its own relative imports resolve from where it is.
      const candidates = destination === 'script' ? importCandidates(path) : [];
      for (const candidate of candidates) {
        const at = this.place(candidate);
        if (at && (await this.find(at, lookups))) return { status: 302, location: candidate };
      }
      // Each is remembered, so adding any of them later reloads the page.
      for (const p of [path, ...candidates]) {
        const at = this.place(p);
        if (at) this.remember(p, at);
      }
      this.served.delete(path);
      return { status: 404, message: this.notHere(path, place) };
    }
    // Refused before it is downloaded, by the size the gallery records; the check after the download
    // covers a file stored without one.
    if ((file.size_bytes ?? 0) > MAX_SERVED_BYTES) {
      return { status: 413, message: `${path} is larger than ${MAX_SERVED_BYTES >> 20} MB, more than the preview serves.` };
    }
    this.missing.delete(path);
    this.served.set(path, { ...place, version: versionOf(file), file });
    const bytes = new Uint8Array(await (await this.folders.download(file)).arrayBuffer());
    if (bytes.byteLength > MAX_SERVED_BYTES) {
      return { status: 413, message: `${path} is larger than ${MAX_SERVED_BYTES >> 20} MB, more than the preview serves.` };
    }
    return this.serve(path, place.name, file.id, file, bytes, destination);
  }

  /** The project's files the page shown has loaded, by path (stored ones; the page being edited is not among them). */
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

  /** The gallery folder and name of a project path, or null when it is not one. */
  place(path: string): { folder: string; name: string } | null {
    const segments = path.split('/');
    const name = segments.pop() ?? '';
    if (!name || segments.some(s => !s || s === '.' || s === '..')) return null;
    const folder = [this.root, ...segments.map(s => s.toLowerCase())].filter(Boolean).join('/');
    return { folder, name };
  }

  /** A page was loaded: what it loads, its import map and its budget of folders are its own. */
  private startPage(): void {
    this.served.clear();
    this.missing.clear();
    this.folderNames.clear();
    this.versions = null;
    this.ownImportMap = false;
  }

  /**
   * The file at a place: the conversation's at that path first, then the gallery folder's. A page at
   * the gallery's top level is given the top level's own files only, never the folders below it.
   */
  private async find(place: { folder: string; name: string }, lookups: Lookups): Promise<FileAttachment | undefined> {
    const inConversation = (await lookups.conversation())?.get(place.folder ? `${place.folder}/${place.name}` : place.name);
    if (inConversation) return inConversation;
    if (!this.root && place.folder) return undefined;
    if (!this.folderNames.has(place.folder)) {
      if (this.folderNames.size >= MAX_FOLDERS) return undefined;
      this.folderNames.add(place.folder);
    }
    return (await lookups.folder(place.folder)).get(place.name);
  }

  private notHere(path: string, place: { folder: string; name: string }): string {
    if (!this.root && place.folder) {
      return `${path} is not served: a page at the top level of your files is shown with the top level's own files only. Put the project in a folder to give it folders of its own.`;
    }
    return `${path} is not in this project.`;
  }

  private remember(path: string, place: { folder: string; name: string }): void {
    if (this.missing.has(path) || this.missing.size < MAX_MISSING) this.missing.set(path, place);
  }

  /** Fresh listings (to see changes) or recent ones (to serve), each read once per lookup. */
  private lookups(fresh: boolean): Lookups {
    return new Lookups(folder => this.folders.list(folder, fresh), this.conversation ? () => this.conversation!(fresh) : null);
  }

  private async slot(): Promise<void> {
    if (this.inFlight < MAX_IN_FLIGHT) {
      this.inFlight++;
      return;
    }
    await new Promise<void>(resolve => this.waiting.push(resolve));
  }

  private release(): void {
    const next = this.waiting.shift();
    if (next) next();
    else this.inFlight--;
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
    } else if (MODULE_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      const source = new TextDecoder().decode(bytes);
      const resolved = this.ownImportMap ? source : await rewriteBareImports(source, await this.packageVersions());
      bytes = new TextEncoder().encode(await rewriteFactoryImports(resolved, path));
    } else if (HTML_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      // Each element's source range and our bridge script first (ADR 0x050 §5), then the modules.
      const page = withSourcePositions(new TextDecoder().decode(bytes), fileId, PAGE_BRIDGE_SCRIPT);
      bytes = new TextEncoder().encode(await this.withInlineModules(page, destination));
    }
    if (/^text\/|javascript|json|svg/.test(contentType) && !/charset=/i.test(contentType)) contentType += '; charset=utf-8';
    // The bytes as they are when they fill their buffer; a copy only of a view into a larger one.
    const body =
      bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength ? (bytes.buffer as ArrayBuffer) : bytes.slice().buffer;
    return { status: 200, contentType, body };
  }

  /**
   * A TypeScript, JSX or SCSS file compiled (§4), once per version (and per import map: bare imports
   * are rewritten or not). A compile error is served so it shows: a script that throws it, a
   * stylesheet that puts it at the top of the page. A stylesheet whose imports changed, or one of
   * which was missing and is there now, is compiled again, though its own file did not change.
   */
  private async compiledOnce(path: string, file: FileAttachment | null, bytes: Uint8Array, destination: string): Promise<Compiled> {
    const key = `${file ? versionOf(file) : path}:${destination === 'script' ? 'module' : 'file'}:${this.ownImportMap ? 'map' : 'cdn'}`;
    let entry = this.compiled.get(key);
    if (entry) {
      const done = await entry.catch(() => null);
      // Another request may have replaced it meanwhile: that one is the newer.
      const now = this.compiled.get(key);
      if (now && now !== entry) entry = now;
      else if (await this.importsChanged(done)) entry = undefined;
    }
    if (!entry) {
      const fresh = this.compile(path, new TextDecoder().decode(bytes), file?.id ?? null);
      fresh.catch(() => this.compiled.get(key) === fresh && this.compiled.delete(key));
      entry = fresh;
    }
    // Most recently used last.
    this.compiled.delete(key);
    this.compiled.set(key, entry);
    for (const oldest of this.compiled.keys()) {
      if (this.compiled.size <= MAX_COMPILED) break;
      this.compiled.delete(oldest);
    }
    return entry;
  }

  /** Whether a file a compiled stylesheet imported has another version now, is gone, or (one it looked for) is there now. */
  private async importsChanged(compiled: Compiled | null): Promise<boolean> {
    for (const [path, version] of compiled?.imports ?? []) {
      const place = this.place(path);
      const now = place ? await this.find(place, this.lookups(false)) : undefined;
      if ((now ? versionOf(now) : ABSENT) !== version) return true;
    }
    return false;
  }

  private async compile(path: string, source: string, fileId: string | null): Promise<Compiled> {
    const how = compiledAs(path)!;
    // The project files a stylesheet imported or looked for, at the versions read (ABSENT: not there).
    const imports = new Map<string, string>();
    try {
      if (how.kind === 'script') {
        const versions = await this.packageVersions();
        const jsxImportSource = jsxImportSourceOf(versions);
        // JSX elements carry their source range (§5), through a runtime the project serves itself,
        // found from where this module is.
        const runtime = ('../'.repeat(path.split('/').length - 1) || './') + JSX_RUNTIME_PATH;
        const code = await this.compiler.script(source, path, how.loader, {
          jsxImportSource,
          ...(fileId ? { sourceRanges: { fileId, runtime } } : {}),
        });
        const resolved = this.ownImportMap ? code : await rewriteBareImports(code, versions);
        return { text: await rewriteFactoryImports(resolved, path), contentType: 'text/javascript; charset=utf-8' };
      }
      const css = await this.compiler.style(source, path, how.syntax, async p => {
        const text = await this.rawText(p);
        imports.set(p, text === null ? ABSENT : (this.served.get(p)?.version ?? ABSENT));
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
      return {
        text: `body::before { content: "${cssString(message)}"; display: block; white-space: pre-wrap; font: 12px/1.4 monospace; color: #b00020; background: #fff0f0; padding: 8px; border-bottom: 1px solid #b00020; }\n`,
        contentType: 'text/css; charset=utf-8',
        // Fixing the partial the error is in, or adding the one it could not find, compiles it again.
        imports,
      };
    }
  }

  /** The JSX dev runtime the project's compiled JSX imports (§5): the library's runtime, giving elements their source range. */
  private async jsxRuntime(): Promise<SiteAnswer> {
    const versions = await this.packageVersions();
    const source = jsxDevRuntime(jsxImportSourceOf(versions));
    const text = this.ownImportMap ? source : await rewriteBareImports(source, versions);
    return textAnswer(text);
  }

  /**
   * The line of the file the user wrote that a place in a served module is (1-based): through the
   * compiled module's source map for TypeScript and JSX (§4), as it is for anything else; null for a
   * compiled module whose map does not say.
   */
  async sourceLine(path: string, line: number, column: number): Promise<number | null> {
    if (compiledAs(path)?.kind !== 'script') return line;
    const file = this.served.get(path)?.file;
    const out = file ? this.compiled.get(`${versionOf(file)}:module:${this.ownImportMap ? 'map' : 'cdn'}`) : undefined;
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
   * counts as served, so a change to it reloads the page and dates its picture (§6, §8); one looked
   * for and not found counts as missing, so adding it does too.
   */
  private async rawText(path: string): Promise<string | null> {
    const place = this.place(path);
    if (!place) return null;
    const file = await this.find(place, this.lookups(false));
    if (!file || (file.size_bytes ?? 0) > MAX_SERVED_BYTES) {
      this.remember(path, place);
      return null;
    }
    this.served.set(path, { ...place, version: versionOf(file), file });
    return (await this.folders.download(file)).text();
  }

  /**
   * The page with its inline module scripts' bare imports rewritten, unless it brings its own import
   * map; a page that asks for TypeScript or JSX starts the compiler loading now.
   */
  private async withInlineModules(html: string, destination: string): Promise<string> {
    const doc = this.parseHtml(html);
    const isPage = destination === 'document' || destination === 'iframe';
    if (isPage) {
      this.ownImportMap = !!doc.querySelector('script[type="importmap" i]');
      if ([...doc.querySelectorAll('script[src]')].some(s => /\.(m?ts|tsx|jsx)(\?|#|$)/i.test(s.getAttribute('src') ?? '')))
        this.compiler.warmUp();
    }
    if (this.ownImportMap) return html;
    const modules = Array.from(doc.querySelectorAll('script[type="module" i]:not([src])'));
    if (!modules.length) return html;
    let changed = false;
    const versions = await this.packageVersions();
    for (const script of modules) {
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

  /**
   * The versions the project's `package.json` asks for (at its root, the conversation's first, as
   * any file), read once per page shown; it counts as served, so a change to it reloads the page.
   */
  private packageVersions(): Promise<Map<string, string>> {
    this.versions ??= (async () => {
      const place = this.place('package.json')!;
      const file = await this.find(place, this.lookups(false));
      if (!file) {
        this.remember('package.json', place);
        return packageVersions(null);
      }
      this.served.set('package.json', { ...place, version: versionOf(file), file });
      // A package.json is a few KB; one the size of a project's limit is not one to read.
      if ((file.size_bytes ?? 0) > MAX_PACKAGE_JSON_BYTES) return packageVersions(null);
      return packageVersions(await (await this.folders.download(file)).text());
    })().catch(() => new Map<string, string>());
    return this.versions;
  }
}

/** A compiled file, and for a stylesheet the project files it imported or looked for, by path, at the versions read. */
interface Compiled {
  text: string;
  contentType: string;
  imports?: ReadonlyMap<string, string>;
}

/** The version a stylesheet's import had when it was not there. */
const ABSENT = '';

/** Where the project's compiled JSX finds its runtime (§5); a path no project file is expected at. */
const JSX_RUNTIME_PATH = '__whatiff/jsx-dev-runtime.js';

/** A module of ours (not the project's), served as it is. */
function textAnswer(text: string): SiteAnswer {
  return { status: 200, contentType: 'text/javascript; charset=utf-8', body: new TextEncoder().encode(text).buffer };
}

/** The JSX runtime a project's JSX compiles against: Preact's when it depends on Preact and not React. */
function jsxImportSourceOf(versions: Map<string, string>): string {
  return versions.has('preact') && !versions.has('react') ? 'preact' : 'react';
}

function versionOf(file: FileAttachment): string {
  return `${file.id}@${file.content_version ?? '?'}`;
}

/** Text as the inside of a CSS string: quotes, backslashes and every control character escaped. */
export function cssString(text: string): string {
  return text
    .replace(/[\\"]/g, '\\$&')
    .replace(/\n/g, '\\A ')
    .replace(/[\u0000-\u001f\u007f]/g, c => `\\${c.charCodeAt(0).toString(16)} `);
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

  /** The conversation's files; none when they cannot be listed now, so the gallery still answers. */
  conversation(): Promise<Map<string, FileAttachment>> | null {
    if (this.conversationFiles === undefined) {
      this.conversationFiles = this.listConversation ? this.listConversation().catch(() => new Map<string, FileAttachment>()) : null;
    }
    return this.conversationFiles;
  }
}
