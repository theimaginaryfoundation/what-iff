import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, computed, inject, input, signal, viewChild } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';

const DEFAULT_HEIGHT_PX = 360;
const MIN_HEIGHT_PX = 60;
const MAX_HEIGHT_PX = 1600;

/**
 * One script-driven output in a frame of its own. The sandbox allows scripts and nothing else: with
 * no `allow-same-origin` the frame has an opaque origin, so it cannot reach this page, its storage or
 * its cookies, and it cannot open popups, submit forms or navigate the app. The output may load
 * libraries from a CDN, which is why the reader has to ask for it.
 */
@Component({
  selector: 'app-notebook-interactive-frame',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <iframe
      #frame
      class="nb-output__frame"
      title="Interactive output"
      sandbox="allow-scripts"
      referrerpolicy="no-referrer"
      [style.height.px]="height()"
      [srcdoc]="trusted()"
    ></iframe>
  `,
  styles: [
    `
      .nb-output__frame {
        background: #fff;
        border: 1px solid var(--border);
        border-radius: 0.25rem;
        display: block;
        width: 100%;
      }
    `,
  ],
})
export class NotebookInteractiveFrameComponent {
  private readonly sanitizer = inject(DomSanitizer);
  private readonly frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');

  readonly document = input.required<string>();
  readonly height = signal(DEFAULT_HEIGHT_PX);
  // Trusted because of where it goes: the sandbox above is what contains it.
  readonly trusted = computed(() => this.sanitizer.bypassSecurityTrustHtml(this.document()));

  constructor() {
    const onMessage = (event: MessageEvent) => {
      // A frame with an opaque origin has no origin to check, so the window is what identifies it.
      if (event.source !== this.frame()?.nativeElement.contentWindow) return;
      const data = event.data as { type?: unknown; height?: unknown } | null;
      if (data?.type !== 'nb-output-height' || typeof data.height !== 'number' || !Number.isFinite(data.height)) return;
      this.height.set(Math.max(MIN_HEIGHT_PX, Math.min(MAX_HEIGHT_PX, Math.ceil(data.height))));
    };
    window.addEventListener('message', onMessage);
    inject(DestroyRef).onDestroy(() => window.removeEventListener('message', onMessage));
  }
}
