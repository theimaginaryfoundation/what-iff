import { ChangeDetectionStrategy, Component, computed, input, linkedSignal, output } from '@angular/core';

import {
  AgentJobParameter,
  AgentJobParameterValue,
  AgentJobParameterValues,
} from '../../../../core/models/agent-job.model';
import { CheckboxComponent } from '../../../../shared/ui/checkbox/checkbox.component';
import { MAX_LIST_ITEMS, isValueSet } from '../../helpers/job-params.helpers';
import { ChipListInputComponent } from './chip-list-input.component';

/** One field per parameter definition, in order: the form view of a job's parameters (ADR 0x044). */
@Component({
  selector: 'app-job-parameters-form',
  standalone: true,
  imports: [CheckboxComponent, ChipListInputComponent],
  templateUrl: './job-parameters-form.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobParametersFormComponent {
  readonly parameters = input<AgentJobParameter[]>([]);
  readonly values = input<AgentJobParameterValues>({});
  readonly disabled = input(false);

  readonly valuesChange = output<AgentJobParameterValues>();

  readonly maxListItems = MAX_LIST_ITEMS;
  /**
   * The values as last edited here, reset whenever the parent passes new ones. Edits build on this,
   * not on the input (which only catches up at the next change detection), so quick successive
   * edits to different fields don't overwrite each other.
   */
  readonly current = linkedSignal(() => this.values());

  readonly fields = computed(() =>
    this.parameters().map((def, index) => {
      const value = this.current()[def.name];
      return {
        def,
        id: `job-param-${index}`,
        label: (def.label || '').trim() || def.name,
        missing: !isValueSet(def, value),
        text: typeof value === 'string' ? value : '',
        items: Array.isArray(value) ? value : [],
      };
    }),
  );

  set(name: string, value: AgentJobParameterValue | undefined): void {
    const next = { ...this.current() };
    if (value === undefined || (Array.isArray(value) ? value.length === 0 : value === '')) {
      delete next[name];
    } else {
      next[name] = value;
    }
    this.current.set(next);
    this.valuesChange.emit(next);
  }

  toggleOption(def: AgentJobParameter, option: string, checked: boolean): void {
    const value = this.current()[def.name];
    const selected = new Set(Array.isArray(value) ? value : []);
    if (checked) selected.add(option);
    else selected.delete(option);
    // Keep the definition's option order.
    this.set(def.name, (def.options ?? []).filter(o => selected.has(o)));
  }

  isChecked(items: string[], option: string): boolean {
    return items.includes(option);
  }
}
