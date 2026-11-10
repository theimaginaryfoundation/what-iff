import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  InjectionToken,
  afterNextRender,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { FileEditor } from './file-editor';

type FilerobotCtor = typeof import('filerobot-image-editor').default;
type Filerobot = InstanceType<FilerobotCtor>;

/** Loads the Filerobot image editor (MIT) on first use; a token so tests can hand in a fake. */
export const FILEROBOT_LOADER = new InjectionToken<() => Promise<FilerobotCtor>>('FILEROBOT_LOADER', {
  providedIn: 'root',
  factory: () => () => import('filerobot-image-editor').then(m => m.default),
});

/** The formats the editor can write back. A save keeps the file's type, so other images are view-only here. */
const SAVE_EXTENSIONS: Record<string, 'png' | 'jpeg' | 'webp'> = {
  'image/png': 'png',
  'image/jpeg': 'jpeg',
  'image/jpg': 'jpeg',
  'image/webp': 'webp',
};

/**
 * The image editor (ADR 0x043 §2): crop, rotate, resize, adjust, filter and annotate, on the
 * Filerobot Image Editor. The host owns saving: this reports changes and hands over the edited
 * image in the file's own format.
 */
@Component({
  selector: 'app-image-editor',
  standalone: true,
  template: `
    @if (unsupported()) {
      <p class="image-editor__notice" role="status">
        This image is {{ file().file_type }}, which the editor cannot write back. Download it to edit it elsewhere.
      </p>
    }
    @if (state() === 'error') {
      <p class="image-editor__notice" role="alert">The image editor could not load.</p>
    }
    <div #host class="image-editor__host" [attr.aria-label]="'Image editor for ' + file().name"></div>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
      }
      .image-editor__host {
        flex: 1 1 auto;
        min-height: 0;
        position: relative;
      }
      .image-editor__notice {
        color: var(--text-secondary);
        margin: 0;
        padding: 0.5rem 1rem;
      }
      /* The workspace's toolbar saves; the editor's own save flow (a naming modal) stays hidden. */
      :host ::ng-deep [class*='FIE_topbar-save'] {
        display: none;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImageEditorComponent implements FileEditor {
  private readonly document = inject(DOCUMENT);
  private readonly load = inject(FILEROBOT_LOADER);
  private readonly host = viewChild.required<ElementRef<HTMLElement>>('host');

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly unsupported = signal(false);

  private editor: Filerobot | null = null;
  private objectUrl: string | null = null;
  private rendered = false;

  private lastContent: Blob | null = null;
  /** Counts changes, so a save can tell whether any landed after it started. */
  private edits = 0;
  private readonly savedEdits = new WeakMap<Blob, number>();

  constructor() {
    afterNextRender(() => {
      this.rendered = true;
      void this.mount(this.blob());
    });
    effect(() => {
      const blob = this.blob();
      if (!this.rendered) return;
      // After a save the host hands back this editor's own bytes; reopening them would discard
      // the undo history and anything changed since.
      if (blob === this.lastContent) return;
      untracked(() => void this.mount(blob));
    });
    inject(DestroyRef).onDestroy(() => this.teardown());
  }

  content(): Promise<Blob> {
    const edits = this.edits;
    return this.render().then(blob => {
      this.lastContent = blob;
      this.savedEdits.set(blob, edits);
      return blob;
    });
  }

  private async render(): Promise<Blob> {
    const type = (this.file().file_type || '').toLowerCase();
    const extension = SAVE_EXTENSIONS[type];
    if (!extension) throw new Error(`cannot write ${type}`);
    if (!this.editor) return this.blob();
    const { imageData, hideLoadingSpinner } = this.editor.getCurrentImgData({ name: baseName(this.file().name), extension, quality: 0.92 }, 1);
    hideLoadingSpinner?.();
    if (imageData.imageCanvas) {
      // The canonical name: browsers do not know image/jpg, and fall back to PNG for any type they
      // cannot encode (WebP on Safari), which would store PNG bytes under the file's type.
      const wanted = `image/${extension}`;
      const blob = await new Promise<Blob | null>(resolve => imageData.imageCanvas!.toBlob(resolve, wanted, 0.92));
      if (blob && blob.type !== wanted) throw new Error(`This browser cannot write ${extension.toUpperCase()} images. Use Save as with another format, or download it.`);
      if (blob) return new Blob([blob], { type });
    }
    if (!imageData.imageBase64) throw new Error('the editor produced no image');
    const response = await fetch(imageData.imageBase64);
    const bytes = await response.arrayBuffer();
    return new Blob([bytes], { type });
  }

  markSaved(saved?: Blob): void {
    // A change made while the save was running is still unsaved.
    const at = saved && this.savedEdits.get(saved);
    this.setDirty(at !== undefined && at !== this.edits);
  }

  private async mount(blob: Blob): Promise<void> {
    this.teardown();
    this.unsupported.set(!SAVE_EXTENSIONS[(this.file().file_type || '').toLowerCase()]);
    this.state.set('loading');
    let ctor: FilerobotCtor;
    try {
      ctor = await this.load();
    } catch {
      this.state.set('error');
      return;
    }
    this.objectUrl = URL.createObjectURL(blob);
    const view = this.document.defaultView;
    const editor = new ctor(this.host().nativeElement, {
      source: this.objectUrl,
      onModify: () => {
        this.edits += 1;
        this.setDirty(true);
      },
      onSave: () => this.saveRequested.emit(),
      avoidChangesNotSavedAlertOnLeave: true,
      showBackButton: false,
      useBackendTranslations: false,
      language: 'en',
      savingPixelRatio: 1,
      previewPixelRatio: view?.devicePixelRatio ?? 1,
    });
    // TABS and TOOLS are instance properties, so the tab set goes in with render().
    const { TABS, TOOLS } = editor;
    editor.render({
      source: this.objectUrl,
      savingPixelRatio: 1,
      previewPixelRatio: view?.devicePixelRatio ?? 1,
      tabsIds: [TABS.ADJUST, TABS.FINETUNE, TABS.FILTERS, TABS.ANNOTATE, TABS.RESIZE],
      defaultTabId: TABS.ADJUST,
      defaultToolId: TOOLS.CROP,
    });
    this.editor = editor;
    this.setDirty(false);
    this.state.set('ready');
  }

  private setDirty(dirty: boolean): void {
    if (this.dirty() === dirty) return;
    this.dirty.set(dirty);
    this.dirtyChange.emit(dirty);
  }

  private teardown(): void {
    this.editor?.terminate();
    this.editor = null;
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl);
      this.objectUrl = null;
    }
  }
}

function baseName(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(0, dot) : name;
}
