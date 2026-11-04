import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { SelectionState } from '../helpers/selection-model';
import { CheckboxComponent } from '../checkbox/checkbox.component';

/**
 * The bordered table the Thread Manager and the gallery list view share: a select-all checkbox
 * column, the caller's header cells and rows, and a message in place of the rows while loading,
 * empty or failed.
 *
 * Header cells go in `[uiTableHead]` (an `ng-container ngProjectAs="[uiTableHead]"` of `<th>`s);
 * rows go in the default slot and render their own `<tr>` (a row component with
 * `:host { display: contents }`). The first cell of each row is its selection checkbox.
 */
@Component({
  selector: 'ui-data-table',
  standalone: true,
  imports: [CheckboxComponent],
  templateUrl: './data-table.component.html',
  styleUrl: './data-table.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DataTableComponent {
  readonly selectAllState = input<SelectionState>('none');
  /** Accessible name of the select-all checkbox, e.g. "Select all threads". */
  readonly selectAllLabel = input.required<string>();
  readonly selectAllTooltip = input('');
  /** Shown instead of the rows when set (loading, empty, or an error). */
  readonly message = input<string | null>(null);
  readonly messageTone = input<'muted' | 'error'>('muted');
  readonly bodyLabel = input<string | null>(null);
  /** `listbox` when rows are options (Thread Manager); otherwise the table's own semantics. */
  readonly bodyRole = input<string | null>(null);

  readonly selectAllChange = output<void>();
}
