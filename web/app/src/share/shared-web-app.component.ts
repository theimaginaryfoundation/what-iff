import { NgComponentOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, DestroyRef, Type, computed, effect, inject, input, signal, untracked } from '@angular/core';

import { FileAttachment } from '../app/core/models/file-attachment.model';
import { FilePreviewKind, previewKindFor } from '../app/features/gallery/helpers/file-preview.helpers';
import { CodeEditorComponent } from '../app/features/file-workspace/editors/code-editor.component';
import { loadViewer } from '../app/features/file-workspace/viewers/registry';
import { HtmlPreviewComponent } from '../app/features/web-preview/html-preview.component';
import { PREVIEW_URL } from '../app/features/web-preview/preview-site.config';
import { ChevDownIconComponent, ChevRightIconComponent, DownloadIconComponent, TabComponent, TabsComponent } from '../app/shared/ui';
import { SharedFile, SharedView, sharedFileUrl } from './share-api.service';
import { ShareFolderFiles, sharedAttachment } from './share-folder-files';
import { TreeRow, buildTree, foldersOf, treeRows } from './file-tree';

/**
 * Kinds shown as code in the Code view: the text itself, read-only, as the code editor shows it. A
 * diagram is its XML here: diagrams have no viewer on the share site (ADR 0x051 §1).
 */
const AS_CODE: ReadonlySet<FilePreviewKind> = new Set(['text', 'markdown', 'mermaid', 'html', 'diagram']);

const PAGE = /\.html?$/i;

/**
 * A shared web app (ADR 0x051 §7): Preview runs the page on the web apps site; Code shows the
 * project's file tree and the chosen file in its viewer, code read-only in the code editor. The
 * project is what the share API lists, which is the page's folder and nothing else. A file that
 * cannot be read says so in its own pane; only a listing that cannot be read stops the view.
 */
@Component({
  selector: 'share-web-app',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    NgComponentOutlet,
    ChevDownIconComponent,
    ChevRightIconComponent,
    DownloadIconComponent,
    CodeEditorComponent,
    HtmlPreviewComponent,
    TabComponent,
    TabsComponent,
  ],
  styleUrl: './shared-view.scss',
  template: `
    <main class="shared-view">
      <header class="shared-view__bar">
        <h1 class="shared-view__name">{{ view().project_name || view().name }}</h1>
        <ui-tabs class="shared-view__switch" size="sm" ariaLabel="View" [activeId]="mode()" (activeIdChange)="setMode($event)">
          <ui-tab id="preview" controls="share-panel">Preview</ui-tab>
          <ui-tab id="code" controls="share-panel">Code</ui-tab>
        </ui-tabs>
        <span class="shared-view__by">Shared with What Iff</span>
        @if (mode() === 'code' && chosenUrl(); as href) {
          <a class="shared-view__download" [href]="href" download>
            <ui-download-icon [size]="14" />
            Download
          </a>
        }
      </header>

      <section id="share-panel" class="shared-view__panel" role="tabpanel" [attr.aria-labelledby]="mode()">
        @if (listFailed()) {
          <p class="shared-view__note" role="alert">The project could not be loaded. Reload to try again.</p>
        } @else if (loading()) {
          <p class="shared-view__note" role="status">Loading…</p>
        } @else if (listing().length === 0) {
          <p class="shared-view__note">This folder has no files.</p>
        } @else if (mode() === 'preview') {
          @if (!pageFile()) {
            <p class="shared-view__note">This project has no page to show. Its files are under Code.</p>
          } @else if (pageFailed()) {
            <p class="shared-view__note" role="alert">The page could not be loaded. Reload to try again.</p>
          } @else if (pageHtml() !== undefined) {
            <app-html-preview class="shared-view__preview" [file]="pageFile()!" [html]="pageHtml()!" />
          } @else {
            <p class="shared-view__note" role="status">Loading…</p>
          }
        } @else {
          <div class="shared-view__split">
            <nav class="shared-view__tree" aria-label="Files">
              <ul class="shared-view__rows">
                @for (row of rows(); track row.kind + row.path) {
                  <li>
                    <button
                      type="button"
                      class="shared-view__row"
                      [class.shared-view__row--on]="row.kind === 'file' && row.path === selected()"
                      [style.padding-left.rem]="0.5 + row.depth * 0.875"
                      [attr.aria-expanded]="row.kind === 'folder' ? row.open : null"
                      [attr.aria-current]="row.kind === 'file' && row.path === selected() ? 'true' : null"
                      [attr.aria-label]="row.kind === 'folder' ? 'Folder ' + row.name : null"
                      (click)="choose(row)"
                    >
                      @if (row.kind === 'folder') {
                        @if (row.open) {
                          <ui-chev-down-icon [size]="12" />
                        } @else {
                          <ui-chev-right-icon [size]="12" />
                        }
                      } @else {
                        <span class="shared-view__spacer"></span>
                      }
                      <span class="shared-view__row-name">{{ row.name }}</span>
                    </button>
                  </li>
                }
              </ul>
            </nav>
            <div class="shared-view__file">
              @if (fileFailed()) {
                <p class="shared-view__note" role="alert">This file could not be loaded.</p>
              } @else if (!blob()) {
                <p class="shared-view__note" role="status">Loading…</p>
              } @else if (asCode()) {
                <app-code-editor class="shared-view__code" [file]="chosenFile()!" [blob]="blob()!" [readOnly]="true" [codeOnly]="true" />
              } @else if (viewer(); as viewer) {
                <ng-container *ngComponentOutlet="viewer; inputs: { file: chosenFile()!, blob: blob()! }" />
              } @else {
                <p class="shared-view__note">This kind of file has no preview. Download it to open it.</p>
              }
            </div>
          </div>
        }
      </section>
    </main>
  `,
})
export class SharedWebAppComponent {
  private readonly files = inject(ShareFolderFiles);
  /** Whether the page runs on the web apps site, which reads the stored page itself. */
  private readonly hasSite = !!inject(PREVIEW_URL);

  readonly shareKey = input.required<string>();
  readonly view = input.required<SharedView>();

  readonly mode = signal<'preview' | 'code'>('preview');
  readonly loading = signal(true);
  readonly listFailed = signal(false);
  readonly listing = signal<SharedFile[]>([]);
  readonly open = signal<ReadonlySet<string>>(new Set());
  /** The file chosen in the tree (Code), by path. */
  readonly selected = signal<string | null>(null);
  /** The page Preview shows, by path: the share's page, or an HTML file chosen in the tree. */
  readonly page = signal<string | null>(null);
  /**
   * What the preview is given as the page's text: null on the web apps site, which then serves the
   * stored page as it does every other file (so an edit to it shows); the text itself for a page
   * shown on its own; undefined while it is read.
   */
  readonly pageHtml = signal<string | null | undefined>(undefined);
  readonly pageFailed = signal(false);
  readonly blob = signal<Blob | null>(null);
  readonly viewer = signal<Type<unknown> | null>(null);
  readonly fileFailed = signal(false);

  readonly tree = computed(() => buildTree(this.listing()));
  readonly rows = computed(() => treeRows(this.tree(), this.open()));
  readonly chosenFile = computed<FileAttachment | null>(() => this.attachmentAt(this.selected()));
  readonly pageFile = computed<FileAttachment | null>(() => this.attachmentAt(this.page()));
  readonly chosenUrl = computed(() => {
    const path = this.selected();
    return path ? sharedFileUrl(this.shareKey(), path) : null;
  });
  readonly asCode = computed(() => {
    const file = this.chosenFile();
    return !!file && AS_CODE.has(previewKindFor(file));
  });

  private alive = true;
  private generation = 0;

  constructor() {
    inject(DestroyRef).onDestroy(() => (this.alive = false));
    effect(() => {
      const key = this.shareKey();
      const view = this.view();
      untracked(() => void this.start(key, view));
    });
    // The chosen file's bytes and viewer, in Code only.
    effect(() => {
      const file = this.mode() === 'code' ? this.chosenFile() : null;
      untracked(() => void this.loadChosen(file));
    });
    // The page Preview shows.
    effect(() => {
      const page = this.mode() === 'preview' ? this.pageFile() : null;
      untracked(() => void this.loadPage(page));
    });
  }

  setMode(id: string): void {
    if (id === 'preview') {
      // Preview shows the HTML file chosen in Code, else the page it showed.
      const chosen = this.selected();
      if (chosen && PAGE.test(chosen)) this.page.set(chosen);
    }
    this.mode.set(id === 'code' ? 'code' : 'preview');
  }

  choose(row: TreeRow): void {
    if (row.kind === 'folder') {
      this.open.update(open => {
        const next = new Set(open);
        if (next.has(row.path)) next.delete(row.path);
        else next.add(row.path);
        return next;
      });
      return;
    }
    this.selected.set(row.path);
  }

  private attachmentAt(path: string | null): FileAttachment | null {
    const file = path ? this.listing().find(f => f.path === path) : null;
    return file ? sharedAttachment(file) : null;
  }

  private async start(key: string, view: SharedView): Promise<void> {
    this.files.use(key, null);
    this.loading.set(true);
    this.listFailed.set(false);
    try {
      const listing = await this.files.all();
      if (!this.alive) return;
      this.listing.set(listing);
      const page = view.page && listing.some(f => f.path === view.page) ? view.page : (listing.find(f => PAGE.test(f.path))?.path ?? null);
      this.page.set(page);
      this.selected.set(page ?? listing[0]?.path ?? null);
      // The root's folders start open one level, and the chosen file's folders are open.
      this.open.set(new Set([...this.tree().folders.map(f => f.path), ...foldersOf(this.selected() ?? '')]));
      // Nothing to preview: the files are what there is to see.
      if (!page) this.mode.set('code');
    } catch {
      if (this.alive) this.listFailed.set(true);
    } finally {
      if (this.alive) this.loading.set(false);
    }
  }

  private async loadChosen(file: FileAttachment | null): Promise<void> {
    const generation = ++this.generation;
    this.blob.set(null);
    this.viewer.set(null);
    this.fileFailed.set(false);
    if (!file) return;
    const kind = previewKindFor(file);
    const load = AS_CODE.has(kind) ? null : loadViewer(kind);
    try {
      const [blob, viewer] = await Promise.all([this.files.download(file), load ?? Promise.resolve(null)]);
      if (!this.alive || generation !== this.generation) return;
      this.viewer.set(viewer);
      this.blob.set(blob);
    } catch {
      if (this.alive && generation === this.generation) this.fileFailed.set(true);
    }
  }

  private async loadPage(page: FileAttachment | null): Promise<void> {
    this.pageHtml.set(undefined);
    this.pageFailed.set(false);
    if (!page) return;
    if (this.hasSite) {
      this.pageHtml.set(null);
      return;
    }
    try {
      const text = await (await this.files.download(page)).text();
      if (this.alive && this.pageFile()?.id === page.id) this.pageHtml.set(text);
    } catch {
      if (this.alive && this.pageFile()?.id === page.id) this.pageFailed.set(true);
    }
  }
}
