import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { PersonalityOption, ThreadSort } from '../../helpers/thread-list.helpers';

@Component({
  selector: 'app-thread-filter-bar',
  standalone: true,
  template: `
    <section class="filters" aria-label="Thread filters">
      <label>
        Sort
        <select [value]="sort()" (change)="sortChange.emit($any($event.target).value)">
          <option value="recent">Recent activity</option>
          <option value="newest">Newest created</option>
          <option value="alphabetical">Alphabetical</option>
        </select>
      </label>

      <label>
        Personality
        <select [value]="personalityId() ?? ''" (change)="personalityChange.emit($any($event.target).value || null)">
          <option value="">All</option>
          @for (personality of personalities(); track personality.id) {
            <option [value]="personality.id">{{ personality.label }}</option>
          }
        </select>
      </label>

      <label>
        Tag
        <select [value]="tag() ?? ''" (change)="tagChange.emit($any($event.target).value || null)">
          <option value="">All</option>
          @for (item of tags(); track item) {
            <option [value]="item">{{ item }}</option>
          }
        </select>
      </label>

      <label class="filters__toggle">
        <input type="checkbox" [checked]="pinnedOnly()" (change)="pinnedOnlyChange.emit($any($event.target).checked)" />
        Pinned only
      </label>
    </section>
  `,
  styles: [`
    .filters {
      display: grid;
      gap: 0.5rem;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    label {
      color: var(--color-text-muted);
      display: grid;
      font-size: 0.75rem;
      gap: 0.25rem;
    }

    select {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      color: var(--color-text-primary);
      padding: 0.45rem 0.5rem;
    }

    .filters__toggle {
      align-items: center;
      display: flex;
      gap: 0.35rem;
      padding-top: 1rem;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadFilterBarComponent {
  readonly sort = input<ThreadSort>('recent');
  readonly pinnedOnly = input(false);
  readonly personalityId = input<string | null>(null);
  readonly tag = input<string | null>(null);
  readonly personalities = input<readonly PersonalityOption[]>([]);
  readonly tags = input<readonly string[]>([]);

  readonly sortChange = output<ThreadSort>();
  readonly pinnedOnlyChange = output<boolean>();
  readonly personalityChange = output<string | null>();
  readonly tagChange = output<string | null>();
}
