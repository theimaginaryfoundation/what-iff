import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, Injector, afterNextRender, inject, input, output, signal } from '@angular/core';

import { Chat, isSandboxed } from '../../core/models/chat.model';
import type { ContextPanelTab } from '../../features/chat/services/context-panel.service';
import { listenForOutsideClick } from '../../shared/ui/helpers/outside-click.helpers';
import { MoreVerticalIconComponent, ShieldIconComponent, StarIconComponent } from '../../shared/ui/icons/icons';

/**
 * The "⋮" button on a sidebar thread row and the small menu it opens: star / unstar the thread,
 * open its Scratchpad, Memories, Tools or Files panel, and archive it. The button shows on row hover or keyboard focus, and stays while
 * the menu is open. The parent supplies the positioned row wrapper; this renders only the control.
 */
@Component({
  selector: 'app-thread-row-menu',
  standalone: true,
  imports: [MoreVerticalIconComponent, ShieldIconComponent, StarIconComponent],
  template: `
    <button
      type="button"
      class="menu__trigger"
      [class.menu__trigger--open]="open()"
      aria-haspopup="menu"
      [attr.aria-expanded]="open()"
      [attr.aria-label]="'Actions for ' + thread().name"
      data-testid="thread-menu-trigger"
      (click)="toggle($event)"
      (keydown.escape)="closeAndRefocus($event)"
      (keydown.arrowDown)="openFromKey($event)"
    >
      <ui-more-vertical-icon [size]="14" />
    </button>
    @if (open()) {
      <div
        class="menu__panel"
        [style.top.px]="pos().top"
        [style.right.px]="pos().right"
        [style.max-height.px]="pos().maxHeight"
        role="menu"
        [attr.aria-label]="'Actions for ' + thread().name"
        (keydown)="onMenuKey($event)"
      >
        <button type="button" role="menuitem" class="menu__item" data-testid="thread-menu-star" (click)="onStar($event)">
          <ui-star-icon [size]="14" [filled]="!!thread().is_favorite" />
          <span>{{ thread().is_favorite ? 'Unstar' : 'Star' }}</span>
        </button>
        @for (link of panelLinks; track link.tab) {
          <button type="button" role="menuitem" class="menu__item" [attr.data-testid]="'thread-menu-' + link.tab" (click)="onPanel($event, link.tab)">
            <span class="menu__item-label">{{ link.label }}</span>
            @if (link.tab === 'memories' && sandboxed()) {
              <ui-shield-icon class="menu__sandbox" [size]="12" aria-label="Sandboxed thread" role="img" />
            }
          </button>
        }
        <hr class="menu__divider" />
        <button type="button" role="menuitem" class="menu__item" data-testid="thread-menu-archive" (click)="onArchive($event)">
          <span class="menu__item-label">Archive</span>
        </button>
      </div>
    }
  `,
  styles: [
    `
      :host {
        position: relative;
        display: inline-flex;
      }
      /* A 24x24 hit area (WCAG 2.5.8) whose box is invisible except while focused or open: the
         row shows only the dots (hover just brightens them), and the target stays comfortably clickable. */
      .menu__trigger {
        align-items: center;
        background: transparent;
        border: 1px solid transparent;
        border-radius: 6px;
        color: var(--text-secondary);
        cursor: pointer;
        display: inline-flex;
        height: 24px;
        justify-content: center;
        width: 24px;
      }
      .menu__trigger:hover {
        color: var(--text-primary, currentColor);
      }
      .menu__trigger:focus-visible,
      .menu__trigger--open {
        background: var(--bg-base);
        color: var(--text-primary, currentColor);
        border-color: var(--accent);
      }
      .menu__trigger:focus-visible,
      .menu__item:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: 1px;
      }
      .menu__panel {
        background: var(--bg-base);
        border: 1px solid var(--border);
        border-radius: 8px;
        box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35);
        display: grid;
        min-width: 150px;
        padding: 4px;
        /* Fixed and above every other layer, so no row, panel or scroll area can cover or clip it.
           Placed from the button's position when it opens (see place()). */
        overflow-y: auto;
        position: fixed;
        z-index: 1300;
      }
      .menu__item {
        align-items: center;
        background: transparent;
        border: 0;
        border-radius: 5px;
        color: var(--text-secondary);
        cursor: pointer;
        display: flex;
        font-size: 12px;
        gap: 8px;
        padding: 6px 8px;
        text-align: left;
      }
      .menu__item:hover {
        background: var(--accent-bg);
      }
      .menu__sandbox {
        color: var(--color-warning);
        margin-left: auto;
      }
      .menu__divider {
        border: 0;
        border-top: 1px solid var(--border);
        margin: 4px 0;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadRowMenuComponent {
  readonly thread = input.required<Chat>();
  readonly toggleStar = output<Chat>();
  readonly archive = output<Chat>();
  /** Open the thread with this right-panel section showing. */
  readonly openPanel = output<{ thread: Chat; tab: ContextPanelTab }>();
  readonly panelLinks: readonly { tab: ContextPanelTab; label: string }[] = [
    { tab: 'scratchpad', label: 'Scratchpad' },
    { tab: 'memories', label: 'Memories' },
    { tab: 'tools', label: 'Tools' },
    { tab: 'files', label: 'Files' },
  ];
  /** Lets the row keep the button visible while the menu is open. */
  readonly openChange = output<boolean>();

  readonly open = signal(false);
  /** Where the open menu sits, in viewport pixels: under the button, or above it when there is no room below. */
  readonly pos = signal<{ top: number; right: number; maxHeight: number }>({ top: 0, right: 0, maxHeight: 400 });

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);
  /** Where the open panel should land in viewport pixels; `settle()` corrects the drawn position to it. */
  private target = { top: 0, right: 0 };

  constructor() {
    listenForOutsideClick(() => this.close());
    // The menu is fixed to the viewport, so a scroll or resize would leave it behind: close it.
    const win = this.host.nativeElement.ownerDocument.defaultView;
    const dismiss = (event: Event) => {
      if (!(event.target instanceof Node) || !this.host.nativeElement.contains(event.target)) this.close();
    };
    win?.addEventListener('resize', dismiss);
    win?.addEventListener('scroll', dismiss, true);
    inject(DestroyRef).onDestroy(() => {
      win?.removeEventListener('resize', dismiss);
      win?.removeEventListener('scroll', dismiss, true);
    });
  }

  /** A sandboxed thread's Memories link carries a shield: that is where its sandbox is set. */
  sandboxed(): boolean {
    return isSandboxed(this.thread());
  }

  toggle(event: Event): void {
    event.stopPropagation();
    const next = !this.open();
    this.set(next);
    if (next) this.focusItem(0);
  }

  /** ArrowDown on the closed button opens the menu, as in the menu-button pattern. */
  openFromKey(event: Event): void {
    event.preventDefault();
    if (!this.open()) {
      this.set(true);
      this.focusItem(0);
    }
  }

  closeAndRefocus(event?: Event): void {
    event?.stopPropagation();
    if (!this.open()) return;
    this.set(false);
    this.trigger()?.focus();
  }

  /** Arrow keys, Home/End move between items; Escape closes and returns to the button; Tab leaves. */
  onMenuKey(event: KeyboardEvent): void {
    const items = this.items();
    const at = items.indexOf(this.host.nativeElement.ownerDocument.activeElement as HTMLElement);
    switch (event.key) {
      case 'ArrowDown':
        items[(at + 1) % items.length]?.focus();
        break;
      case 'ArrowUp':
        items[(at - 1 + items.length) % items.length]?.focus();
        break;
      case 'Home':
        items[0]?.focus();
        break;
      case 'End':
        items[items.length - 1]?.focus();
        break;
      case 'Escape':
        this.closeAndRefocus(event);
        break;
      case 'Tab':
        this.set(false);
        return;
      default:
        return;
    }
    event.preventDefault();
  }

  private items(): HTMLElement[] {
    return Array.from(this.host.nativeElement.querySelectorAll<HTMLElement>('[role="menuitem"]'));
  }

  private trigger(): HTMLElement | null {
    return this.host.nativeElement.querySelector<HTMLElement>('[data-testid="thread-menu-trigger"]');
  }

  private focusItem(index: number): void {
    afterNextRender(() => this.items()[index]?.focus(), { injector: this.injector });
  }

  close(): void {
    if (this.open()) this.set(false);
  }

  onStar(event: Event): void {
    event.stopPropagation();
    this.toggleStar.emit(this.thread());
    this.close();
  }

  onPanel(event: Event, tab: ContextPanelTab): void {
    event.stopPropagation();
    this.openPanel.emit({ thread: this.thread(), tab });
    this.close();
  }

  onArchive(event: Event): void {
    event.stopPropagation();
    this.archive.emit(this.thread());
    this.close();
  }

  /** Positions the panel from the button: below it, or above when the space below is too small. */
  private place(): void {
    const trigger = this.trigger();
    const win = this.host.nativeElement.ownerDocument.defaultView;
    if (!trigger || !win) return;
    const rect = trigger.getBoundingClientRect();
    const gap = 4;
    const margin = 8;
    const wanted = 260; // about the height of the six items and the divider
    const below = win.innerHeight - rect.bottom - gap - margin;
    const above = rect.top - gap - margin;
    const openUp = below < wanted && above > below;
    const maxHeight = Math.max(120, openUp ? above : below);
    const top = openUp ? Math.max(margin, rect.top - gap - Math.min(wanted, maxHeight)) : rect.bottom + gap;
    const right = Math.max(margin, win.innerWidth - rect.right);
    this.target = { top, right: win.innerWidth - right };
    this.pos.set({ top, right, maxHeight });
  }

  /**
   * An ancestor with a transform (the mobile sidebar drawer slides in with one) becomes the containing
   * block of `position: fixed`, so the offsets above would be measured from the drawer, not the viewport.
   * Once drawn, measure where the panel really is and shift it by the difference.
   */
  private settle(): void {
    const panel = this.host.nativeElement.querySelector<HTMLElement>('.menu__panel');
    if (!panel) return;
    const box = panel.getBoundingClientRect();
    const dTop = this.target.top - box.top;
    const dRight = box.right - this.target.right;
    if (Math.abs(dTop) < 0.5 && Math.abs(dRight) < 0.5) return;
    this.pos.update(p => ({ ...p, top: p.top + dTop, right: p.right + dRight }));
  }

  private set(value: boolean): void {
    if (value) this.place();
    this.open.set(value);
    if (value) afterNextRender(() => this.settle(), { injector: this.injector });
    this.openChange.emit(value);
  }
}
