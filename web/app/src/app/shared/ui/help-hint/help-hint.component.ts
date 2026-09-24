import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  computed,
  inject,
  input,
  signal,
  viewChild,
} from '@angular/core';

import { GuideKey, guideUrl } from '../../../core/constants/guides.constants';
import { CircleHelpIconComponent } from '../icons/icons';

let helpHintId = 0;

/** Shared so add/removeEventListener always see the same options. */
const SCROLL_LISTENER_OPTIONS: AddEventListenerOptions = { capture: true, passive: true };

/**
 * A small (?) button that opens a short explanation of the concept next to it, with an optional
 * link to the matching product guide. Click/tap to toggle, so it works on touch (unlike
 * `uiTooltip`, which is hover/focus-only) and the guide link stays clickable. Escape or a click
 * outside closes it. Body text is projected content; keep it to a sentence or two.
 */
@Component({
  selector: 'ui-help-hint',
  standalone: true,
  imports: [CircleHelpIconComponent],
  templateUrl: './help-hint.component.html',
  styleUrl: './help-hint.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class HelpHintComponent {
  /** Accessible name for the button, e.g. "What is a personality?". */
  readonly label = input.required<string>();
  /** Optional bold heading inside the panel. */
  readonly heading = input<string>('');
  /** Guide to link to; the link is hidden when docs aren't configured. */
  readonly guide = input<GuideKey | null>(null);
  readonly guideLabel = input('Read the guide');
  /** Which edge of the button the panel lines up with. */
  readonly align = input<'start' | 'end'>('start');

  readonly open = signal(false);
  /** Fixed-position coordinates for the panel, clamped to the viewport when it opens. */
  readonly panelPosition = signal<{ top: number; left: number; width: number } | null>(null);
  readonly panelId = `ui-help-hint-${++helpHintId}`;
  readonly guideHref = computed(() => {
    const key = this.guide();
    return key ? guideUrl(key) : null;
  });

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly document = inject(DOCUMENT);
  private readonly trigger = viewChild.required<ElementRef<HTMLButtonElement>>('trigger');
  // Scroll in any container (the page itself scrolls inside an overflow element) moves the
  // trigger out from under a fixed panel, so close instead of leaving it adrift.
  private readonly onAnyScroll = (): void => this.close();

  constructor() {
    inject(DestroyRef).onDestroy(() => this.stopScrollWatch());
  }

  toggle(): void {
    if (this.open()) {
      this.close();
      return;
    }
    this.panelPosition.set(this.computePosition());
    this.open.set(true);
    this.document.addEventListener('scroll', this.onAnyScroll, SCROLL_LISTENER_OPTIONS);
  }

  close(returnFocus = false): void {
    if (!this.open()) return;
    this.open.set(false);
    this.stopScrollWatch();
    if (returnFocus) this.trigger().nativeElement.focus();
  }

  @HostListener('window:resize')
  onResize(): void {
    this.close();
  }

  private stopScrollWatch(): void {
    this.document.removeEventListener('scroll', this.onAnyScroll, SCROLL_LISTENER_OPTIONS);
  }

  /**
   * Places the panel under the trigger (above it if there's no room below), preferring the
   * requested alignment but always keeping it inside the viewport with a small gutter, so it
   * works for triggers anywhere on a narrow screen and inside overflow-clipping containers.
   */
  private computePosition(): { top: number; left: number; width: number } {
    const gutter = 16;
    const gap = 6;
    const view = this.document.defaultView;
    const viewportWidth = view?.innerWidth ?? 1024;
    const viewportHeight = view?.innerHeight ?? 768;
    const rect = this.trigger().nativeElement.getBoundingClientRect();
    const width = Math.min(288, viewportWidth - gutter * 2);
    const preferredLeft = this.align() === 'end' ? rect.right - width : rect.left;
    const left = Math.min(Math.max(preferredLeft, gutter), viewportWidth - gutter - width);
    const estimatedHeight = 160;
    const below = rect.bottom + gap;
    const top = below + estimatedHeight > viewportHeight && rect.top - gap - estimatedHeight > gutter
      ? rect.top - gap - estimatedHeight
      : below;
    return { top, left, width };
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    this.close(true);
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    if (!this.host.nativeElement.contains(event.target as Node)) this.close();
  }
}
