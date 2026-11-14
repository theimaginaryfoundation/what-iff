import { NgComponentOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, DestroyRef, Type, computed, effect, inject, input, signal, untracked } from '@angular/core';

import { FileAttachment } from '../app/core/models/file-attachment.model';
import { saveBlobAsFile } from '../app/core/utils/download.helpers';
import { FilePreviewKind, previewKindFor } from '../app/features/gallery/helpers/file-preview.helpers';
import { CodeEditorComponent } from '../app/features/file-workspace/editors/code-editor.component';
import { loadViewer } from '../app/features/file-workspace/viewers/registry';
import { HtmlPreviewComponent } from '../app/features/web-preview/html-preview.component';
import { ButtonComponent, ChevDownIconComponent, ChevRightIconComponent, DownloadIconComponent } from '../app/shared/ui';
import { SharedFile, SharedView } from './share-api.service';
import { ShareFolderFiles, sharedAttachment } from './share-folder-files';
import { TreeRow, buildTree, foldersOf, treeRows } from './file-tree';

/** Kinds shown as code in the Code view: the text itself, read-only, as the code editor shows it. */
const AS_CODE: ReadonlySet<FilePreviewKind> = new Set(['text', 'markdown', 'mermaid', 'html']);

const PAGE = /\.html?$/i;

/**
 * A shared web app (ADR 0x051 §7): Preview runs the page on the web apps site; Code shows the
 * project's file tree and the chosen file in its viewer, code read-only in the code editor. The
 * project is what the share API lists, which is the page's folder and nothing else.
 */
@Component({
  selector: 'share-web-app',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    NgComponentOutlet,
    ButtonComponent,
    ChevDownIconComponent,
    ChevRightIconComponent,
    DownloadIconComponent,
    CodeEditorComponent,
    HtmlPreviewComponent,
  ],
  styleUrl: './shared-view.scss',
  template: `
    <div class="shared-view">
      <header class="shared-view__bar">
        <h1 class="shared-view__name">{{ view().project_name || view().name }}</h1>
        <div class="shared-view__switch" role="tablist" aria-label="View">
          <button
            type="button"
            role="tab"
            class="shared-view__tab"
            [class.shared-view__tab--on]="mode() === 'preview'"
            [attr.aria-selected]="mode() === 'preview'"
            (click)="showPreview()"
          >
            Preview
          </button>
          <button
            type="button"
            role="tab"
            class="shared-view__tab"
            [class.shared-view__tab--on]="mode() === 'code'"
            [attr.aria-selected]="mode() === 'code'"
            (click)="mode.set('code')"
          >
            Code
          </button>
        </div>
        <span class="shared-view__by">Shared with What Iff</span>
        @if (mode() === 'code' && blob()) {
          <ui-button size="sm" (click)="download()">
            <ui-download-icon [size]="14" />
            Download
          </ui-button>
        }
      </header>

      @if (failed()) {
        <p class="shared-view__note">The project could not be loaded. Reload to try again.</p>
      } @else if (mode() === 'preview') {
        <section class="shared-view__body">
          @if (pageHtml() !== null && pageFile(); as page) {
            <app-html-preview class="shared-view__preview" [file]="page" [html]="pageHtml()!" />
          } @else {
            <p class="shared-view__note">Loading…</p>
          }
        </section>
      } @else {
        <section class="shared-view__split">
          <nav class="shared-view__tree" aria-label="Files">
            <ul role="tree" class="shared-view__rows">
              @for (row of rows(); track row.kind + row.path) {
                <li
                  role="treeitem"
                  [attr.aria-expanded]="row.kind === 'folder' ? row.open : null"
                  [attr.aria-selected]="row.path === selected()"
                >
                  <button
                    type="button"
                    class="shared-view__row"
                    [class.shared-view__row--on]="row.kind === 'file' && row.path === selected()"
                    [style.padding-left.rem]="0.5 + row.depth * 0.875"
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
            @if (!blob()) {
              <p class="shared-view__note">Loading…</p>
            } @else if (asCode()) {
              <app-code-editor class="shared-view__code" [file]="chosenFile()!" [blob]="blob()!" [readOnly]="true" [codeOnly]="true" />
            } @else if (viewer(); as viewer) {
              <ng-container *ngComponentOutlet="viewer; inputs: { file: chosenFile()!, blob: blob()! }" />
            } @else {
              <p class="shared-view__note">This kind of file has no preview. Download it to open it.</p>
            }
          </div>
        </section>
      }
    </div>
  `,
})
export class SharedWebAppComponent {
  private readonly files = inject(ShareFolderFiles);

  readonly shareKey = input.required<string>();
  readonly view = input.required<SharedView>();

  readonly mode = signal<'preview' | 'code'>('preview');
  readonly failed = signal(false);
  readonly listing = signal<SharedFile[]>([]);
  readonly open = signal<ReadonlySet<string>>(new Set());
  /** The file chosen in the tree (Code), by path. */
  readonly selected = signal<string | null>(null);
  /** The page Preview shows, by path: the share's page, or an HTML file chosen in the tree. */
  readonly page = signal<string | null>(null);
  readonly pageHtml = signal<string | null>(null);
  readonly blob = signal<Blob | null>(null);
  readonly viewer = signal<Type<unknown> | null>(null);

  readonly tree = computed(() => buildTree(this.listing()));
  readonly rows = computed(() => treeRows(this.tree(), this.open()));
  readonly chosenFile = computed<FileAttachment | null>(() => {
    const path = this.selected();
    const file = path ? this.listing().find(f => f.path === path) : null;
    return file ? sharedAttachment(file) : null;
  });
  readonly pageFile = computed<FileAttachment | null>(() => {
    const path = this.page();
    const file = path ? this.listing().find(f => f.path === path) : null;
    return file ? sharedAttachment(file) : null;
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
    // The chosen file's bytes and viewer, in Code.
    effect(() => {
      const file = this.chosenFile();
      untracked(() => void this.loadChosen(file));
    });
    // The page Preview shows: its text, which the preview serves with the project around it.
    effect(() => {
      const page = this.pageFile();
      untracked(() => void this.loadPage(page));
    });
  }

  private async start(key: string, view: SharedView): Promise<void> {
    this.files.use(key, null);
    try {
      const listing = await this.files.all();
      if (!this.alive) return;
      this.listing.set(listing);
      const page = view.page && listing.some(f => f.path === view.page) ? view.page : (listing.find(f => PAGE.test(f.path))?.path ?? null);
      this.page.set(page);
      this.selected.set(page ?? listing[0]?.path ?? null);
      // The root's folders start open one level, and the chosen file's folders are open.
      this.open.set(new Set([...this.tree().folders.map(f => f.path), ...foldersOf(page ?? '')]));
    } catch {
      if (this.alive) this.failed.set(true);
    }
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

  /** Preview shows the HTML file chosen in Code, else the page it showed. */
  showPreview(): void {
    const chosen = this.selected();
    if (chosen && PAGE.test(chosen)) this.page.set(chosen);
    this.mode.set('preview');
  }

  download(): void {
    const blob = this.blob();
    const file = this.chosenFile();
    if (blob && file) saveBlobAsFile(blob, file.name);
  }

  private async loadChosen(file: FileAttachment | null): Promise<void> {
    const generation = ++this.generation;
    this.blob.set(null);
    this.viewer.set(null);
    if (!file) return;
    const kind = previewKindFor(file);
    const load = AS_CODE.has(kind) ? null : loadViewer(kind);
    try {
      const [blob, viewer] = await Promise.all([this.files.download(file), load ?? Promise.resolve(null)]);
      if (!this.alive || generation !== this.generation) return;
      this.viewer.set(viewer);
      this.blob.set(blob);
    } catch {
      if (this.alive && generation === this.generation) this.failed.set(true);
    }
  }

  private async loadPage(page: FileAttachment | null): Promise<void> {
    this.pageHtml.set(null);
    if (!page) return;
    try {
      const text = await (await this.files.download(page)).text();
      if (this.alive && this.pageFile()?.id === page.id) this.pageHtml.set(text);
    } catch {
      if (this.alive) this.failed.set(true);
    }
  }
}
