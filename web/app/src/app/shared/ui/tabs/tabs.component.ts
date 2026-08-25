import { ChangeDetectionStrategy, Component, effect, signal, input, output } from '@angular/core';

@Component({
  selector: 'ui-tabs',
  standalone: true,
  templateUrl: './tabs.component.html',
  styleUrl: './tabs.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class TabsComponent {
  readonly ariaLabel = input<string | null>(null);
  readonly activeId = input<string | null>(null);
  readonly activeIdChange = output<string>();
  readonly selectedId = signal<string | null>(null);
  private readonly tabs: Array<{ id: string; element: HTMLButtonElement }> = [];

  constructor() {
    effect(() => {
      const activeId = this.activeId();

      if (activeId) {
        this.selectedId.set(activeId);
      }
    });
  }

  registerTab(id: string, element: HTMLButtonElement): void {
    if (!this.tabs.some(tab => tab.id === id)) {
      this.tabs.push({ id, element });
    }

    if (!this.selectedId()) {
      this.selectedId.set(this.activeId() ?? id);
    }
  }

  unregisterTab(id: string): void {
    const index = this.tabs.findIndex(tab => tab.id === id);
    if (index >= 0) this.tabs.splice(index, 1);
  }

  activate(id: string): void {
    this.selectedId.set(id);
    this.activeIdChange.emit(id);
  }

  isSelected(id: string): boolean { return this.selectedId() === id; }

  focusRelative(id: string, delta: number): void {
    const index = this.tabs.findIndex(tab => tab.id === id);
    if (index < 0 || this.tabs.length === 0) return;
    const next = (index + delta + this.tabs.length) % this.tabs.length;
    this.tabs[next].element.focus();
  }

  focusFirst(): void { this.tabs[0]?.element.focus(); }
  focusLast(): void { this.tabs[this.tabs.length - 1]?.element.focus(); }
}
