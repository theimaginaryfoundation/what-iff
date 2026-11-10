import { ChangeDetectionStrategy, Component, computed, input, linkedSignal, output } from '@angular/core';

import {
  AgentJobParameter,
  AgentJobParameterFormat,
  AgentJobParameterType,
} from '../../../../core/models/agent-job.model';
import { TagComponent } from '../../../../shared/ui/tag/tag.component';
import {
  MAX_OPTIONS,
  MAX_PARAMETERS,
  PARAMETER_TYPE_OPTIONS,
  ParameterAnalysis,
  defaultFormat,
  isMultiType,
  isSelectType,
} from '../../helpers/job-params.helpers';
import { ChipListInputComponent } from './chip-list-input.component';

/** A rename the builder reports when a name field loses focus with a new name. */
export interface ParameterRename {
  from: string;
  to: string;
}

/**
 * The builder view of a job's parameters (ADR 0x044): one row per definition, then the problems
 * found against the prompt. Undefined placeholders are errors with an "Add definition" action;
 * definitions the prompt doesn't use are notes.
 */
@Component({
  selector: 'app-job-parameters-builder',
  standalone: true,
  imports: [ChipListInputComponent, TagComponent],
  templateUrl: './job-parameters-builder.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobParametersBuilderComponent {
  readonly parameters = input<AgentJobParameter[]>([]);
  readonly analysis = input.required<ParameterAnalysis>();
  readonly disabled = input(false);

  readonly parametersChange = output<AgentJobParameter[]>();
  readonly renamed = output<ParameterRename>();

  /**
   * The definitions as last edited here, reset whenever the parent passes new ones. Edits build on
   * this, not on the input (which only catches up at the next change detection), so fast typing
   * doesn't drop characters.
   */
  readonly current = linkedSignal(() => this.parameters());

  readonly typeOptions = PARAMETER_TYPE_OPTIONS;
  readonly maxOptions = MAX_OPTIONS;
  readonly isSelectType = isSelectType;
  readonly isMultiType = isMultiType;
  readonly canAdd = computed(() => this.current().length < MAX_PARAMETERS);
  readonly listProblems = computed(() => this.analysis().problems.filter(p => p.index === null));

  /** The name each row had when its name field got focus, to report a rename on blur. */
  private readonly nameAtFocus = new Map<number, string>();

  rowProblems(index: number): string[] {
    return this.analysis().problems.filter(p => p.index === index).map(p => p.message);
  }

  isUnused(name: string): boolean {
    return this.analysis().unusedDefinitions.includes(name.trim());
  }

  add(name = ''): void {
    if (!this.canAdd()) return;
    this.emit([...this.current(), { name, label: '', type: 'text' }]);
  }

  update(index: number, patch: Partial<AgentJobParameter>): void {
    this.emit(this.current().map((def, i) => (i === index ? { ...def, ...patch } : def)));
  }

  setType(index: number, type: AgentJobParameterType): void {
    const def = this.current()[index];
    this.update(index, {
      type,
      options: isSelectType(type) ? (def.options ?? []) : undefined,
      format: isMultiType(type) ? (isMultiType(def.type) ? def.format : defaultFormat(type)) : undefined,
    });
  }

  setFormat(index: number, format: AgentJobParameterFormat): void {
    this.update(index, { format });
  }

  formatOf(def: AgentJobParameter): AgentJobParameterFormat | undefined {
    return def.format || defaultFormat(def.type);
  }

  move(index: number, delta: -1 | 1): void {
    const to = index + delta;
    const defs = [...this.current()];
    if (to < 0 || to >= defs.length) return;
    [defs[index], defs[to]] = [defs[to], defs[index]];
    this.emit(defs);
  }

  remove(index: number): void {
    this.emit(this.current().filter((_, i) => i !== index));
  }

  private emit(defs: AgentJobParameter[]): void {
    this.current.set(defs);
    this.parametersChange.emit(defs);
  }

  onNameFocus(index: number): void {
    this.nameAtFocus.set(index, (this.current()[index]?.name || '').trim());
  }

  onNameBlur(index: number): void {
    const from = this.nameAtFocus.get(index) ?? '';
    this.nameAtFocus.delete(index);
    const to = (this.current()[index]?.name || '').trim();
    if (from && to && from !== to) this.renamed.emit({ from, to });
  }
}
