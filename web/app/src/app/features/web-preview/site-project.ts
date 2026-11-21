import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { packageVersions, rewriteBareImports } from './module-imports';
import { servedType } from './page-assets';

/** An answer to the preview site's worker: a file, or why there is none. */
export interface SiteAnswer {
  status: number;
  contentType?: string;
  body?: ArrayBuffer;
  message?: string;
}

const MODULE_EXTENSIONS = ['.js', '.mjs'];
const HTML_EXTENSIONS = ['.html', '.htm'];

/**
 * One open project on the preview site (ADR 0x050 §2, §3): a gallery folder as the site's root,
 * whose files are found by path when the page asks for them. A path's folders are the gallery's
 * (matched without case: the gallery keeps folder names lower case), its name is the file's name.
 * Text the editor holds but has not saved is served in place of the stored file.
 *
 * It remembers what it served and what it could not find, so `changed` can tell when a reload would
 * show something new: a served file with a new version, or a missing one that now exists.
 */
export class SiteProject {
  /** What each served path was, by its folder and name and the version served. */
  private readonly served = new Map<string, { folder: string; name: string; version: string }>();
  /** Paths asked for that were not there. */
  private readonly missing = new Map<string, { folder: string; name: string }>();
  private versions: Promise<Map<string, string>> | null = null;
  /** A page of the project brought its own import map: bare imports are then the page's to resolve. */
  private ownImportMap = false;

  constructor(
    private readonly folders: FolderFiles,
    /** The gallery folder that is the site's root ("" for the top level). */
    readonly root: string,
    /** Unsaved text by path (the page being edited), served instead of the stored file. */
    private readonly unsaved: ReadonlyMap<string, string>,
    /** Parses HTML (the document's DOMParser). */
    private readonly parseHtml: (html: string) => Document,
  ) {}

  /** The file at `path` in the project, ready to serve. */
  async get(path: string): Promise<SiteAnswer> {
    const place = this.place(path);
    if (!place) return { status: 400, message: `${path} is not a path in this project.` };
    const text = this.unsaved.get(path);
    if (text !== undefined) return this.serve(path, place.name, null, new TextEncoder().encode(text));
    const file = (await this.folders.list(place.folder)).get(place.name);
    if (!file) {
      this.missing.set(path, place);
      this.served.delete(path);
      return { status: 404, message: `${path} is not in this project.` };
    }
    this.missing.delete(path);
    this.served.set(path, { ...place, version: versionOf(file) });
    const bytes = new Uint8Array(await (await this.folders.download(file)).arrayBuffer());
    return this.serve(path, place.name, file, bytes);
  }

  /** Whether anything served, or asked for and missing, is different now. */
  async changed(): Promise<boolean> {
    const folders = new Set([...this.served.values(), ...this.missing.values()].map(p => p.folder));
    const listings = new Map<string, Map<string, FileAttachment>>();
    for (const folder of folders) listings.set(folder, await this.folders.list(folder, true));
    for (const [path, was] of this.served) {
      if (this.unsaved.has(path)) continue;
      const now = listings.get(was.folder)?.get(was.name);
      if (!now || versionOf(now) !== was.version) return true;
    }
    for (const was of this.missing.values()) {
      if (listings.get(was.folder)?.has(was.name)) return true;
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

  private async serve(path: string, name: string, file: FileAttachment | null, bytes: Uint8Array): Promise<SiteAnswer> {
    const lower = name.toLowerCase();
    let contentType = servedType(name, file?.file_type);
    if (MODULE_EXTENSIONS.some(ext => lower.endsWith(ext)) && !this.ownImportMap) {
      const source = new TextDecoder().decode(bytes);
      bytes = new TextEncoder().encode(await rewriteBareImports(source, await this.packageVersions()));
    } else if (HTML_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      bytes = new TextEncoder().encode(await this.withInlineModules(new TextDecoder().decode(bytes)));
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
