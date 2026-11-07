import { ChangeDetectionStrategy, Component, ViewEncapsulation, computed, inject, input, signal } from '@angular/core';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { MarkdownModule } from 'ngx-markdown';

import { NotebookOutput } from './nbformat';
import { MAX_OUTPUT_CHARS, OutputView, outputViews } from './notebook-outputs';

interface ShownOutput {
  view: OutputView;
  /** HTML already through the output sanitizer, or built from escaped text. */
  trusted?: SafeHtml;
  text?: string;
  truncated?: boolean;
}

/** A code cell's outputs. Encapsulation is off because the sanitized HTML inside needs these styles too. */
@Component({
  selector: 'app-notebook-output',
  standalone: true,
  imports: [MarkdownModule],
  encapsulation: ViewEncapsulation.None,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @for (item of shown(); track $index) {
      @switch (item.view.kind) {
        @case ('html') {
          <div class="nb-output nb-output--html" [innerHTML]="item.trusted"></div>
        }
        @case ('ansi') {
          <pre class="nb-output nb-output--text" [class.nb-output--stderr]="item.view.stderr" [innerHTML]="item.trusted"></pre>
        }
        @case ('image') {
          <img
            class="nb-output nb-output--image"
            alt="Cell output"
            [src]="item.view.src"
            [attr.width]="item.view.width ?? null"
            [attr.height]="item.view.height ?? null"
          />
        }
        @case ('markdown') {
          <markdown class="nb-output prose prose-sm max-w-none dark:prose-invert" [data]="item.view.text"></markdown>
        }
        @case ('unsupported') {
          <p class="nb-output nb-output--note">Interactive output ({{ item.view.mime }}) cannot be shown here.</p>
        }
        @default {
          <pre class="nb-output nb-output--text" [class.nb-output--stderr]="item.view.kind === 'text' && item.view.stderr">{{ item.text }}</pre>
        }
      }
      @if (item.truncated) {
        <button type="button" class="nb-output__more" (click)="showAll.set(true)">Show all output</button>
      }
    }
  `,
  styles: [
    `
      app-notebook-output {
        display: block;
      }
      .nb-output {
        color: var(--text-primary);
        font-size: 0.8125rem;
        margin: 0.25rem 0;
        max-width: 100%;
      }
      .nb-output--text {
        font-family: var(--font-mono, ui-monospace, monospace);
        line-height: 1.4;
        max-height: 32rem;
        overflow: auto;
        white-space: pre-wrap;
        word-break: break-word;
      }
      .nb-output--stderr {
        background: color-mix(in srgb, #dc2626 10%, transparent);
        border-radius: 0.25rem;
        padding: 0.25rem 0.4rem;
      }
      /* contain: a fixed-position element in untrusted HTML cannot leave the output. */
      .nb-output--html {
        contain: layout paint;
        overflow: auto;
      }
      .nb-output--html table {
        border-collapse: collapse;
        font-size: 0.75rem;
      }
      .nb-output--html th,
      .nb-output--html td {
        border: 1px solid var(--border);
        padding: 0.15rem 0.5rem;
        text-align: right;
      }
      .nb-output--html thead th {
        background: var(--bg-card);
      }
      .nb-output--image {
        background: #fff;
        display: block;
        height: auto;
      }
      .nb-output--note {
        color: var(--text-secondary);
        font-style: italic;
      }
      .nb-output__more {
        border: 1px solid var(--border);
        border-radius: 0.4rem;
        color: var(--text-primary);
        font-size: 0.75rem;
        padding: 0.15rem 0.5rem;
      }
      .nb-ansi-black { color: #6b7280; }
      .nb-ansi-red { color: #e5484d; }
      .nb-ansi-green { color: #30a46c; }
      .nb-ansi-yellow { color: #d4a017; }
      .nb-ansi-blue { color: #3b82f6; }
      .nb-ansi-magenta { color: #c026d3; }
      .nb-ansi-cyan { color: #0ea5b7; }
      .nb-ansi-white { color: var(--text-secondary); }
      .nb-ansi-bg-black { background: #374151; }
      .nb-ansi-bg-red { background: color-mix(in srgb, #e5484d 30%, transparent); }
      .nb-ansi-bg-green { background: color-mix(in srgb, #30a46c 30%, transparent); }
      .nb-ansi-bg-yellow { background: color-mix(in srgb, #d4a017 30%, transparent); }
      .nb-ansi-bg-blue { background: color-mix(in srgb, #3b82f6 30%, transparent); }
      .nb-ansi-bg-magenta { background: color-mix(in srgb, #c026d3 30%, transparent); }
      .nb-ansi-bg-cyan { background: color-mix(in srgb, #0ea5b7 30%, transparent); }
      .nb-ansi-bg-white { background: color-mix(in srgb, #9ca3af 30%, transparent); }
      .nb-ansi-bold { font-weight: 700; }
    `,
  ],
})
export class NotebookOutputComponent {
  private readonly sanitizer = inject(DomSanitizer);

  readonly outputs = input.required<readonly NotebookOutput[]>();
  readonly showAll = signal(false);
  // A computed, so the trusted values only change with the outputs and Angular leaves the DOM alone.
  readonly shown = computed<ShownOutput[]>(() => outputViews(this.outputs()).map(view => this.show(view)));

  private show(view: OutputView): ShownOutput {
    if (view.kind === 'html' || view.kind === 'ansi') {
      // Both are safe by construction: sanitizeOutputHtml, or escaped text in fixed spans.
      return { view, trusted: this.sanitizer.bypassSecurityTrustHtml(view.html) };
    }
    if (view.kind === 'text' || view.kind === 'json') {
      const truncated = !this.showAll() && view.text.length > MAX_OUTPUT_CHARS;
      return { view, text: truncated ? view.text.slice(0, MAX_OUTPUT_CHARS) : view.text, truncated };
    }
    return { view };
  }
}
