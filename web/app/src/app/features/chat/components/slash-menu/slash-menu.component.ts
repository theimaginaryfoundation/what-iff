
import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal } from '@angular/core';

import { SlashCommand } from '../../helpers/slash-command.helpers';

@Component({
  selector: 'app-slash-menu',
  standalone: true,
  imports: [],
  template: `
    @if (open() && commands().length) {
      <div
        class="slash-menu"
        role="listbox"
        [attr.aria-activedescendant]="activeId()"
        aria-label="Slash commands"
        (keydown)="onKeydown($event)"
      >
        @for (command of commands(); track command.id; let index = $index) {
          <button
            type="button"
            role="option"
            class="slash-menu__row"
            [id]="rowId(command)"
            [class.slash-menu__row--active]="index === selectedIndex()"
            [attr.aria-selected]="index === selectedIndex()"
            (mouseenter)="selectedIndex.set(index)"
            (click)="select(command)"
          >
            <span>/{{ command.id }}</span>
            <small>{{ command.description }}</small>
          </button>
        }
      </div>
    }
  `,
  styles: [`
    .slash-menu {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 1rem;
      box-shadow: 0 18px 45px rgb(0 0 0 / 0.18);
      display: grid;
      gap: 0.25rem;
      max-height: 18rem;
      overflow-y: auto;
      padding: 0.375rem;
    }

    .slash-menu__row {
      border-radius: 0.75rem;
      color: var(--color-text-primary);
      display: grid;
      gap: 0.125rem;
      padding: 0.625rem 0.75rem;
      text-align: left;
    }

    .slash-menu__row--active,
    .slash-menu__row:hover {
      background: var(--color-surface-muted);
    }

    small {
      color: var(--color-text-muted);
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SlashMenuComponent {
  readonly commands = input.required<readonly SlashCommand[]>();
  readonly open = input(false);
  readonly selected = output<SlashCommand>();
  readonly closed = output<void>();
  readonly selectedIndex = signal(0);
  readonly activeId = computed(() => this.commands()[this.selectedIndex()] ? this.rowId(this.commands()[this.selectedIndex()]) : null);

  constructor() {
    effect(() => {
      this.commands();
      this.selectedIndex.set(0);
    });
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      this.closed.emit();
      return;
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      this.move(event.key === 'ArrowDown' ? 1 : -1);
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      const command = this.commands()[this.selectedIndex()];
      if (command) this.select(command);
    }
  }

  select(command: SlashCommand): void {
    this.selected.emit(command);
  }

  /** Selects the highlighted row (for keyboard handling in the parent textarea). */
  selectHighlighted(): void {
    const command = this.commands()[this.selectedIndex()];
    if (command) this.select(command);
  }

  rowId(command: SlashCommand): string {
    return `slash-command-${command.id}`;
  }

  private move(delta: number): void {
    const count = this.commands().length;
    if (!count) return;
    this.selectedIndex.set((this.selectedIndex() + delta + count) % count);
  }
}
