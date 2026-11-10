import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, inject, input, output, signal, viewChild } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { DomSanitizer } from '@angular/platform-browser';

import { usableRunUrl } from './notebook-run.config';

/** The site downloads a lot the first time, so this is generous. */
const START_TIMEOUT_MS = 90_000;
const CLOSE_TIMEOUT_MS = 5_000;

/**
 * JupyterLite in a frame (ADR 0x043 §11): hands it the notebook once it says it is ready, and passes
 * its saves up. It runs in the browser on an origin of its own; only the notebook crosses over.
 */
@Component({
  selector: 'app-notebook-run',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (state() === 'failed' || !trustedUrl()) {
      <p class="notebook-run__message" role="alert">The notebook runner could not be reached. Check your connection, or edit the cells without running them.</p>
    } @else {
      @if (state() === 'starting') {
        <p class="notebook-run__message" role="status">Starting Python in your browser. The first time takes a while…</p>
      }
      <!--
        allow-same-origin gives JupyterLite its own origin's storage, which it needs to start. It
        is safe only because usableRunUrl refuses this app's origin; change the two together.
        The rest are JupyterLab's own UI: downloads (Download, export), forms and modals (its
        dialogs), popups (help and documentation links).
      -->
      <iframe
        #frame
        class="notebook-run__frame"
        [class.notebook-run__frame--hidden]="state() === 'starting'"
        title="Run this notebook"
        sandbox="allow-scripts allow-same-origin allow-downloads allow-forms allow-modals allow-popups"
        referrerpolicy="no-referrer"
        [src]="trustedUrl()"
      ></iframe>
    }
  `,
  styles: [
    `
      :host {
        display: flex;
        flex: 1 1 clamp(360px, 75vh, 1000px);
        flex-direction: column;
        min-height: 0;
      }
      .notebook-run__frame {
        border: 0;
        flex: 1 1 auto;
        min-height: 0;
        width: 100%;
      }
      .notebook-run__frame--hidden {
        height: 0;
        visibility: hidden;
      }
      .notebook-run__message {
        color: var(--text-secondary);
        padding: 2rem 1rem;
        text-align: center;
      }
    `,
  ],
})
export class NotebookRunComponent {
  private readonly sanitizer = inject(DomSanitizer);
  private readonly document = inject(DOCUMENT);
  private readonly frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');

  readonly url = input.required<string>();
  readonly name = input.required<string>();
  /** The notebook as parsed JSON, read once when the runner is ready. */
  readonly notebook = input.required<unknown>();

  /** The runner saved: the notebook as parsed JSON. */
  readonly saved = output<unknown>();
  readonly closed = output<void>();

  readonly state = signal<'starting' | 'ready' | 'failed'>('starting');
  /**
   * The URL is checked again here, not only where the token is made, so this component never
   * frames a same-origin or plain-http runner, whoever hands it the URL.
   */
  private readonly checkedUrl = computed(() => usableRunUrl(this.url(), this.document.location.origin));
  readonly trustedUrl = computed(() => {
    const url = this.checkedUrl();
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
  });
  private readonly origin = computed(() => new URL(this.checkedUrl() ?? 'about:blank').origin);

  private startTimer: ReturnType<typeof setTimeout> | null = null;
  private closeTimer: ReturnType<typeof setTimeout> | null = null;
  private opened = false;

  constructor() {
    const window = globalThis.window;
    const onMessage = (event: MessageEvent) => this.onMessage(event);
    window.addEventListener('message', onMessage);
    this.startTimer = setTimeout(() => {
      if (this.state() === 'starting') this.state.set('failed');
    }, START_TIMEOUT_MS);
    inject(DestroyRef).onDestroy(() => {
      window.removeEventListener('message', onMessage);
      if (this.startTimer) clearTimeout(this.startTimer);
      if (this.closeTimer) clearTimeout(this.closeTimer);
    });
  }

  /** Asks the runner to save what is unsaved, then reports closed; it gets a few seconds. */
  requestClose(): void {
    const target = this.frame()?.nativeElement.contentWindow;
    if (!target || this.state() !== 'ready') {
      this.closed.emit();
      return;
    }
    target.postMessage({ type: 'whatiff:close' }, this.origin());
    this.closeTimer = setTimeout(() => this.closed.emit(), CLOSE_TIMEOUT_MS);
  }

  private onMessage(event: MessageEvent): void {
    const target = this.frame()?.nativeElement.contentWindow;
    // Only the frame we made, on the origin we chose, can speak to the editor.
    if (!target || event.source !== target || event.origin !== this.origin()) return;
    const message = event.data as { type?: string; content?: unknown } | null;
    switch (message?.type) {
      case 'whatiff:ready':
        if (this.opened) return;
        this.opened = true;
        this.state.set('ready');
        target.postMessage({ type: 'whatiff:open', name: this.name(), content: this.notebook() }, this.origin());
        return;
      case 'whatiff:save':
        if (message.content && typeof message.content === 'object') this.saved.emit(message.content);
        return;
      case 'whatiff:closed':
        if (this.closeTimer) clearTimeout(this.closeTimer);
        this.closed.emit();
        return;
    }
  }
}
