import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { WORKSPACE_CONTEXT } from '../file-workspace/editors/file-editor';
import { FolderFiles } from './folder-files.service';
import { PageBridgeAction, PageBridgeEvent, readBridgeEvent } from './page-bridge';
import { PREVIEW_URL } from './preview-site.config';
import { SiteProject } from './site-project';

/** How long the site may take to say it is ready before the frame says it did not load. */
export const SITE_LOAD_TIMEOUT_MS = 20_000;
/** How often the project's files are checked for changes made elsewhere (an agent's edit). */
export const SITE_POLL_MS = 3_000;

/** Messages from the site's host page (web/preview/site/host.js). */
type HostMessage =
  | { type: 'get'; id: number; path: string }
  | { type: 'loaded'; path: string }
  | { type: 'failed'; message: string }
  | { type: 'bridge'; data: unknown };

/**
 * An `.html` file's page served by the preview site (ADR 0x050 §3), with its folder as the site's
 * root: the page's stylesheets, scripts, modules, images, data files and other pages are found by
 * path in that folder and the folders beneath it, as a web server would find them.
 *
 * The site runs on an origin of its own, so the frame's `allow-same-origin` gives the page that
 * origin, never the app's. Its host page asks for each file over a MessagePort; this component
 * answers from the gallery (SiteProject). The page reloads when the text being edited changes, and
 * when a file it uses changed elsewhere (checked every few seconds while the app is visible).
 */
@Component({
  selector: 'app-site-frame',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (unreachable()) {
      <p class="site-frame__notice" role="alert">The preview site didn't load from {{ siteOrigin }}.</p>
    } @else if (failure(); as message) {
      <p class="site-frame__notice" role="alert">The preview could not start: {{ message }}</p>
    }
    <iframe
      #frame
      class="site-frame__frame"
      [title]="'Page: ' + file().name"
      sandbox="allow-scripts allow-same-origin allow-forms allow-modals allow-popups allow-downloads"
      referrerpolicy="no-referrer"
      [src]="src()"
    ></iframe>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
      }
      .site-frame__frame {
        background: #fff;
        border: 0;
        display: block;
        flex: 1;
        /* A host that does not constrain the height (the gallery page) still gets a usable page. */
        min-height: clamp(360px, 75vh, 1000px);
        width: 100%;
      }
      .site-frame__notice {
        border-bottom: 1px solid var(--border);
        color: var(--color-text-secondary);
        font-size: 0.75rem;
        margin: 0;
        padding: 0.375rem 0.75rem;
      }
    `,
  ],
})
export class SiteFrameComponent {
  private readonly document = inject(DOCUMENT);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly folders = inject(FolderFiles);
  /** The conversation the workspace is open in, whose files the page finds first (ADR 0x050 §2). */
  private readonly workspace = inject(WORKSPACE_CONTEXT, { optional: true });
  private readonly site = inject(PREVIEW_URL)!;
  readonly siteOrigin = new URL(this.site).origin;
  private readonly frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');

  /** The page to open; its folder is the site's root. */
  readonly file = input.required<FileAttachment>();
  /** The page's text as it is now, an editor's unsaved text included; null serves the stored file. */
  readonly html = input<string | null>(null);
  /** What the page's bridge script reports (ADR 0x050 §5). */
  readonly pageMessage = output<PageBridgeEvent>();

  /** Each opening of a page has a key of its own, which the site's worker serves it under. */
  private readonly token = signal(newToken());
  readonly src = computed(() => {
    const url = new URL(this.site);
    url.searchParams.set('t', this.token());
    return this.sanitizer.bypassSecurityTrustResourceUrl(url.href);
  });
  readonly unreachable = signal(false);
  readonly failure = signal<string | null>(null);

  private port: MessagePort | null = null;
  private project: SiteProject | null = null;
  private readonly unsaved = new Map<string, { text: string; file: string }>();
  private loadTimer: ReturnType<typeof setTimeout> | null = null;
  private polling = false;

  constructor() {
    // A different page (or folder) is a new site: new key, new project.
    effect(() => {
      const file = this.file();
      untracked(() => this.open(file));
    });
    // New text for the page being edited reloads it.
    let first = true;
    effect(() => {
      const html = this.html();
      untracked(() => {
        const entry = this.file().name;
        if (html === null) this.unsaved.delete(entry);
        else this.unsaved.set(entry, { text: html, file: this.file().id });
        if (first) first = false;
        else this.port?.postMessage({ type: 'reload' });
      });
    });
    const onMessage = (event: MessageEvent) => this.onWindowMessage(event);
    const view = this.document.defaultView!;
    view.addEventListener('message', onMessage);
    const poll = view.setInterval(() => void this.poll(), SITE_POLL_MS);
    inject(DestroyRef).onDestroy(() => {
      view.removeEventListener('message', onMessage);
      view.clearInterval(poll);
      this.close();
    });
  }

  private open(file: FileAttachment): void {
    this.close();
    // The first page keeps the key the frame was made with; another page gets a new one.
    if (this.project) this.token.set(newToken());
    const parse = (html: string) => new (this.document.defaultView?.DOMParser ?? DOMParser)().parseFromString(html, 'text/html');
    const chatId = this.workspace?.chatId() ?? null;
    const conversation = chatId ? (fresh: boolean) => this.folders.conversation(chatId, fresh) : null;
    this.project = new SiteProject(this.folders, file.folder ?? '', this.unsaved, parse, conversation);
    this.unreachable.set(false);
    this.failure.set(null);
    this.loadTimer = setTimeout(() => this.unreachable.set(true), SITE_LOAD_TIMEOUT_MS);
  }

  private close(): void {
    if (this.loadTimer) clearTimeout(this.loadTimer);
    this.loadTimer = null;
    this.port?.close();
    this.port = null;
  }

  private onWindowMessage(event: MessageEvent): void {
    const frame = this.frame()?.nativeElement.contentWindow;
    if (event.origin !== this.siteOrigin || !frame || event.source !== frame) return;
    const data = event.data as { type?: unknown; token?: unknown } | null;
    if (data?.type !== 'whatiff-preview-ready' || data.token !== this.token() || this.port) return;
    if (this.loadTimer) clearTimeout(this.loadTimer);
    this.unreachable.set(false);
    const channel = new MessageChannel();
    this.port = channel.port1;
    this.port.onmessage = message => void this.onHostMessage(message.data as HostMessage);
    frame.postMessage({ type: 'whatiff-preview-init', entry: this.file().name }, this.siteOrigin, [channel.port2]);
  }

  private async onHostMessage(message: HostMessage): Promise<void> {
    const port = this.port;
    const project = this.project;
    if (!port || !project) return;
    if (message.type === 'get' && typeof message.path === 'string') {
      let answer;
      try {
        answer = await project.get(message.path);
      } catch {
        answer = { status: 502, message: `${message.path} could not be read.` };
      }
      if (this.port !== port) return;
      port.postMessage({ type: 'file', id: message.id, ...answer }, answer.body ? [answer.body] : []);
    } else if (message.type === 'bridge') {
      const event = readBridgeEvent(message.data);
      if (event) this.pageMessage.emit(event);
    } else if (message.type === 'failed') {
      this.failure.set(String(message.message ?? 'unknown error'));
    }
  }

  /** Tells the page's bridge script something, through the host page. */
  sendToPage(action: PageBridgeAction): void {
    this.port?.postMessage({ type: 'bridge', data: action });
  }

  /** Reloads the page when a file it uses changed elsewhere. */
  private async poll(): Promise<void> {
    const project = this.project;
    if (!this.port || !project || this.polling || this.document.visibilityState !== 'visible') return;
    this.polling = true;
    try {
      if ((await project.changed()) && this.project === project) this.port?.postMessage({ type: 'reload' });
    } catch {
      // A failed check is tried again on the next tick.
    } finally {
      this.polling = false;
    }
  }
}

/** A random key for one opening of a page: 24 URL-safe characters. */
function newToken(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(18));
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, '-')
    .replace(/\//g, '_');
}
