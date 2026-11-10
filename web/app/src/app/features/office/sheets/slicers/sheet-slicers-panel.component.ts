import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';

import { SlicerEntry, SlicerWorkspace } from './slicer-workspace';

/**
 * Slicers: a click chooses one item, Ctrl or Shift adds to the choice, as in Excel. The grid shows each
 * slicer's state as a picture where it sits; choosing happens here.
 */
@Component({
  selector: 'app-sheet-slicers-panel',
  standalone: true,
  templateUrl: './sheet-slicers-panel.component.html',
  styleUrl: './sheet-slicers-panel.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SheetSlicersPanelComponent {
  readonly workspace = input.required<SlicerWorkspace>();
  readonly changed = output<void>();

  readonly targetId = signal('');
  readonly field = signal('');
  readonly targets = computed(() => this.workspace().targets());
  readonly target = computed(() => this.targets().find(t => t.id === this.targetId()) ?? this.targets()[0] ?? null);

  itemsOf(entry: SlicerEntry): { label: string; selected: boolean }[] {
    return this.workspace().itemsFor(entry);
  }

  click(entry: SlicerEntry, label: string, event: MouseEvent): void {
    this.workspace().click(entry.name, label, event.ctrlKey || event.metaKey || event.shiftKey);
    this.changed.emit();
  }

  clear(entry: SlicerEntry): void {
    this.workspace().clearFilter(entry.name);
    this.changed.emit();
  }

  remove(entry: SlicerEntry): void {
    this.workspace().remove(entry.name);
    this.changed.emit();
  }

  add(): void {
    const target = this.target();
    const field = this.field() || target?.fields[0];
    if (!target || !field) return;
    this.workspace().add(target.id, field);
    this.field.set('');
    this.changed.emit();
  }
}
