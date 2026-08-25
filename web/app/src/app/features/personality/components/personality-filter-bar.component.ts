import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';

export type PersonalitySortMode = 'recent' | 'alpha' | 'most-used';

export interface PersonalityFilterValue {
  query: string;
  sort: PersonalitySortMode;
  defaultOnly: boolean;
}

const SORT_LABELS: Record<PersonalitySortMode, string> = {
  recent: 'Recently updated',
  alpha: 'A → Z',
  'most-used': 'Most used',
};

/**
 * Filter + sort row for the personalities grid. Stays presentational —
 * the parent owns the canonical filter state and reacts to the
 * `valueChange` output to re-query the list.
 */
@Component({
  selector: 'app-personality-filter-bar',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div
      class="personality-filter-bar flex flex-col gap-3 rounded-xl border border-(--color-border-default) bg-(--color-surface-elevated) p-3 sm:flex-row sm:items-center"
      role="region"
      aria-label="Personality filters"
    >
      <label class="flex flex-1 items-center gap-2">
        <span class="sr-only">Search</span>
        <span aria-hidden="true" class="text-(--color-text-secondary)">⌕</span>
        <input
          type="text"
          class="w-full rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
          [placeholder]="searchPlaceholder()"
          [ngModel]="query()"
          (ngModelChange)="onQueryChange($event)"
        />
      </label>

      <label class="flex items-center gap-2">
        <span class="sr-only">Sort</span>
        <select
          class="rounded-lg border border-(--color-border-default) bg-(--color-surface-input) px-3 py-2 text-sm text-(--color-text-primary) outline-none focus:border-(--color-accent) focus:ring-2 focus:ring-(--color-accent)"
          [ngModel]="sort()"
          (ngModelChange)="onSortChange($event)"
        >
          @for (option of sortOptions; track option.value) {
            <option [value]="option.value">{{ option.label }}</option>
          }
        </select>
      </label>

      <label class="inline-flex select-none items-center gap-2 text-sm text-(--color-text-secondary)">
        <input
          type="checkbox"
          class="h-4 w-4 rounded border-(--color-border-default) text-(--color-accent) focus:ring-(--color-accent)"
          [ngModel]="defaultOnly()"
          (ngModelChange)="onDefaultOnlyChange($event)"
        />
        Default only
      </label>
    </div>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityFilterBarComponent {
  readonly value = input<PersonalityFilterValue | null>(null);
  readonly searchPlaceholder = input<string>('Search personalities…');

  readonly valueChange = output<PersonalityFilterValue>();

  readonly sortOptions: { value: PersonalitySortMode; label: string }[] = (
    Object.keys(SORT_LABELS) as PersonalitySortMode[]
  ).map(value => ({ value, label: SORT_LABELS[value] }));

  private readonly internalQuery = signal('');
  private readonly internalSort = signal<PersonalitySortMode>('recent');
  private readonly internalDefaultOnly = signal(false);

  readonly query = computed(() => this.value()?.query ?? this.internalQuery());
  readonly sort = computed<PersonalitySortMode>(() => this.value()?.sort ?? this.internalSort());
  readonly defaultOnly = computed(() => this.value()?.defaultOnly ?? this.internalDefaultOnly());

  onQueryChange(value: string): void {
    this.internalQuery.set(value);
    this.emit();
  }

  onSortChange(value: PersonalitySortMode): void {
    this.internalSort.set(value);
    this.emit();
  }

  onDefaultOnlyChange(value: boolean): void {
    this.internalDefaultOnly.set(value);
    this.emit();
  }

  private emit(): void {
    this.valueChange.emit({
      query: this.query(),
      sort: this.sort(),
      defaultOnly: this.defaultOnly(),
    });
  }
}
