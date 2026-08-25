import {
  ChangeDetectionStrategy,
  Component,
  effect,
  inject,
  input,
  OnInit,
  output,
  signal,
} from '@angular/core';

import { Personality } from '../../../core/models/personality.model';
import { PersonalityService } from '../../../core/services/personality.service';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { PersonaPickerComponent } from './persona-picker.component';

/**
 * Dialog wrapper around `persona-picker`. Loads the personality list when
 * the dialog opens and re-emits the picker's `select` / `cancel` / `create`
 * outputs to the parent.
 */
@Component({
  selector: 'app-persona-picker-dialog',
  standalone: true,
  imports: [ModalComponent, PersonaPickerComponent],
  template: `
    <ui-modal [open]="open()" [labelledBy]="labelId" size="lg" (dismiss)="onCancel()">
      <div modal-header>
        <div class="grid gap-1">
          <h2 [id]="labelId" class="text-base font-semibold text-(--color-text-primary)">{{ title() }}</h2>
          @if (subtitle()) {
            <p class="m-0 text-xs text-(--color-text-secondary)">{{ subtitle() }}</p>
          }
        </div>
      </div>
      @if (loading()) {
        <p class="text-sm text-(--color-text-secondary)" role="status">Loading personalities…</p>
      } @else if (errorMessage()) {
        <p class="rounded-md border border-red-500 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300" role="alert">{{ errorMessage() }}</p>
      } @else {
        <persona-picker
          [personalities]="personalities()"
          [ariaLabel]="title()"
          [appearance]="pickerAppearance()"
          (select)="onSelect($event)"
          (cancel)="onCancel()"
          (create)="onCreate()"
        />
      }
    </ui-modal>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonaPickerDialogComponent implements OnInit {
  readonly open = input<boolean>(false);
  readonly title = input<string>('Pick a personality');
  readonly subtitle = input<string | null>(null);
  readonly pickerAppearance = input<'rich' | 'filter-list'>('rich');

  readonly select = output<Personality>();
  readonly cancel = output<void>();
  readonly create = output<void>();

  private readonly personalityService = inject(PersonalityService);

  readonly labelId = `persona-picker-dialog-${randomId()}`;
  readonly personalities = signal<Personality[]>([]);
  readonly loading = signal(false);
  readonly errorMessage = signal<string | null>(null);

  private hasLoaded = false;

  constructor() {
    effect(() => {
      if (this.open() && !this.hasLoaded) this.load();
    });
  }

  ngOnInit(): void {
    if (this.open() && !this.hasLoaded) this.load();
  }

  refresh(): void {
    this.hasLoaded = false;
    this.load();
  }

  onSelect(personality: Personality): void {
    this.select.emit(personality);
  }

  onCancel(): void {
    this.cancel.emit();
  }

  onCreate(): void {
    this.create.emit();
  }

  private load(): void {
    this.loading.set(true);
    this.errorMessage.set(null);
    this.personalityService.listPersonalities(1, 100).subscribe({
      next: response => {
        this.personalities.set(response.results ?? []);
        this.loading.set(false);
        this.hasLoaded = true;
      },
      error: err => {
        console.error('Failed to load personalities', err);
        this.errorMessage.set('Failed to load personalities. Please try again.');
        this.loading.set(false);
      },
    });
  }
}

function randomId(): string {
  return Math.random().toString(36).slice(2, 10);
}
