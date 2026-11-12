import { ChangeDetectionStrategy, Component, ElementRef, Injector, afterNextRender, inject, input, output, signal } from '@angular/core';

import { Chat, isSandboxed } from '../../core/models/chat.model';
import { listenForOutsideClick } from '../../shared/ui/helpers/outside-click.helpers';
import { MoreVerticalIconComponent, ShieldIconComponent, StarIconComponent } from '../../shared/ui/icons/icons';

/**
 * The "⋮" button on a sidebar thread row and the small menu it opens: star / unstar the thread
 * and open its sandbox settings. The button shows on row hover or keyboard focus, and stays while
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
        role="menu"
        [attr.aria-label]="'Actions for ' + thread().name"
        (keydown)="onMenuKey($event)"
      >
        <button type="button" role="menuitem" class="menu__item" data-testid="thread-menu-star" (click)="onStar($event)">
          <ui-star-icon [size]="14" [filled]="!!thread().is_favorite" />
          <span>{{ thread().is_favorite ? 'Unstar' : 'Star' }}</span>
        </button>
        <button type="button" role="menuitem" class="menu__item" data-testid="thread-menu-sandbox" (click)="onSandbox($event)">
          <ui-shield-icon [size]="14" />
          <span>Sandbox</span>
          <span class="menu__state" [class.menu__state--on]="sandboxed()">{{ sandboxed() ? 'On' : 'Off' }}</span>
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
        position: absolute;
        right: 0;
        top: calc(100% + 4px);
        z-index: 30;
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
      .menu__state {
        color: var(--text-muted);
        font-size: 11px;
        margin-left: auto;
      }
      .menu__state--on {
        color: var(--color-warning);
        font-weight: 600;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadRowMenuComponent {
  readonly thread = input.required<Chat>();
  readonly toggleStar = output<Chat>();
  readonly openSandbox = output<Chat>();
  /** Lets the row keep the button visible while the menu is open. */
  readonly openChange = output<boolean>();

  readonly open = signal(false);

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

  constructor() {
    listenForOutsideClick(() => this.close());
  }

  sandboxed(): boolean {
    return isSandboxed(this.thread()) === true;
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

  onSandbox(event: Event): void {
    event.stopPropagation();
    this.openSandbox.emit(this.thread());
    this.close();
  }

  private set(value: boolean): void {
    this.open.set(value);
    this.openChange.emit(value);
  }
}
