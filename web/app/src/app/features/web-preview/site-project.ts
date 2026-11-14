import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { packageVersions, rewriteBareImports } from './module-imports';
import { servedType } from './page-assets';
import { PAGE_BRIDGE_SCRIPT } from './page-bridge';
import { withSourcePositions } from './source-positions';

/** An answer to the preview site's worker: a file, or why there is none. */
export interface SiteAnswer {
  status: number;
  contentType?: string;
  body?: ArrayBuffer;
  message?: string;
}

const MODULE_EXTENSIONS = ['.js', '.mjs'];
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
  ) {}

  /** The file at `path` in the project, ready to serve. */
  async get(path: string): Promise<SiteAnswer> {
    const place = this.place(path);
    if (!place) return { status: 400, message: `${path} is not a path in this project.` };
    const unsaved = this.unsaved.get(path);
    if (unsaved !== undefined) return this.serve(path, place.name, unsaved.file, null, new TextEncoder().encode(unsaved.text));
    const file = await this.find(place, this.lookups(false));
    if (!file) {
      this.missing.set(path, place);
      this.served.delete(path);
      return { status: 404, message: `${path} is not in this project.` };
    }
    this.missing.delete(path);
    this.served.set(path, { ...place, version: versionOf(file), file });
    const bytes = new Uint8Array(await (await this.folders.download(file)).arrayBuffer());
    return this.serve(path, place.name, file.id, file, bytes);
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

  private async serve(path: string, name: string, fileId: string, file: FileAttachment | null, bytes: Uint8Array): Promise<SiteAnswer> {
    const lower = name.toLowerCase();
    let contentType = servedType(name, file?.file_type);
    if (MODULE_EXTENSIONS.some(ext => lower.endsWith(ext)) && !this.ownImportMap) {
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
