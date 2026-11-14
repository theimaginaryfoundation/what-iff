import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { BuiltPage, PageFiles } from './page-files.service';

/**
 * A lone `.html` file rendered (ADR 0x050 §1), with the files it names beside it in its folder.
 *
 * The sandbox allows scripts and nothing else: with no `allow-same-origin` the frame has an opaque
 * origin, so the page cannot reach this app's storage, cookies or pages, and it cannot open popups,
 * submit forms, show dialogs or navigate anything but itself. The page is rendered as it is, scripts
 * included; the sandbox is what contains it.
 */
@Component({
  selector: 'app-html-page-frame',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (notice(); as text) {
      <p class="html-page__notice" role="status">{{ text }}</p>
    }
    @if (failed()) {
      <p class="html-page__placeholder" role="alert">Could not load the files this page uses.</p>
    }
    @if (built(); as page) {
      <iframe
        class="html-page__frame"
        [title]="'Page: ' + file().name"
        sandbox="allow-scripts"
        referrerpolicy="no-referrer"
        [srcdoc]="trusted()"
      ></iframe>
    } @else if (!failed()) {
      <p class="html-page__placeholder" role="status">Loading the page…</p>
    }
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        height: 100%;
        min-height: 0;
      }
      .html-page__frame {
        background: #fff;
        border: 0;
        display: block;
        flex: 1;
        /* A host that does not constrain the height (the gallery page) still gets a usable page,
           as the diagram editor does. */
        min-height: clamp(360px, 75vh, 1000px);
        width: 100%;
      }
      .html-page__notice {
        border-bottom: 1px solid var(--border);
        color: var(--color-text-secondary);
        font-size: 0.75rem;
        margin: 0;
        padding: 0.375rem 0.75rem;
      }
      .html-page__placeholder {
        color: var(--color-text-secondary);
        font-size: 0.875rem;
        padding: 1rem;
      }
    `,
  ],
})
export class HtmlPageFrameComponent {
  private readonly sanitizer = inject(DomSanitizer);
  private readonly pages = inject(PageFiles);

  /** The page's file, for its folder. */
  readonly file = input.required<FileAttachment>();
  /** The page's HTML, as it is now (an editor's unsaved text included). */
  readonly html = input.required<string>();

  readonly built = signal<BuiltPage | null>(null);
  readonly failed = signal(false);
  // Trusted because of where it goes: the sandbox above is what contains it.
  readonly trusted = computed(() => this.sanitizer.bypassSecurityTrustHtml(this.built()?.html ?? ''));
  /** What the page asked for that it does not get. */
  readonly notice = computed(() => {
    const page = this.built();
    if (!page) return null;
    const parts: string[] = [];
    if (page.missing.length) parts.push(`Not in this folder: ${list(page.missing)}.`);
    if (page.skipped.length) parts.push(`Not loaded, as a page on its own loads at most a few hundred files: ${list(page.skipped)}.`);
    return parts.join(' ') || null;
  });

  private generation = 0;

  constructor() {
    effect(() => {
      const file = this.file();
      const html = this.html();
      const generation = ++this.generation;
      untracked(() => void this.build(file, html, generation));
    });
    inject(DestroyRef).onDestroy(() => this.generation++);
  }

  private async build(file: FileAttachment, html: string, generation: number): Promise<void> {
    try {
      const page = await this.pages.build(file, html);
      if (generation !== this.generation) return;
      this.failed.set(false);
      this.built.set(page);
    } catch {
      if (generation === this.generation) this.failed.set(true);
    }
  }
}

function list(names: string[]): string {
  const shown = names.slice(0, 5).join(', ');
  return names.length > 5 ? `${shown} and ${names.length - 5} more` : shown;
}
