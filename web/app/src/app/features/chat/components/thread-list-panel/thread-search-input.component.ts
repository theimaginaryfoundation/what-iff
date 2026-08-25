import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

@Component({
  selector: 'app-thread-search-input',
  standalone: true,
  template: `
    <div class="search">
      <label class="search__label" for="thread-search">Search threads</label>
      <div class="search__row">
        <input
          id="thread-search"
          type="search"
          [value]="query()"
          (input)="queryChange.emit($any($event.target).value)"
          (keydown.escape)="clear.emit()"
          placeholder="Search by title or tag"
          aria-describedby="thread-search-hint"
        />
        <button type="button" (click)="clear.emit()" aria-label="Clear thread search">Clear</button>
      </div>
      <p id="thread-search-hint">Search currently matches thread titles and tags.</p>
    </div>
  `,
  styles: [`
    .search {
      display: grid;
      gap: 0.35rem;
    }

    .search__label {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      font-weight: 600;
      text-transform: uppercase;
    }

    .search__row {
      display: grid;
      gap: 0.5rem;
      grid-template-columns: minmax(0, 1fr) auto;
    }

    input {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.625rem;
      color: var(--color-text-primary);
      padding: 0.55rem 0.75rem;
    }

    button {
      background: transparent;
      border: 1px solid var(--color-border-base);
      border-radius: 0.625rem;
      color: var(--color-text-muted);
      padding: 0.55rem 0.75rem;
    }

    p {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadSearchInputComponent {
  readonly query = input('');
  readonly queryChange = output<string>();
  readonly clear = output<void>();
}
